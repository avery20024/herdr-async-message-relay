package main

import (
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

func TestUIRejectsArgumentsWithoutService(t *testing.T) {
	c := cliConfig(t)
	err := run(t.Context(), []string{"--root", c.Root, "ui", "unexpected"})
	if err == nil || !strings.Contains(err.Error(), "ui takes no arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}
func TestUIDetailedHelpDocumentsReadOnlyControls(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		var out strings.Builder
		if err := printCommandHelp(&out, language, "ui"); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"Ctrl+W", "Ctrl+P", "Ctrl+C", "Asia/Taipei", "ui"} {
			if !strings.Contains(out.String(), text) {
				t.Fatalf("missing %s", text)
			}
		}
	}
}
