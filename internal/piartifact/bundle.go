// Package piartifact validates the portable Pi release envelope.
package piartifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	PackageVersion = "0.1.0"
	maxCompressed  = 64 << 20
	maxExpanded    = 96 << 20
	maxInner       = 64 << 20
	maxMetadata    = 64 << 10
)

var (
	versionRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))(?:\.(?:(?:0|[1-9][0-9]*)|(?:[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	hex40RE   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hex64RE   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Release struct {
	ReleaseVersion string `json:"releaseVersion"`
	Commit         string `json:"commit"`
	PackageVersion string `json:"packageVersion"`
	SourceSHA256   string `json:"sourceSHA256"`
}

// Bundle contains the validated inner package files and release metadata.
type Bundle struct {
	Inner, Provenance, Checksums []byte
	Release                      Release
}

func innerName() string { return "vgxness-pi-" + PackageVersion + ".tgz" }

func Filename(version string) (string, error) {
	if !versionRE.MatchString(version) {
		return "", errors.New("invalid release version")
	}
	return "vgxness-pi_" + strings.TrimPrefix(version, "v") + "_portable.tar.gz", nil
}

func Encode(bundle Bundle) ([]byte, error) {
	if err := validate(bundle); err != nil {
		return nil, err
	}
	release, err := json.Marshal(bundle.Release)
	if err != nil {
		return nil, fmt.Errorf("encode release metadata: %w", err)
	}
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, member := range []struct {
		name string
		data []byte
	}{
		{innerName(), bundle.Inner},
		{"PROVENANCE.json", bundle.Provenance},
		{"SHA256SUMS", bundle.Checksums},
		{"RELEASE.json", release},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: member.name, Mode: 0o644, Size: int64(len(member.data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(member.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if output.Len() > maxCompressed {
		return nil, errors.New("bundle exceeds compressed size limit")
	}
	return output.Bytes(), nil
}

func Decode(data []byte) (Bundle, error) {
	if len(data) > maxCompressed {
		return Bundle{}, errors.New("bundle exceeds compressed size limit")
	}
	input := bytes.NewReader(data)
	gz, err := gzip.NewReader(input)
	if err != nil {
		return Bundle{}, fmt.Errorf("invalid bundle gzip: %w", err)
	}
	defer gz.Close()
	gz.Multistream(false)
	limited := &io.LimitedReader{R: gz, N: maxExpanded + 1}
	tr := tar.NewReader(limited)
	files := make(map[string][]byte, 4)
	var expanded int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Bundle{}, fmt.Errorf("invalid bundle tar: %w", err)
		}
		if err := validHeader(header, files, expanded); err != nil {
			return Bundle{}, err
		}
		limit := maxMetadata
		if header.Name == innerName() {
			limit = maxInner
		}
		if header.Size > int64(limit) || expanded+header.Size > maxExpanded {
			return Bundle{}, errors.New("bundle exceeds expanded size limit")
		}
		member, err := readExact(tr, header.Size)
		if err != nil {
			return Bundle{}, fmt.Errorf("invalid bundle member %q: %w", header.Name, err)
		}
		expanded += header.Size
		files[header.Name] = member
	}
	rest, err := io.ReadAll(limited)
	if err != nil {
		return Bundle{}, fmt.Errorf("invalid gzip footer: %w", err)
	}
	if limited.N == 0 {
		return Bundle{}, errors.New("bundle exceeds expanded size limit")
	}
	if len(rest) != 0 {
		return Bundle{}, errors.New("bundle has trailing tar data")
	}
	if err := gz.Close(); err != nil {
		return Bundle{}, fmt.Errorf("invalid gzip footer: %w", err)
	}
	if input.Len() != 0 {
		return Bundle{}, errors.New("bundle has trailing gzip data")
	}
	if len(files) != 4 {
		return Bundle{}, errors.New("incomplete bundle")
	}
	release, err := decodeRelease(files["RELEASE.json"])
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{Inner: files[innerName()], Provenance: files["PROVENANCE.json"], Checksums: files["SHA256SUMS"], Release: release}
	if err := validate(bundle); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func validHeader(header *tar.Header, files map[string][]byte, expanded int64) error {
	if header == nil || header.Typeflag != tar.TypeReg || header.Linkname != "" || header.Size < 0 {
		return errors.New("invalid bundle member type")
	}
	allowed := map[string]bool{innerName(): true, "PROVENANCE.json": true, "SHA256SUMS": true, "RELEASE.json": true}
	if !allowed[header.Name] || strings.Contains(header.Name, "..") || strings.Contains(header.Name, "/") {
		return errors.New("invalid bundle member name")
	}
	if _, exists := files[header.Name]; exists {
		return errors.New("duplicate bundle member")
	}
	if expanded > maxExpanded {
		return errors.New("bundle exceeds expanded size limit")
	}
	return nil
}

func readExact(reader io.Reader, size int64) ([]byte, error) {
	if size < 0 || size > maxInner {
		return nil, errors.New("member size limit")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}
	return data, nil
}

func validate(bundle Bundle) error {
	if _, err := Filename(bundle.Release.ReleaseVersion); err != nil {
		return err
	}
	if bundle.Release.PackageVersion != PackageVersion || !hex40RE.MatchString(bundle.Release.Commit) || !hex64RE.MatchString(bundle.Release.SourceSHA256) {
		return errors.New("invalid release metadata")
	}
	if len(bundle.Inner) == 0 || len(bundle.Inner) > maxInner || len(bundle.Provenance) > maxMetadata || len(bundle.Checksums) > maxMetadata {
		return errors.New("bundle member exceeds size limit")
	}
	provenance, err := strictStringObject(bundle.Provenance, "name", "version", "sourceSHA256", "provenance")
	if err != nil || provenance["name"] != "@vgxness/pi" || provenance["version"] != PackageVersion || provenance["sourceSHA256"] != bundle.Release.SourceSHA256 || provenance["provenance"] != "local source snapshot; unpublished" {
		return errors.New("provenance source identity mismatch")
	}
	sum := sha256.Sum256(bundle.Inner)
	want := hex.EncodeToString(sum[:]) + "  " + innerName() + "\n"
	if !bytes.Equal(bundle.Checksums, []byte(want)) {
		return errors.New("inner checksum mismatch")
	}
	return nil
}

func decodeRelease(data []byte) (Release, error) {
	object, err := strictStringObject(data, "releaseVersion", "commit", "packageVersion", "sourceSHA256")
	if err != nil {
		return Release{}, errors.New("invalid release metadata")
	}
	return Release{ReleaseVersion: object["releaseVersion"], Commit: object["commit"], PackageVersion: object["packageVersion"], SourceSHA256: object["sourceSHA256"]}, nil
}

func strictStringObject(data []byte, fields ...string) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid object")
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	object := make(map[string]string, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return nil, errors.New("unknown object field")
		}
		if _, duplicate := object[key]; duplicate {
			return nil, errors.New("duplicate object field")
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		object[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("invalid object trailing data")
	}
	if len(object) != len(fields) {
		return nil, errors.New("incomplete object")
	}
	return object, nil
}
