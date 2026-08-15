//go:build windows

package secrets

func readCredentialFile(string) (string, error) { return "", ErrUnsupported }
