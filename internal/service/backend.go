package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

func (m *Manager) label(r Record) string {
	if m.Platform == "darwin" {
		return "dev.avery20024.hrelay." + key(r.Config)
	}
	return "hrelay-" + key(r.Config) + ".service"
}

func xmlText(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

func systemdQuote(value string, command bool) string {
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(value)
	if command {
		value = strings.ReplaceAll(value, "$", "$$")
	}
	return "\"" + value + "\""
}

func (m *Manager) render(r Record) (string, []byte, error) {
	values := []string{r.Binary, r.Config.Root}
	for name, value := range r.Environment {
		values = append(values, name, value)
	}
	for _, value := range values {
		if strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 }) {
			return "", nil, i18n.NewError("service paths and environment must not contain control characters")
		}
	}
	names := make([]string, 0, len(r.Environment))
	for name := range r.Environment {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	if m.Platform == "darwin" {
		out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<plist version=\"1.0\"><dict>\n")
		fmt.Fprintf(&out, "<key>Label</key><string>%s</string>\n", xmlText(m.label(r)))
		out.WriteString("<key>ProgramArguments</key><array>")
		for _, arg := range []string{r.Binary, "--root", r.Config.Root, "serve"} {
			fmt.Fprintf(&out, "<string>%s</string>", xmlText(arg))
		}
		out.WriteString("</array>\n<key>EnvironmentVariables</key><dict>\n")
		for _, name := range names {
			fmt.Fprintf(&out, "<key>%s</key><string>%s</string>\n", xmlText(name), xmlText(r.Environment[name]))
		}
		out.WriteString("</dict>\n<key>RunAtLoad</key><true/>\n<key>KeepAlive</key><true/>\n<key>ThrottleInterval</key><integer>5</integer>\n")
		for _, name := range []string{"StandardOutPath", "StandardErrorPath"} {
			fmt.Fprintf(&out, "<key>%s</key><string>%s</string>\n", name, xmlText(filepath.Join(m.dir(r.Config), "service.log")))
		}
		out.WriteString("</dict></plist>\n")
		return filepath.Join(m.Home, "Library", "LaunchAgents", m.label(r)+".plist"), []byte(out.String()), nil
	}
	if m.Platform != "linux" {
		return "", nil, i18n.NewError("unsupported service manager")
	}
	out.WriteString("[Unit]\nDescription=Herdr Async Message Relay\n\n[Service]\nType=simple\n")
	out.WriteString("ExecStart=")
	for i, arg := range []string{r.Binary, "--root", r.Config.Root, "serve"} {
		if i != 0 {
			out.WriteByte(' ')
		}
		out.WriteString(systemdQuote(arg, true))
	}
	out.WriteByte('\n')
	for _, name := range names {
		fmt.Fprintf(&out, "Environment=%s\n", systemdQuote(name+"="+r.Environment[name], false))
	}
	out.WriteString("Restart=always\nRestartSec=5\nUMask=0077\n\n[Install]\nWantedBy=default.target\n")
	config := r.Environment["XDG_CONFIG_HOME"]
	if config == "" {
		config = filepath.Join(m.Home, ".config")
	}
	if !filepath.IsAbs(config) {
		return "", nil, i18n.NewError("XDG_CONFIG_HOME must be absolute for user service installation")
	}
	return filepath.Join(config, "systemd", "user", m.label(r)), []byte(out.String()), nil
}

func (m *Manager) start(ctx context.Context, r Record) error {
	path, body, err := m.render(r)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err = atomicWrite(path, body, 0600); err != nil {
		return err
	}
	if m.Platform == "darwin" {
		domain := fmt.Sprintf("gui/%d", m.UID)
		if _, err = m.run(ctx, "launchctl", "enable", domain+"/"+m.label(r)); err != nil {
			return err
		}
		if _, err := m.run(ctx, "launchctl", "print", domain+"/"+m.label(r)); err == nil {
			// 不使用 -k：既有工作即使暫時未 ready，也不可被強制中止。
			_, err = m.run(ctx, "launchctl", "kickstart", domain+"/"+m.label(r))
			return err
		}
		_, err = m.run(ctx, "launchctl", "bootstrap", domain, path)
		return err
	}
	if _, err = m.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	_, err = m.run(ctx, "systemctl", "--user", "enable", "--now", m.label(r))
	return err
}

func (m *Manager) enable(ctx context.Context, r Record) error {
	if m.Platform == "darwin" {
		_, err := m.run(ctx, "launchctl", "enable", fmt.Sprintf("gui/%d/%s", m.UID, m.label(r)))
		return err
	}
	if m.Platform == "linux" {
		_, err := m.run(ctx, "systemctl", "--user", "enable", m.label(r))
		return err
	}
	return i18n.NewError("unsupported service manager")
}

func (m *Manager) stop(ctx context.Context, r Record) error {
	if m.Platform == "darwin" {
		target := fmt.Sprintf("gui/%d/%s", m.UID, m.label(r))
		if _, err := m.run(ctx, "launchctl", "disable", target); err != nil {
			return err
		}
		_, err := m.run(ctx, "launchctl", "bootout", target)
		var exit *exec.ExitError
		// disable 成功後，明確 ESRCH 只表示該 job 已不存在；其餘錯誤不得當成功。
		if errors.As(err, &exit) && exit.ExitCode() == int(syscall.ESRCH) {
			return nil
		}
		return err
	}
	if m.Platform == "linux" {
		_, err := m.run(ctx, "systemctl", "--user", "disable", "--now", m.label(r))
		return err
	}
	return i18n.NewError("unsupported service manager")
}
