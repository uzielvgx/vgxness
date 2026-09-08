package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const piVersion = "0.1.0"

func RunPi(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vgxness-release pi", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var output string
	flags.StringVar(&output, "output", "", "new local output directory")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || output == "" {
		fmt.Fprintln(stderr, "usage: vgxness-release pi --output <new-directory>")
		return 2
	}
	repository, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "pi release: resolve repository")
		return 1
	}
	if err := PackagePi(ctx, repository, output); err != nil {
		fmt.Fprintf(stderr, "pi release: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "output=%s\n", output)
	return 0
}

// PackagePi writes npm-compatible package tarballs only to a new local output directory.
func PackagePi(ctx context.Context, repository, output string) (resultErr error) {
	repository, err := filepath.Abs(repository)
	if err != nil {
		return errors.New("invalid repository")
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return errors.New("invalid output directory")
	}
	if err := validateOutput(output); err != nil {
		return err
	}
	if rel, err := filepath.Rel(repository, output); err == nil && (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("output directory must be outside repository")
	}
	if err := requireRegular(filepath.Join(repository, "LICENSE")); err != nil {
		return err
	}
	if err := requireRegular(filepath.Join(repository, "packages", "pi", "package.json")); err != nil {
		return err
	}
	if err := validatePiSourceMetadata(repository); err != nil {
		return err
	}
	if err := assertNoLinkAncestors(repository); err != nil {
		return err
	}
	if err := assertNoLinkAncestors(output); err != nil {
		return err
	}
	before, err := piSourceIdentity(repository)
	if err != nil {
		return err
	}
	parent := filepath.Dir(output)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return errors.New("output parent must be an existing directory")
	}
	publicationAncestors, err := capturePathIdentities(repository, parent)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".vgxness-pi-release-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	keep := true
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		} else if resultErr != nil {
			resultErr = fmt.Errorf("%w; staging retained at %s", resultErr, stage)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := packagePiMain(repository, stage); err != nil {
		return err
	}
	after, err := piSourceIdentity(repository)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("source changed during Pi release assembly")
	}
	data, _ := json.Marshal(map[string]string{"name": "@vgxness/pi", "version": piVersion, "sourceSHA256": before, "provenance": "local source snapshot; unpublished"})
	if err := os.WriteFile(filepath.Join(stage, "PROVENANCE.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	names, err := piRegularNames(stage)
	if err != nil {
		return err
	}
	if err := writeChecksums(stage, names); err != nil {
		return err
	}
	if err := verifyPiRelease(stage, before, repository); err != nil {
		return err
	}
	if err := revalidatePathIdentities(publicationAncestors); err != nil || assertNoLinkAncestors(output) != nil {
		if err != nil {
			return err
		}
		return errors.New("linked output ancestor")
	}
	if after, err := piSourceIdentity(repository); err != nil || after != before {
		if err != nil {
			return err
		}
		return errors.New("source changed before Pi release publication")
	}
	if err := assertNoLinkAncestors(stage); err != nil {
		return err
	}
	published, err := publishAssetsWithHooks(stage, output, defaultDurabilityHooks().publish, defaultDurabilityHooks().syncDirectory)
	if err != nil {
		if published {
			keep = false
		}
		return fmt.Errorf("publish local output without overwrite: %w", err)
	}
	if err := verifyPiRelease(output, before, repository); err != nil {
		return fmt.Errorf("published output readback: %w", err)
	}
	keep = false
	return nil
}

func validatePiSourceMetadata(repository string) error {
	main, err := readJSON(filepath.Join(repository, "packages", "pi", "package.json"))
	if err != nil {
		return err
	}
	if main["name"] != "@vgxness/pi" || main["version"] != piVersion {
		return errors.New("invalid Pi package identity")
	}
	for _, lifecycle := range []string{"preinstall", "install", "postinstall"} {
		if _, ok := main[lifecycle]; ok {
			return errors.New("Pi lifecycle scripts are forbidden")
		}
		if scripts, ok := main["scripts"].(map[string]any); ok {
			if _, found := scripts[lifecycle]; found {
				return errors.New("Pi lifecycle scripts are forbidden")
			}
		}
	}
	return validatePortablePiMetadata(main)
}

func validatePortablePiMetadata(main map[string]any) error {
	if main["name"] != "@vgxness/pi" || main["version"] != piVersion || main["type"] != "module" {
		return errors.New("invalid Pi package identity")
	}
	for _, name := range []string{"optionalDependencies", "dependencies", "bin", "os", "cpu"} {
		if _, ok := main[name]; ok {
			return errors.New("Pi package must be portable and host-loaded")
		}
	}
	pi, ok := main["pi"].(map[string]any)
	if !ok {
		return errors.New("invalid Pi extension contract")
	}
	extensions, ok := pi["extensions"].([]any)
	if !ok || len(extensions) != 1 || extensions[0] != "./src/extension.ts" {
		return errors.New("invalid Pi extension contract")
	}
	engines, ok := main["engines"].(map[string]any)
	if !ok || engines["node"] != ">=22.19.0" {
		return errors.New("invalid Pi Node engine")
	}
	peers, ok := main["peerDependencies"].(map[string]any)
	if !ok || len(peers) != 2 || peers["typebox"] != "1.3.7" || peers["@earendil-works/pi-coding-agent"] != "^0.84.4" {
		return errors.New("invalid Pi host dependency contract")
	}
	return nil
}

func packagePiMain(repository, stage string) error {
	metadata, err := readJSON(filepath.Join(repository, "packages", "pi", "package.json"))
	if err != nil {
		return err
	}
	delete(metadata, "private")
	delete(metadata, "scripts") // test/build helpers are source-tree only.
	files := []archiveFile{{"package.json", mustJSON(metadata), 0o644}}
	for _, dir := range []string{"src", "resources"} {
		root := filepath.Join(repository, "packages", "pi", dir)
		entries, err := regularTree(root)
		if err != nil {
			return err
		}
		for _, rel := range entries {
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				return err
			}
			files = append(files, archiveFile{filepath.ToSlash(filepath.Join(dir, rel)), data, 0o644})
		}
	}
	// The extension uses this read-only fallback only when the managed shared
	// catalog is unavailable. It is copied into the package, never installed.
	skillsRoot := filepath.Join(repository, "internal", "skills", "pack")
	skills, err := regularTree(skillsRoot)
	if err != nil {
		return err
	}
	for _, rel := range skills {
		data, err := os.ReadFile(filepath.Join(skillsRoot, rel))
		if err != nil {
			return err
		}
		files = append(files, archiveFile{filepath.ToSlash(filepath.Join("resources", "skills", rel)), data, 0o644})
	}
	license, err := os.ReadFile(filepath.Join(repository, "LICENSE"))
	if err != nil {
		return err
	}
	files = append(files, archiveFile{"LICENSE", license, 0o644})
	return writeNpmTarball(filepath.Join(stage, "vgxness-pi-"+piVersion+".tgz"), files)
}

