package deliver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

type API struct {
	Store  *Store
	Config Config
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func apiError(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		apiError(w, 400, "invalid JSON request: "+err.Error())
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		apiError(w, 400, "expected exactly one JSON object")
		return false
	}
	return true
}

func (a API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var req SendRequest
		if !decode(w, r, int64(6*a.Config.BatchBytes+4096), &req) {
			return
		}
		if err := a.Config.Validate(req); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		m, err := a.Store.Enqueue(r.Context(), req)
		if errors.Is(err, ErrConflict) {
			apiError(w, 409, err.Error())
			return
		}
		if err != nil {
			apiError(w, 500, "unable to commit message")
			return
		}
		respond(w, 202, map[string]any{"accepted": true, "id": m.ID, "idempotency_key": m.IdempotencyKey, "state": m.State})
	})
	mux.HandleFunc("GET /v1/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, err := a.Store.Q.GetMessage(r.Context(), r.PathValue("id"))
		if errors.Is(err, sql.ErrNoRows) {
			apiError(w, 404, "message not found")
			return
		}
		if err != nil {
			apiError(w, 500, "unable to read message")
			return
		}
		respond(w, 200, m)
	})
	mux.HandleFunc("GET /v1/batches/{id}", func(w http.ResponseWriter, r *http.Request) {
		batch, err := a.Store.Q.GetBatch(r.Context(), r.PathValue("id"))
		if errors.Is(err, sql.ErrNoRows) {
			apiError(w, 404, "batch not found")
			return
		}
		if err != nil {
			apiError(w, 500, "unable to read batch")
			return
		}
		messages, err := a.Store.Q.BatchMessages(r.Context(), batch.ID)
		if err != nil {
			apiError(w, 500, "unable to read batch members")
			return
		}
		respond(w, 200, map[string]any{"batch": batch, "messages": messages})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		counts, err := a.Store.Q.CountStates(r.Context())
		if err != nil {
			apiError(w, 500, "unable to read counts")
			return
		}
		messages, err := a.Store.Q.ListMessages(r.Context(), 100)
		if err != nil {
			apiError(w, 500, "unable to read messages")
			return
		}
		// 狀態摘要不重複攜帶完整訊息；需要本文時以 ID 查詢。
		for i := range messages {
			messages[i].Body = ""
		}
		batches, err := a.Store.Q.ListBatches(r.Context(), 100)
		if err != nil {
			apiError(w, 500, "unable to read batches")
			return
		}
		respond(w, 200, map[string]any{"project_root": a.Config.Root, "counts": counts, "messages": messages, "batches": batches, "batch_size": a.Config.BatchSize, "batch_bytes": a.Config.BatchBytes, "poll_ms": a.Config.Poll.Milliseconds()})
	})
	mux.HandleFunc("POST /v1/batches/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action  string `json:"action"`
			Note    string `json:"note"`
			Confirm bool   `json:"confirm"`
		}
		if !decode(w, r, 4096, &req) {
			return
		}
		if !req.Confirm || strings.TrimSpace(req.Note) == "" {
			apiError(w, 400, "explicit confirmation and inspection note required")
			return
		}
		if err := a.Store.Resolve(r.Context(), r.PathValue("id"), req.Action, req.Note); err != nil {
			apiError(w, 409, err.Error())
			return
		}
		respond(w, 200, map[string]bool{"resolved": true})
	})
	return mux
}

// Serve 使用私人 Unix socket，不開 TCP port；檔案鎖防止雙 worker 與重複復原。
func Serve(ctx context.Context, c Config, logger *slog.Logger) error {
	if len(c.Socket()) > 100 {
		return i18n.NewError("Unix socket path too long; set a shorter DELIVER_STATE_DIR")
	}
	if err := os.MkdirAll(c.StateDir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(c.StateDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return i18n.NewError("state directory must not be a symlink")
	}
	if err = os.Chmod(c.StateDir, 0700); err != nil {
		return err
	}
	lockPath := filepath.Join(c.StateDir, "daemon.lock")
	if info, e := os.Lstat(lockPath); e == nil && !info.Mode().IsRegular() {
		return i18n.NewError("lock must be a regular file")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return i18n.NewError("daemon already running or state lock unavailable")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if info, e := os.Lstat(c.Socket()); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return i18n.NewError("socket path exists and is not a socket")
		}
		probe, e := net.DialTimeout("unix", c.Socket(), time.Second)
		if e == nil {
			probe.Close()
			return i18n.NewError("socket already in use")
		}
		if err = os.Remove(c.Socket()); err != nil {
			return err
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	s, err := OpenStore(ctx, filepath.Join(c.StateDir, "queue.db"))
	if err != nil {
		return err
	}
	defer s.DB.Close()
	if err = s.Q.BindProject(ctx, c.Root); err != nil {
		return err
	}
	boundRoot, err := s.Q.BoundProject(ctx)
	if err != nil {
		return err
	}
	if boundRoot != c.Root {
		return i18n.NewError("state directory belongs to another project root")
	}
	if err = s.Recover(ctx); err != nil {
		return err
	}
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(c.Socket(), 0600); err != nil {
		return err
	}
	defer os.Remove(c.Socket())
	server := &http.Server{Handler: API{Store: s, Config: c}.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	workerDone := make(chan struct{})
	worker := &Worker{Store: s, Config: c, Herdr: CLIHerdr{Binary: c.Herdr, Timeout: c.CommandTimeout}, Log: logger}
	go func() { defer close(workerDone); worker.Run(workerCtx) }()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	logger.Info("herdr-async-message-relay ready", "socket", c.Socket(), "batch_size", c.BatchSize, "poll", c.Poll)
	select {
	case <-ctx.Done():
		err = nil
	case e := <-serverDone:
		if !errors.Is(e, http.ErrServerClosed) {
			err = e
		}
	}
	cancelWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e := server.Shutdown(shutdownCtx); e != nil {
		_ = server.Close()
		if err == nil {
			err = e
		}
	}
	<-workerDone
	return err
}
