package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const linkerPackage = "github.com/vgxness/vgxness/internal/buildinfo"

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	semverPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))(?:\.(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

type Options struct {
	Version    string
	Commit     string
	Date       string
	Output     string
	Repository string
}

type target struct {
	os   string
	arch string
}

var targets = []target{
	{os: "linux", arch: "amd64"},
	{os: "linux", arch: "arm64"},
	{os: "darwin", arch: "amd64"},
	{os: "darwin", arch: "arm64"},
	{os: "windows", arch: "amd64"},
	{os: "windows", arch: "arm64"},
}

type archiveFile struct {
	name string
	data []byte
	mode os.FileMode
}

type durabilityHooks struct {
	syncFile      func(string) error
	syncDirectory func(string) error
	publish       func(string, string) error
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vgxness-release", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options Options
	flags.StringVar(&options.Version, "version", "", "release tag")
	flags.StringVar(&options.Commit, "commit", "", "full Git commit")
	flags.StringVar(&options.Date, "date", "", "RFC3339 commit date")
	flags.StringVar(&options.Output, "output", "", "output directory")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || options.Version == "" || options.Commit == "" || options.Date == "" || options.Output == "" {
		fmt.Fprintln(stderr, "usage: vgxness-release --version vMAJOR.MINOR.PATCH --commit <40-lowercase-hex> --date <RFC3339> --output <directory>")
		return 2
	}
	repository, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "release: resolve repository")
		return 1
	}
	options.Repository = repository
	if err := Package(ctx, options); err != nil {
		fmt.Fprintf(stderr, "release: %v\n", err)
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "output=%s\n", options.Output); err != nil {
		return 1
	}
	return 0
}

func Package(ctx context.Context, options Options) error {
	return packageWithBuildAndHooks(ctx, options, build, durabilityHooks{
		syncFile:      syncFilePath,
		syncDirectory: syncDirectoryPath,
		publish:       publishNoReplace,
	})
}

func packageWithBuild(ctx context.Context, options Options, builder func(context.Context, string, string, target, Options) error) error {
	return packageWithBuildAndHooks(ctx, options, builder, durabilityHooks{
		syncFile:      syncFilePath,
		syncDirectory: syncDirectoryPath,
		publish:       publishNoReplace,
	})
}

