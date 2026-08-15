package secrets

import (
	"strings"
)

const maxCredentialFileBytes = 514

// ReadCredentialFile reads exactly one LF- or CRLF-terminated credential line.
// Its platform implementation rejects unsafe filesystem objects before reading.
func ReadCredentialFile(path string) (string, error) {
	value, err := readCredentialFile(path)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else if strings.HasSuffix(value, "\n") {
		value = strings.TrimSuffix(value, "\n")
	}
	if value == "" || len(value) > maxCredentialFileBytes-2 || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalid
	}
	return value, nil
}
