package buildinfo

import (
	"fmt"
	"runtime"
)

// These values are replaced by release builds with linker -X flags.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info is an immutable snapshot of build and runtime metadata.
type Info struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	OS        string
	Arch      string
}

func Current() Info {
	return Info{
		Version: Version, Commit: Commit, Date: Date,
		GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
	}
}

func Render(info Info) string {
	return fmt.Sprintf("version=%s\ncommit=%s\ndate=%s\ngo_version=%s\nos=%s\narch=%s\n",
		info.Version, info.Commit, info.Date, info.GoVersion, info.OS, info.Arch)
}
