//go:build windows

package memory

import "os"

func privateSQLiteBackupParent(os.FileMode) bool { return true }

func privateSQLiteBackupOutput(info os.FileInfo) bool { return info.Mode().IsRegular() }

func syncProjectMarkerDir(string) error { return nil }

func syncProjectMarkerParent(string) error { return nil }

func syncSQLiteBackupDirectory(string) error { return nil }
