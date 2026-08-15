package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

func acquireSyncEnrollmentLock(ctx context.Context, database string) (func(), error) {
	path := filepath.Join(filepath.Dir(database), ".sync-enrollment.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("invalid sync enrollment lock")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	lock, err := acquireSyncEnrollmentFile(ctx, file)
	if err != nil {
		return nil, err
	}
	opened, openErr := file.Stat()
	current, currentErr := os.Lstat(path)
	if openErr != nil || currentErr != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		lock.release()
		return nil, fmt.Errorf("invalid sync enrollment lock")
	}
	return lock.release, nil
}

func syncEnrollmentCredentialRefs(database string) (string, string) {
	digest := sha256.Sum256([]byte(filepath.Clean(database)))
	prefix := fmt.Sprintf("secret://keychain/sync/%x", digest[:])
	return prefix + "/0", prefix + "/1"
}