func packageWithBuildAndHooks(ctx context.Context, options Options, builder func(context.Context, string, string, target, Options) error, hooks durabilityHooks) (resultErr error) {
	if hooks.syncFile == nil || hooks.syncDirectory == nil || hooks.publish == nil {
		return errors.New("invalid release durability hooks")
	}
	date, err := validateMetadata(options.Version, options.Commit, options.Date)
	if err != nil {
		return err
	}
	if err := validateOutput(options.Output); err != nil {
		return err
	}
	repository, err := filepath.Abs(options.Repository)
	if err != nil || options.Repository == "" {
		return errors.New("invalid repository")
	}
	output, err := filepath.Abs(options.Output)
	if err != nil {
		return errors.New("invalid output directory")
	}
	parent := filepath.Dir(output)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return errors.New("output parent must be an existing directory")
	}
	license, err := os.ReadFile(filepath.Join(repository, "LICENSE"))
	if err != nil {
		return fmt.Errorf("read LICENSE: %w", err)
	}
	readme, err := os.ReadFile(filepath.Join(repository, "README.md"))
	if err != nil {
		return fmt.Errorf("read README.md: %w", err)
	}

	stage, err := os.MkdirTemp(parent, ".vgxness-release-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	stageRetained := true
	defer func() {
		if !stageRetained {
			_ = os.RemoveAll(stage)
			return
		}
		if resultErr != nil {
			resultErr = fmt.Errorf("%w; staging retained at %s", resultErr, stage)
		}
	}()
	assets := stage
	work := filepath.Join(stage, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		return fmt.Errorf("create build staging: %w", err)
	}

	version := strings.TrimPrefix(options.Version, "v")
	archives := make([]string, 0, len(targets))
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		stem := fmt.Sprintf("vgxness_%s_%s_%s", version, target.os, target.arch)
		executable := "vgxness"
		extension := ".tar.gz"
		if target.os == "windows" {
			executable = "vgxness.exe"
			extension = ".zip"
		}
		buildDirectory := filepath.Join(work, target.os+"-"+target.arch)
		if err := os.Mkdir(buildDirectory, 0o755); err != nil {
			return fmt.Errorf("create %s/%s build directory: %w", target.os, target.arch, err)
		}
		binaryPath := filepath.Join(buildDirectory, executable)
		if err := builder(ctx, repository, binaryPath, target, options); err != nil {
			return err
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			return fmt.Errorf("read %s/%s binary: %w", target.os, target.arch, err)
		}
		files := []archiveFile{
			{name: executable, data: binary, mode: 0o755},
			{name: "LICENSE", data: license, mode: 0o644},
			{name: "README.md", data: readme, mode: 0o644},
		}
		archiveName := stem + extension
		archivePath := filepath.Join(assets, archiveName)
		if target.os == "windows" {
			err = writeZip(archivePath, stem, files, date)
		} else {
			err = writeTarGz(archivePath, stem, files, date)
		}
		if err != nil {
			return fmt.Errorf("package %s/%s: %w", target.os, target.arch, err)
		}
		if err := hooks.syncFile(archivePath); err != nil {
			return fmt.Errorf("sync staged archive %s: %w", archiveName, err)
		}
		archives = append(archives, archiveName)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.RemoveAll(work); err != nil {
		return fmt.Errorf("clean build staging: %w", err)
	}
	if err := writeChecksums(assets, archives); err != nil {
		return err
	}
	checksums := filepath.Join(assets, "SHA256SUMS")
	if err := hooks.syncFile(checksums); err != nil {
		return fmt.Errorf("sync staged SHA256SUMS: %w", err)
	}
	if err := hooks.syncDirectory(assets); err != nil {
		return fmt.Errorf("sync staging directory: %w", err)
	}
	if err := hooks.syncDirectory(parent); err != nil {
		return fmt.Errorf("sync output parent before publish: %w", err)
	}
	published, err := publishAssetsWithHooks(assets, output, hooks.publish, hooks.syncDirectory)
	if err != nil {
		if published {
			stageRetained = false
			return fmt.Errorf("%w; published output retained at %s", err, output)
		}
		return err
	}
	stageRetained = false
	return nil
}

func publishAssets(assets, output string) error {
	return publishAssetsWith(assets, output, publishNoReplace)
}

func publishAssetsWith(assets, output string, publish func(string, string) error) error {
	_, err := publishAssetsWithHooks(assets, output, publish, syncDirectoryPath)
	return err
}

func publishAssetsWithHooks(assets, output string, publish func(string, string) error, syncDirectory func(string) error) (bool, error) {
	staged, err := os.Open(assets)
	if err != nil {
		return false, fmt.Errorf("open staged assets: %w", err)
	}
	defer staged.Close()
	stagedInfo, err := staged.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect staged assets: %w", err)
	}
	entries, err := os.ReadDir(assets)
	if err != nil {
		return false, fmt.Errorf("read staged assets: %w", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return false, fmt.Errorf("inspect staged asset %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("staged asset %s is not a regular file", entry.Name())
		}
	}
	if _, err := os.Lstat(output); err == nil {
		return false, errors.New("output changed before publish: output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect output before publish: %w", err)
	}
	if err := publish(assets, output); err != nil {
		outputInfo, statErr := os.Stat(output)
		if statErr == nil && os.SameFile(stagedInfo, outputInfo) {
			return true, fmt.Errorf("publish staged release: %w", err)
		}
		if statErr == nil {
			return false, fmt.Errorf("publish staged release: %w; publication conflict: output identity differs", err)
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return false, fmt.Errorf("publish staged release: %w; inspect output identity: %v", err, statErr)
		}
		return false, fmt.Errorf("publish staged release: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return true, fmt.Errorf("sync output parent after publish: %w", err)
	}
	return true, nil
}

func validateMetadata(version, commit, date string) (time.Time, error) {
	if !semverPattern.MatchString(version) {
		return time.Time{}, errors.New("version must be a strict v-prefixed SemVer tag")
	}
	if !commitPattern.MatchString(commit) {
		return time.Time{}, errors.New("commit must be 40 lowercase hexadecimal characters")
	}
	parsed, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return time.Time{}, errors.New("date must be RFC3339")
	}
	return parsed.UTC(), nil
}