func writeNpmTarball(path string, files []archiveFile) (resultErr error) {
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
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	for _, file := range files {
		if strings.Contains(file.name, "..") || file.name == "" {
			return errors.New("invalid package path")
		}
		h := &tar.Header{Name: "package/" + file.name, Mode: int64(file.mode.Perm()), Size: int64(len(file.data)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(file.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func verifyPiRelease(directory, source, repository string) error {
	if err := verifyReleaseDirectory(directory); err != nil {
		return err
	}
	names, err := piRegularNames(directory)
	if err != nil {
		return err
	}
	if len(names) != 1 {
		return fmt.Errorf("release has %d tarballs, want 1", len(names))
	}
	for _, name := range names {
		if err := verifyPiTarball(filepath.Join(directory, name)); err != nil {
			return fmt.Errorf("readback %s: %w", name, err)
		}
	}
	if err := verifyPiProvenance(filepath.Join(directory, "PROVENANCE.json"), source); err != nil {
		return err
	}
	checksums, err := os.ReadFile(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		return err
	}
	gotChecksums, err := parsePiChecksums(string(checksums))
	if err != nil {
		return err
	}
	if len(gotChecksums) != len(names) {
		return errors.New("checksum set mismatch")
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if gotChecksums[name] != fmt.Sprintf("%x", sha256.Sum256(data)) {
			return fmt.Errorf("checksum missing %s", name)
		}
	}
	return verifyPiMainSource(filepath.Join(directory, "vgxness-pi-"+piVersion+".tgz"), repository)
}

func verifyReleaseDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	expected := map[string]bool{"PROVENANCE.json": true, "SHA256SUMS": true, "vgxness-pi-" + piVersion + ".tgz": true}
	if len(entries) != len(expected) {
		return errors.New("release has unexpected files")
	}
	for _, entry := range entries {
		if !expected[entry.Name()] || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return errors.New("release has unexpected files")
		}
	}
	return nil
}

func verifyPiProvenance(path, source string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	value, err := strictStringObject(data)
	if err != nil || len(value) != 4 || value["name"] != "@vgxness/pi" || value["version"] != piVersion || value["sourceSHA256"] != source || value["provenance"] != "local source snapshot; unpublished" {
		return errors.New("provenance source identity mismatch")
	}
	return nil
}

func parsePiChecksums(data string) (map[string]string, error) {
	if !strings.HasSuffix(data, "\n") {
		return nil, errors.New("invalid checksum layout")
	}
	checksums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || len(parts[0]) != sha256.Size*2 || !isLowerHex(parts[0]) || parts[1] == "" {
			return nil, errors.New("invalid checksum layout")
		}
		if _, exists := checksums[parts[1]]; exists {
			return nil, errors.New("duplicate checksum")
		}
		checksums[parts[1]] = parts[0]
	}
	return checksums, nil
}

func strictStringObject(data []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid object")
	}
	value := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("invalid object key")
		}
		if _, exists := value[name]; exists {
			return nil, errors.New("duplicate object key")
		}
		var field string
		if err := decoder.Decode(&field); err != nil {
			return nil, err
		}
		value[name] = field
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid object")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("invalid object")
	}
	return value, nil
}

