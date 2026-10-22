package i18n

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	for _, test := range []struct {
		name, choice string
		env          map[string]string
		want         Language
		fail         bool
	}{
		{"empty", "", nil, English, false},
		{"explicit", "en", map[string]string{"HERDR_RELAY_LANG": "zh-TW", "LC_ALL": "zh_TW.UTF-8"}, English, false},
		{"explicit Chinese", "zh-TW", nil, TraditionalChinese, false},
		{"environment", "", map[string]string{"HERDR_RELAY_LANG": "zh-TW", "LC_ALL": "en_US.UTF-8"}, TraditionalChinese, false},
		{"explicit auto ignores override", "auto", map[string]string{"HERDR_RELAY_LANG": "zh-TW", "LANG": "en_US.UTF-8"}, English, false},
		{"env auto", "", map[string]string{"HERDR_RELAY_LANG": "auto", "LANG": "zh-TW.UTF-8"}, TraditionalChinese, false},
		{"LC_ALL", "auto", map[string]string{"LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "zh_TW.UTF-8"}, English, false},
		{"LC_MESSAGES", "auto", map[string]string{"LC_MESSAGES": "zh_TW.UTF-8", "LANG": "en_US.UTF-8"}, TraditionalChinese, false},
		{"LANG", "auto", map[string]string{"LANG": "zh_TW.UTF-8"}, TraditionalChinese, false},
		{"script", "auto", map[string]string{"LANG": "zh-Hant-TW"}, TraditionalChinese, false},
		{"HK", "auto", map[string]string{"LANG": "zh_HK.UTF-8"}, TraditionalChinese, false},
		{"modifier", "auto", map[string]string{"LANG": "zh.UTF-8@traditional"}, TraditionalChinese, false},
		{"unsupported system locale", "auto", map[string]string{"LC_ALL": "ja_JP.UTF-8", "LANG": "zh_TW.UTF-8"}, English, false},
		{"simplified is not Traditional", "auto", map[string]string{"LANG": "zh_CN.UTF-8"}, English, false},
		{"C", "auto", map[string]string{"LC_ALL": "C", "LANG": "zh_TW.UTF-8"}, English, false},
		{"C UTF8", "auto", map[string]string{"LANG": "C.UTF-8"}, English, false},
		{"POSIX", "auto", map[string]string{"LANG": "POSIX"}, English, false},
		{"invalid choice", "fr", nil, English, true},
		{"invalid environment", "", map[string]string{"HERDR_RELAY_LANG": "fr"}, English, true},
		{"override invalid environment", "en", map[string]string{"HERDR_RELAY_LANG": "fr"}, English, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(test.choice, func(key string) string { return test.env[key] })
			if got != test.want || (err != nil) != test.fail {
				t.Fatalf("%s %v", got, err)
			}
		})
	}
}

func TestErrorPresentationPreservesEvidenceAndIdentity(t *testing.T) {
	cause := NewError("relay service is paused; explicitly run service start to resume")
	wrapped := Errorf("relay startup failed; no message was enqueued: %w", cause)
	english := wrapped.Error()
	translated := LocalizeError(TraditionalChinese, wrapped)
	if !strings.Contains(translated.Error(), "relay 啟動失敗") || !strings.Contains(translated.Error(), "服務已暫停") || !errors.Is(translated, cause) {
		t.Fatal(translated)
	}
	var typed *messageError
	if !errors.As(translated, &typed) || wrapped.Error() != english || LocalizeError(English, wrapped) != wrapped {
		t.Fatal("error identity or English evidence changed")
	}
	joined := LocalizeError(TraditionalChinese, errors.Join(wrapped, os.ErrPermission))
	if !errors.Is(joined, os.ErrPermission) || !strings.Contains(joined.Error(), "relay 啟動失敗") {
		t.Fatal(joined)
	}
	literal := NewError("100% unknown diagnostic")
	if LocalizeError(TraditionalChinese, literal).Error() != literal.Error() {
		t.Fatal("unknown literal rewritten")
	}
	path := "role directory missing: %s"
	dynamic := Errorf("role directory missing: %s", path)
	if !strings.HasSuffix(LocalizeError(TraditionalChinese, dynamic).Error(), path) {
		t.Fatal("dynamic data translated")
	}
	if LocalizeError(TraditionalChinese, nil) != nil {
		t.Fatal("nil changed")
	}
}

func TestContextLanguageIsIndependent(t *testing.T) {
	for _, language := range []Language{English, TraditionalChinese} {
		t.Run(string(language), func(t *testing.T) {
			t.Parallel()
			ctx := WithLanguage(context.Background(), language)
			if FromContext(ctx) != language || FromContext(context.Background()) != English {
				t.Fatal("language leaked")
			}
		})
	}
}

func TestCatalogFormatParity(t *testing.T) {
	verbs := regexp.MustCompile(`%[sdqvw]`)
	for english, translated := range zhTW {
		if !reflect.DeepEqual(verbs.FindAllString(english, -1), verbs.FindAllString(translated, -1)) {
			t.Errorf("format mismatch: %q -> %q", english, translated)
		}
	}
}

// 只檢查人類提示入口；JSON、daemon 日誌與外部程式錯誤不屬於翻譯目錄。
func TestOwnedErrorCatalogCoverage(t *testing.T) {
	for _, dir := range []string{"../../cmd/hrelay", "../deliver", "../service", "../cliinstall"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				owner, ok := sel.X.(*ast.Ident)
				if !ok || owner.Name != "i18n" || (sel.Sel.Name != "NewError" && sel.Sel.Name != "Errorf") {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					t.Errorf("dynamic translation key in %s", path)
					return true
				}
				key, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Error(err)
					return true
				}
				if _, ok := zhTW[key]; !ok && key != "%s: %w" && key != "%s %v: %w: %s" {
					t.Errorf("missing translation in %s: %q", path, key)
				}
				return true
			})
		}
	}
}
