// Package i18n 僅翻譯人類操作提示；協定、日誌與 AI 訊息封套維持固定格式。
package i18n

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Language string

const (
	English            Language = "en"
	TraditionalChinese Language = "zh-TW"
)

// Resolve 不讀取全域環境，讓呼叫端及測試明確提供同一份環境快照。
func Resolve(choice string, getenv func(string) string) (Language, error) {
	if choice == "" {
		choice = getenv("HERDR_RELAY_LANG")
	}
	switch choice {
	case "en":
		return English, nil
	case "zh-TW":
		return TraditionalChinese, nil
	case "", "auto":
		for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if value := strings.TrimSpace(getenv(name)); value != "" {
				return detect(value), nil
			}
		}
		return English, nil
	default:
		return English, fmt.Errorf("unsupported language %q; choose auto, en or zh-TW", choice)
	}
}

func detect(locale string) Language {
	locale = strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
	base, modifier, _ := strings.Cut(locale, "@")
	base, _, _ = strings.Cut(base, ".")
	if base == "zh-tw" || base == "zh-hk" || base == "zh-mo" || base == "zh-hant" || strings.HasPrefix(base, "zh-hant-") ||
		(base == "zh" && (modifier == "traditional" || modifier == "hant")) {
		return TraditionalChinese
	}
	return English
}

type contextKey struct{}

func WithLanguage(ctx context.Context, language Language) context.Context {
	return context.WithValue(ctx, contextKey{}, language)
}

func FromContext(ctx context.Context) Language {
	if language, ok := ctx.Value(contextKey{}).(Language); ok {
		return language
	}
	return English
}

// Text 以英文原文為穩定訊息鍵；未收錄文字保持原文，不猜測翻譯動態內容。
func (l Language) Text(english string) string {
	if l == TraditionalChinese {
		if text, ok := zhTW[english]; ok {
			return text
		}
	}
	return english
}

func (l Language) Format(english string, args ...any) string {
	return fmt.Sprintf(l.Text(english), args...)
}

type messageError struct {
	format   string
	args     []any
	original error
}

func (e *messageError) Error() string { return e.original.Error() }
func (e *messageError) Unwrap() error { return e.original }

// NewError 與 Errorf 預設仍回傳英文；僅 CLI 顯示邊界決定是否翻譯。
func NewError(text string) error {
	return &messageError{format: text, original: errors.New(text)}
}

func Errorf(format string, args ...any) error {
	return &messageError{format: format, args: args, original: fmt.Errorf(format, args...)}
}

type displayError struct {
	text     string
	original error
}

func (e *displayError) Error() string { return e.text }
func (e *displayError) Unwrap() error { return e.original }

// LocalizeError 保留 errors.Is／As 鏈，且不修改原錯誤或 API 回應。
func LocalizeError(language Language, err error) error {
	if err == nil || language == English {
		return err
	}
	if e, ok := err.(*messageError); ok {
		text := language.Text(e.format)
		if e.args != nil {
			args := append([]any(nil), e.args...)
			for i, arg := range args {
				if cause, ok := arg.(error); ok {
					args[i] = LocalizeError(language, cause)
				}
			}
			text = fmt.Sprintf(strings.ReplaceAll(text, "%w", "%v"), args...)
		}
		return &displayError{text: text, original: err}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var parts []string
		for _, cause := range joined.Unwrap() {
			if cause != nil {
				parts = append(parts, LocalizeError(language, cause).Error())
			}
		}
		return &displayError{text: strings.Join(parts, "\n"), original: err}
	}
	return err
}