func isLowerHex(value string) bool {
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func verifyPiMainSource(path, repository string) error {
	entries, err := readPiTarball(path)
	if err != nil {
		return err
	}
	expected, err := piMainSources(repository)
	if err != nil {
		return err
	}
	if len(entries) != len(expected) {
		return errors.New("main package source set mismatch")
	}
	for name, want := range expected {
		got, ok := entries[name]
		if !ok || got.mode.Perm() != want.mode || !bytes.Equal(got.data, want.data) {
			return fmt.Errorf("main package source mismatch: %s", name)
		}
	}
	return nil
}

func piMainSources(repository string) (map[string]archiveFile, error) {
	metadata, err := readJSON(filepath.Join(repository, "packages", "pi", "package.json"))
	if err != nil {
		return nil, err
	}
	delete(metadata, "private")
	delete(metadata, "scripts")
	files := map[string]archiveFile{"package/package.json": {name: "package/package.json", data: mustJSON(metadata), mode: 0o644}}
	for _, source := range []struct{ root, prefix string }{{filepath.Join(repository, "packages", "pi", "src"), "src"}, {filepath.Join(repository, "packages", "pi", "resources"), "resources"}, {filepath.Join(repository, "internal", "skills", "pack"), "resources/skills"}} {
		entries, err := regularTree(source.root)
		if err != nil {
			return nil, err
		}
		for _, rel := range entries {
			data, err := os.ReadFile(filepath.Join(source.root, rel))
			if err != nil {
				return nil, err
			}
			name := "package/" + filepath.ToSlash(filepath.Join(source.prefix, rel))
			files[name] = archiveFile{name: name, data: data, mode: 0o644}
		}
	}
	license, err := os.ReadFile(filepath.Join(repository, "LICENSE"))
	if err != nil {
		return nil, err
	}
	files["package/LICENSE"] = archiveFile{name: "package/LICENSE", data: license, mode: 0o644}
	return files, nil
}
func verifyPiTarball(path string) error {
	name := filepath.Base(path)
	entries, err := readPiTarball(path)
	if err != nil {
		return err
	}
	if name != "vgxness-pi-"+piVersion+".tgz" {
		return errors.New("unexpected tarball name")
	}
	metadata, ok := entries["package/package.json"]
	if !ok {
		return errors.New("tarball lacks package metadata")
	}
	var pkg map[string]any
	if json.Unmarshal(metadata.data, &pkg) != nil || pkg["name"] != "@vgxness/pi" || pkg["version"] != piVersion {
		return errors.New("main package metadata mismatch")
	}
	if err := validatePortablePiMetadata(pkg); err != nil {
		return err
	}
	for name, entry := range entries {
		if entry.mode.Perm() != 0o644 || strings.Contains(name, "/node_modules/") || strings.Contains(name, "/bin/") || strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".exe") {
			return errors.New("non-portable Pi package member")
		}
	}
	migrations := 0
	for name := range entries {
		if strings.HasPrefix(name, "package/resources/migrations/") && strings.HasSuffix(name, ".sql") {
			migrations++
		}
	}
	if migrations != 23 {
		return errors.New("Pi package must contain exactly 23 SQL migrations")
	}
	var manifest struct {
		Version    int
		Migrations []struct {
			Version      int
			File, SHA256 string
		}
	}
	if json.Unmarshal(entries["package/resources/migrations/manifest.json"].data, &manifest) != nil || manifest.Version != 23 || len(manifest.Migrations) != 23 {
		return errors.New("invalid Pi migration manifest")
	}
	seen := map[string]bool{}
	for index, migration := range manifest.Migrations {
		entry, exists := entries["package/resources/migrations/"+migration.File]
		if migration.Version != index+1 || filepath.Base(migration.File) != migration.File || !strings.HasSuffix(migration.File, ".sql") || seen[migration.File] || !exists || fmt.Sprintf("%x", sha256.Sum256(entry.data)) != migration.SHA256 {
			return errors.New("Pi migration hash mismatch")
		}
		seen[migration.File] = true
	}
	for _, name := range []string{"package/src/extension.ts", "package/src/probe.ts", "package/resources/migrations/manifest.json"} {
		if _, ok := entries[name]; !ok {
			return errors.New("Pi package missing runtime resource")
		}
	}
	if _, ok := entries["package/LICENSE"]; !ok {
		return errors.New("main package lacks license")
	}
	if _, ok := entries["package/resources/prompts/manager.md"]; !ok {
		return errors.New("main package lacks prompt resources")
	}
	if _, ok := entries["package/resources/skills/sdd-lifecycle/SKILL.md"]; !ok {
		return errors.New("main package lacks skill fallback")
	}
	return nil
}

