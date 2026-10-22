package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

func captureOutput(t *testing.T, target **os.File, fn func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	original := *target
	*target = file
	defer func() { *target = original }()
	fn()
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestLanguageSelectionBeforeProjectAndSideEffects(t *testing.T) {
	rootWithoutRoles := t.TempDir()
	t.Setenv("HERDR_RELAY_LANG", "zh-TW")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{}, "用法："},
		{[]string{"--lang", "en"}, "usage:"},
		{[]string{"--lang=auto"}, "usage:"},
		{[]string{"--lang", "zh-TW", "--root", rootWithoutRoles, "send"}, "根目錄必須包含"},
		{[]string{"--lang", "en", "--root", rootWithoutRoles, "send"}, "root must contain"},
		{[]string{"--lang", "fr", "version"}, "unsupported language"},
		{[]string{"--lang", "zh-TW", "cli", "install", "--bin-dir", "relative"}, "CLI 安裝目錄必須"},
	} {
		err := run(context.Background(), test.args)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%v: %v", test.args, err)
		}
	}
}

func TestLocalizedInstallGuidanceKeepsJSON(t *testing.T) {
	binDir := t.TempDir()
	t.Setenv("PATH", "")
	var stderr string
	stdout := captureOutput(t, &os.Stdout, func() {
		stderr = captureOutput(t, &os.Stderr, func() {
			if err := run(context.Background(), []string{"--lang", "zh-TW", "cli", "install", "--bin-dir", binDir}); err != nil {
				t.Fatal(err)
			}
		})
	})
	var result struct {
		Path   string `json:"path"`
		OnPath bool   `json:"on_path"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.Path == "" || result.OnPath {
		t.Fatalf("%s: %v", stdout, err)
	}
	if !strings.Contains(stderr, "CLI 已安裝於 "+result.Path) || !strings.Contains(stderr, "既有行程不會被修改") {
		t.Fatal(stderr)
	}
}

func TestHelpUsesChosenLanguage(t *testing.T) {
	c := cliConfig(t)
	for _, test := range []struct {
		language string
		args     []string
		want     string
	}{
		{"zh-TW", []string{"--help"}, "操作提示語系"},
		{"en", []string{"--help"}, "display language"},
		{"zh-TW", []string{"send", "--help"}, "完整訊息原文"},
		{"zh-TW", []string{"resolve", "--help"}, "查核證據"},
		{"zh-TW", []string{"cli", "install", "--help"}, "固定安裝目錄"},
	} {
		args := append([]string{"--root", c.Root, "--lang", test.language}, test.args...)
		out := captureOutput(t, &os.Stderr, func() {
			if err := run(context.Background(), args); !errors.Is(err, flag.ErrHelp) {
				t.Fatal(err)
			}
		})
		if !strings.Contains(out, test.want) {
			t.Fatalf("%v: %s", args, out)
		}
	}
}

func TestChineseAmbiguousSendRetainsRetryKey(t *testing.T) {
	c := cliConfig(t)
	err := run(context.Background(), []string{"--lang", "zh-TW", "--root", c.Root, "send", "--from", "dev", "--to", "gm", "--body", "original", "--key", "keep-key"})
	if err == nil || !strings.Contains(err.Error(), "訊息可能已儲存") || !strings.Contains(err.Error(), `--key "keep-key"`) {
		t.Fatal(err)
	}
}

func TestLanguagesDoNotChangeJSONOrRequestBody(t *testing.T) {
	c := cliConfig(t)
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	const response = "{\"accepted\":true,\"state\":\"queued\",\"id\":\"msg-fixed\"}\n"
	// 直接比較兩種語系實際送出的 bytes，避免 JSON 欄位命名或原文被翻譯。
	received := make(chan string, 2)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, response)
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	for _, language := range []string{"en", "zh-TW"} {
		out := captureOutput(t, &os.Stdout, func() {
			err := run(context.Background(), []string{"--lang", language, "--root", c.Root, "send", "--from", "dev", "--to", "gm", "--body", "原文\n--lang en", "--key", "stable-key"})
			if err != nil {
				t.Fatal(err)
			}
		})
		if out != response {
			t.Fatalf("JSON changed: %q", out)
		}
	}
	first, second := <-received, <-received
	if first != second || !strings.Contains(first, `原文\n--lang en`) {
		t.Fatalf("%s != %s", first, second)
	}
}

func TestLanguageDoesNotChangeAPIErrorEvidence(t *testing.T) {
	c := cliConfig(t)
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	const evidence = `{"error":"idempotency key already used with different content"}`
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(409); _, _ = io.WriteString(w, evidence) })}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	err = run(context.Background(), []string{"--lang", "zh-TW", "--root", c.Root, "send", "--from", "dev", "--to", "gm", "--body", "body", "--key", "stable-key"})
	if err == nil || !strings.Contains(err.Error(), "尚未確認送出") || !strings.Contains(err.Error(), evidence) {
		t.Fatal(err)
	}
}
