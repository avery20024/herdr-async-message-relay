package roles

import (
	"unicode"
	"unicode/utf8"
)

// ValidName 接受可作安全單層目錄的多語角色名稱，拒絕路徑與控制字元。
func ValidName(name string) bool {
	if name == "" || len(name) > 64 || !utf8.ValidString(name) {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) {
				return false
			}
		} else if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
