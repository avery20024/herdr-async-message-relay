package deliver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnixDaemonEndToEndAndSingleInstance(t *testing.T) {
	_, c := fixture(t)
	state, err := os.MkdirTemp("/tmp", "hd-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(state)
	c.StateDir = state
	fake := filepath.Join(state, "fake-herdr")
	promptLog := filepath.Join(state, "prompt")
	list := fmt.Sprintf(`{"result":{"agents":[{"foreground_cwd":%q,"pane_id":"wC:p5","agent_status":"idle","state_change_seq":1}]}}`, filepath.Join(c.Root, "角色", "gm"))
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1 $2\" = 'agent list' ]; then\nprintf '%%s\\n' '%s'\nelse\nprintf '%%s' \"$4\" > '%s'\nprintf '%%s\\n' '{\"result\":{\"agent_status\":\"working\"}}'\nfi\n", list, promptLog)
	if err = os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c.Herdr = fake
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, c, logger()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket())
	}}}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, e := net.DialTimeout("unix", c.Socket(), 10*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon socket did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	info, err := os.Stat(c.Socket())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%v %v", info, err)
	}
	if err := Serve(ctx, c, logger()); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatal(err)
	}
	data := []byte(`{"sender":"dev","recipient":"gm","body":"original\nmessage","idempotency_key":"e2e-key"}`)
	resp, err := client.Post("http://unix/v1/messages", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatal(resp.StatusCode, string(body))
	}
	var accepted struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(body, &accepted); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		resp, err = client.Get("http://unix/v1/messages/" + accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		var result struct {
			State string `json:"state"`
		}
		if err = json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if result.State == "submitted" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(string(body))
		}
		time.Sleep(5 * time.Millisecond)
	}
	prompt, err := os.ReadFile(promptLog)
	if err != nil || !strings.Contains(string(prompt), "original\nmessage") || !strings.Contains(string(prompt), "[hrelay | batch ") {
		t.Fatalf("%s %v", prompt, err)
	}
}

func TestSocketSafetyRefusesNonSocket(t *testing.T) {
	_, c := fixture(t)
	state, err := os.MkdirTemp("/tmp", "hd-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(state)
	c.StateDir = state
	if err = os.WriteFile(c.Socket(), []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Serve(context.Background(), c, logger()); err == nil {
		t.Fatal("overwrote non-socket")
	}
	data, _ := os.ReadFile(c.Socket())
	if string(data) != "keep me" {
		t.Fatal(string(data))
	}
}
