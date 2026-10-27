package roles

import (
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, name := range []string{"gm", "dev-2", "dev_2", "担当A", "Réviseur", "e\u0301quipe", strings.Repeat("a", 64)} {
		if !ValidName(name) {
			t.Errorf("valid name rejected: %q", name)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "-flag", "_role", "1role", "a role", "a\nrole", "a\x00", string([]byte{0xff}), strings.Repeat("a", 65)} {
		if ValidName(name) {
			t.Errorf("invalid name accepted: %q", name)
		}
	}
}