func validateOutput(output string) error {
	if output == "" || strings.ContainsRune(output, 0) || filepath.Clean(output) != output || filepath.Clean(output) == "." {
		return errors.New("invalid output directory")
	}
	for _, part := range strings.FieldsFunc(output, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return errors.New("output traversal is not allowed")
		}
	}
	info, err := os.Lstat(output)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("output must be a directory")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		return fmt.Errorf("read output: %w", err)
	}
	_ = entries
	return errors.New("output directory already exists")
}

func build(ctx context.Context, repository, output string, target target, options Options) error {
	ldflags := strings.Join([]string{
		"-s", "-w", "-buildid=",
		"-X", linkerPackage + ".Version=" + options.Version,
		"-X", linkerPackage + ".Commit=" + options.Commit,
		"-X", linkerPackage + ".Date=" + options.Date,
	}, " ")
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-mod=readonly", "-ldflags", ldflags, "-o", output, "./cmd/vgxness")
	command.Dir = repository
	command.Env = releaseEnvironment(target, options.Date)
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s/%s: %w: %s", target.os, target.arch, err, strings.TrimSpace(string(combined)))
	}
	return nil
}

func releaseEnvironment(target target, date string) []string {
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && (name == "CGO_ENABLED" || name == "GOOS" || name == "GOARCH" || name == "SOURCE_DATE_EPOCH") {
			continue
		}
		environment = append(environment, entry)
	}
	parsed, _ := time.Parse(time.RFC3339, date)
	return append(environment,
		"CGO_ENABLED=0", "GOOS="+target.os, "GOARCH="+target.arch,
		fmt.Sprintf("SOURCE_DATE_EPOCH=%d", parsed.Unix()),
	)
}

func writeTarGz(path, stem string, files []archiveFile, date time.Time) (resultErr error) {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err := output.Close(); resultErr == nil && err != nil {
			resultErr = err
		}
	}()
	gz, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.Header.ModTime = date.UTC()
	gz.Header.OS = 255
	tarWriter := tar.NewWriter(gz)
	if err := tarWriter.WriteHeader(&tar.Header{Name: stem + "/", Mode: 0o755, Typeflag: tar.TypeDir, ModTime: date.UTC(), Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	for _, file := range files {
		if err := validateArchiveFile(file); err != nil {
			return err
		}
		header := &tar.Header{Name: stem + "/" + file.name, Mode: int64(file.mode.Perm()), Size: int64(len(file.data)), Typeflag: tar.TypeReg, ModTime: date.UTC(), Format: tar.FormatUSTAR}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write(file.data); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeZip(path, stem string, files []archiveFile, date time.Time) (resultErr error) {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err := output.Close(); resultErr == nil && err != nil {
			resultErr = err
		}
	}()
	writer := zip.NewWriter(output)
	directory := &zip.FileHeader{Name: stem + "/", Method: zip.Store, Modified: date.UTC()}
	directory.SetMode(os.ModeDir | 0o755)
	if _, err := writer.CreateHeader(directory); err != nil {
		return err
	}
	for _, file := range files {
		if err := validateArchiveFile(file); err != nil {
			return err
		}
		header := &zip.FileHeader{Name: stem + "/" + file.name, Method: zip.Deflate, Modified: date.UTC()}
		header.SetMode(file.mode.Perm())
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := entry.Write(file.data); err != nil {
			return err
		}
	}
	return writer.Close()
}

func validateArchiveFile(file archiveFile) error {
	if file.name == "" || filepath.Base(file.name) != file.name || strings.ContainsAny(file.name, `/\\`) {
		return errors.New("invalid archive file name")
	}
	return nil
}

func writeChecksums(directory string, archives []string) error {
	names := append([]string(nil), archives...)
	sort.Strings(names)
	var output bytes.Buffer
	for _, name := range names {
		if filepath.Base(name) != name {
			return errors.New("invalid checksum file name")
		}
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("checksum %s: %w", name, err)
		}
		digest := sha256.Sum256(data)
		fmt.Fprintf(&output, "%x  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(directory, "SHA256SUMS"), output.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write SHA256SUMS: %w", err)
	}
	return nil
}
