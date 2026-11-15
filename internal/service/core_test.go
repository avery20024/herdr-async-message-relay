package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func coreServiceFixture(t *testing.T, platform string) *fixture {
	f := setup(t, platform)
	f.c.Backend, f.c.Core, f.c.CoreSocket = "core", f.m.Executable, "/test/core.sock"
	f.m.Environment = []string{"HOME=" + f.m.Home, "PATH=/usr/bin:/bin", "HC_TERMINAL_ID=t13", "HC_TERMINAL_SOCKET=/test/core.sock", "HC_SCOPE=not-for-service", "HERDR_SOCKET_PATH=/old/herdr.sock", "SECRET_TOKEN=not-for-services"}
	return f
}

func TestCoreServicePersistsBackendNotCallerIdentity(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			f := coreServiceFixture(t, platform)
			if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
				t.Fatal(err)
			}
			r, err := f.m.load(f.c)
			if err != nil {
				t.Fatal(err)
			}
			if r.Schema != 2 || r.Environment[deliver.BackendEnv] != "core" || r.Environment["HC_SOCKET"] != "/test/core.sock" {
				t.Fatalf("%+v", r)
			}
			for _, key := range []string{"HC_TERMINAL_ID", "HC_TERMINAL_SOCKET", "HC_SCOPE", "HERDR_SOCKET_PATH", "SECRET_TOKEN"} {
				if _, ok := r.Environment[key]; ok {
					t.Fatal("persisted caller or unrelated context", key)
				}
			}
			_, body, err := f.m.render(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "HC_SOCKET") || !strings.Contains(string(body), "HRELAY_BACKEND") || strings.Contains(string(body), "HC_TERMINAL_ID") {
				t.Fatal(string(body))
			}
			before := len(f.calls)
			if err := f.m.Ensure(context.Background(), f.c, false); err != nil || len(f.calls) != before {
				t.Fatal("healthy service was changed", err)
			}
			f.m.Environment = []string{"HC_TERMINAL_ID=t1", "HC_TERMINAL_SOCKET=/other.sock"}
			if err := f.m.Ensure(context.Background(), f.c, false); err == nil || len(f.calls) != before {
				t.Fatal("cross socket takeover", err)
			}
		})
	}
}

func TestCoreRegistryNamespaceDoesNotExposeSchemaToOldReader(t *testing.T) {
	t.Setenv("HERDR_RELAY_SERVICE_HOME", filepath.Join(t.TempDir(), "registry"))
	t.Setenv(deliver.BackendEnv, "herdr")
	old, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(deliver.BackendEnv, "core")
	t.Setenv("HC_TERMINAL_ID", "")
	t.Setenv("HC_TERMINAL_SOCKET", "")
	t.Setenv("HC_SOCKET", "/core.sock")
	core, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if core.Base != filepath.Join(old.Base, "core") {
		t.Fatal(old.Base, core.Base)
	}
}

func TestOldAndCoreServiceRecordsCoexistWithoutModifyingOldRecord(t *testing.T) {
	f := setup(t, "linux")
	f.ready.Store(true)
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	path := f.m.recordPath(f.c)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := *f.m
	m.Base = filepath.Join(f.m.Base, "core")
	m.Environment = []string{deliver.BackendEnv + "=core", "HC_SOCKET=/core.sock"}
	c := f.c
	c.Backend, c.Core, c.CoreSocket, c.StateDir = "core", f.m.Executable, "/core.sock", filepath.Join(f.c.Root, "core-state")
	if err := m.Ensure(context.Background(), c, false); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(before) != string(after) {
		t.Fatal("old record changed")
	}
	records, err := f.m.records()
	if err != nil || len(records) != 1 || records[0].Schema != 1 {
		t.Fatal(records, err)
	}
	if _, err := f.m.load(c); err == nil {
		t.Fatal("cross registry access")
	}
	// 即使顯式重用舊 state，也不能以 core 接管舊服務。
	c.StateDir = f.c.StateDir
	if _, err := f.m.load(c); err == nil {
		t.Fatal("cross backend state accepted")
	}
}

func TestCoreServiceIDsAndRegisteredEmptyStateAreIsolated(t *testing.T) {
	f := setup(t, "linux")
	f.m.RegistryRoot = f.m.Base
	f.ready.Store(true)
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(f.m.recordPath(f.c))
	m := *f.m
	m.Base = filepath.Join(f.m.Base, "core")
	m.Environment = []string{deliver.BackendEnv + "=core", "HC_SOCKET=/a.sock"}
	c := f.c
	c.Backend, c.Core, c.CoreSocket = "core", f.m.Executable, "/a.sock"
	if m.ID(c) == f.m.ID(f.c) {
		t.Fatal("OS service ID collides with old backend")
	}
	if err := m.Ensure(context.Background(), c, false); err == nil {
		t.Fatal("core claimed an old registered state without a DB")
	}
	if after, _ := os.ReadFile(f.m.recordPath(f.c)); string(before) != string(after) {
		t.Fatal("old registration was modified")
	}
	c.StateDir = filepath.Join(c.Root, "separate-core")
	if err := m.Ensure(context.Background(), c, false); err != nil {
		t.Fatal(err)
	}
	b := c
	b.CoreSocket = "/b.sock"
	if m.ID(b) == m.ID(c) {
		t.Fatal("OS service ID collides across core sockets")
	}
	m.Environment = []string{deliver.BackendEnv + "=core", "HC_SOCKET=/b.sock"}
	if err := m.Ensure(context.Background(), b, false); err == nil {
		t.Fatal("another core claimed an already registered state without a DB")
	}
}

func TestLegacyEnsureNeverReadsBrokenCoreRegistry(t *testing.T) {
	f := setup(t, "linux")
	f.m.RegistryRoot = f.m.Base
	f.ready.Store(true)
	dir := filepath.Join(f.m.Base, "core", "broken")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "service.json")
	if err := os.WriteFile(path, []byte("invalid core record"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal("core corruption affected legacy service", err)
	}
	if body, _ := os.ReadFile(path); string(body) != "invalid core record" {
		t.Fatal("core record changed")
	}
}

func TestCoreRecordSchemaAndSavedContextValidation(t *testing.T) {
	f := coreServiceFixture(t, "linux")
	r, err := f.m.newRecord(f.c)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRecord(r); err != nil {
		t.Fatal(err)
	}
	r.Environment["HC_TERMINAL_ID"] = "t13"
	if validateRecord(r) == nil {
		t.Fatal("stored caller id must not be restored")
	}
	delete(r.Environment, "HC_TERMINAL_ID")
	r.Environment["HC_SOCKET"] = "/different.sock"
	if validateRecord(r) == nil {
		t.Fatal("inconsistent saved socket")
	}
}
