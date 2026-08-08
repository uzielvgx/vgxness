//go:build !darwin && !linux && !windows

package codex

import "os"

func safeAncestor(string, os.FileInfo) bool { return false }
func ownedDir(string, os.FileInfo) bool     { return false }
