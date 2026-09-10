package modelplan

import (
	"errors"
	"strings"
)

var ErrInvalid = errors.New("invalid model plan")
var ErrProviderMismatch = errors.New("model provider mismatch")

func validText(value string, limit int) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len([]rune(trimmed)) > limit || trimmed != value {
		return false
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return false
		}
	}
	return true
}