type piTarEntry struct {
	data []byte
	mode os.FileMode
}

func readPiTarball(path string) (map[string]piTarEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := map[string]piTarEntry{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(h.Name, "package/") || strings.Contains(h.Name, "..") || h.Typeflag != tar.TypeReg || h.Size < 0 || h.Mode&^0o777 != 0 {
			return nil, errors.New("invalid tarball entry")
		}
		if _, exists := entries[h.Name]; exists {
			return nil, errors.New("duplicate tarball entry")
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		entries[h.Name] = piTarEntry{data, os.FileMode(h.Mode)}
	}
	return entries, nil
}
func assertNoLinkAncestors(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := filepath.VolumeName(absolute) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(absolute, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			break
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked path ancestor: %s", current)
		}
	}
	return nil
}

type pathIdentity struct {
	path string
	info os.FileInfo
}

func capturePathIdentities(paths ...string) ([]pathIdentity, error) {
	identities := make([]pathIdentity, 0, len(paths))
	for _, path := range paths {
		if err := assertNoLinkAncestors(path); err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		identities = append(identities, pathIdentity{path: path, info: info})
	}
	return identities, nil
}

func revalidatePathIdentities(identities []pathIdentity) error {
	for _, identity := range identities {
		if err := assertNoLinkAncestors(identity.path); err != nil {
			return err
		}
		info, err := os.Stat(identity.path)
		if err != nil || !os.SameFile(info, identity.info) {
			return errors.New("publication ancestor changed")
		}
	}
	return nil
}
func piRegularNames(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("linked staged artifact")
		}
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".tgz") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
func requireRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("required source is not a regular file: %s", path)
	}
	return nil
}
func readJSON(path string) (map[string]any, error) {
	if err := requireRegular(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}
func mustJSON(value any) []byte { b, _ := json.Marshal(value); return append(b, '\n') }
func regularTree(root string) ([]string, error) {
	out := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked source: %s", path)
		}
		if d.Type().IsRegular() {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}
func piSourceIdentity(root string) (string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	data, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("enumerate source identity: %w", err)
	}
	files := strings.FieldsFunc(string(data), func(r rune) bool { return r == 0 })
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		// The coverage workflow writes this repository-root report while package
		// tests may assemble Pi concurrently. It is not package or build input.
		if rel == "coverage.out" {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, e := os.Lstat(path)
		if errors.Is(e, os.ErrNotExist) {
			fmt.Fprintf(h, "%s\x00ABSENT\n", rel)
			continue
		}
		if e != nil {
			return "", e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("non-regular source: %s", rel)
		}
		contents, e := os.ReadFile(path)
		if e != nil {
			return "", e
		}
		sum := sha256.Sum256(contents)
		fmt.Fprintf(h, "%s\x00%x\n", rel, sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
