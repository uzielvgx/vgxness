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
	"io/fs"
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
	return packageWithBuild(ctx, options, build)
}

func packageWithBuild(ctx context.Context, options Options, builder func(context.Context, string, string, target, Options) error) error {
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
	defer os.RemoveAll(stage)
	assets := filepath.Join(stage, "assets")
	work := filepath.Join(stage, "work")
	if err := os.Mkdir(assets, 0o755); err != nil {
		return fmt.Errorf("create asset staging: %w", err)
	}
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
	if err := publishAssets(assets, output); err != nil {
		return err
	}
	return nil
}

func publishAssets(assets, output string) (resultErr error) {
	parentRoot, err := os.OpenRoot(filepath.Dir(output))
	if err != nil {
		return fmt.Errorf("open output parent: %w", err)
	}
	defer func() {
		if err := parentRoot.Close(); resultErr == nil && err != nil {
			resultErr = fmt.Errorf("close output parent: %w", err)
		}
	}()

	outputName := filepath.Base(output)
	outputInfo, err := parentRoot.Lstat(outputName)
	if errors.Is(err, os.ErrNotExist) {
		if err := parentRoot.Mkdir(outputName, 0o755); err != nil {
			return fmt.Errorf("reserve output directory: %w", err)
		}
		outputInfo, err = parentRoot.Lstat(outputName)
	}
	if err != nil {
		return fmt.Errorf("inspect output before publish: %w", err)
	}
	if outputInfo.Mode()&os.ModeSymlink != 0 || !outputInfo.IsDir() {
		return errors.New("output changed before publish: output must be a directory")
	}

	outputRoot, err := parentRoot.OpenRoot(outputName)
	if err != nil {
		return fmt.Errorf("open output directory: %w", err)
	}
	defer func() {
		if err := outputRoot.Close(); resultErr == nil && err != nil {
			resultErr = fmt.Errorf("close output directory: %w", err)
		}
	}()
	rootInfo, err := outputRoot.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect reserved output: %w", err)
	}
	if !os.SameFile(outputInfo, rootInfo) {
		return errors.New("output changed before publish")
	}
	existing, err := fs.ReadDir(outputRoot.FS(), ".")
	if err != nil {
		return fmt.Errorf("read output before publish: %w", err)
	}
	if len(existing) != 0 {
		return errors.New("output changed before publish: output directory is not empty")
	}

	entries, err := os.ReadDir(assets)
	if err != nil {
		return fmt.Errorf("read staged assets: %w", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect staged asset %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("staged asset %s is not a regular file", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(assets, entry.Name()))
		if err != nil {
			return fmt.Errorf("read staged asset %s: %w", entry.Name(), err)
		}
		file, err := outputRoot.OpenFile(entry.Name(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("reserve release asset %s: %w", entry.Name(), err)
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return fmt.Errorf("publish release asset %s: %w", entry.Name(), err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close release asset %s: %w", entry.Name(), err)
		}
	}

	currentInfo, err := parentRoot.Stat(outputName)
	if err != nil {
		return fmt.Errorf("inspect output after publish: %w", err)
	}
	if !os.SameFile(rootInfo, currentInfo) {
		return errors.New("output changed during publish")
	}
	published, err := fs.ReadDir(outputRoot.FS(), ".")
	if err != nil {
		return fmt.Errorf("read output after publish: %w", err)
	}
	if len(published) != len(entries) {
		return errors.New("output changed during publish")
	}
	for index := range entries {
		if published[index].Name() != entries[index].Name() {
			return errors.New("output changed during publish")
		}
	}
	return nil
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
	if len(entries) != 0 {
		return errors.New("output directory is not empty")
	}
	return nil
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
