package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vgxness/vgxness/internal/piartifact"
)

// PackagePiBundle wraps the offline Pi package in a no-overwrite portable release.
func PackagePiBundle(ctx context.Context, repository, output, releaseVersion, commit string) (resultErr error) {
	repository, err := filepath.Abs(repository)
	if err != nil {
		return errors.New("invalid repository")
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return errors.New("invalid output directory")
	}
	filename, err := piartifact.Filename(releaseVersion)
	if err != nil {
		return err
	}
	if !commitPattern.MatchString(commit) {
		return errors.New("commit must be 40 lowercase hexadecimal characters")
	}
	if err := validateOutput(output); err != nil {
		return err
	}
	if rel, err := filepath.Rel(repository, output); err == nil && (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("output directory must be outside repository")
	}
	if err := assertNoLinkAncestors(repository); err != nil {
		return err
	}
	if err := assertNoLinkAncestors(output); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	head, err := repositoryHEAD(ctx, repository)
	if err != nil {
		return err
	}
	if head != commit {
		return errors.New("repository HEAD does not match requested commit")
	}
	before, err := piSourceIdentity(repository)
	if err != nil {
		return err
	}
	parent := filepath.Dir(output)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return errors.New("output parent must be an existing directory")
	}
	ancestors, err := capturePathIdentities(repository, parent)
	if err != nil {
		return err
	}
	tempParent, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return fmt.Errorf("resolve bundle temporary directory: %w", err)
	}
	work, err := os.MkdirTemp(tempParent, ".vgxness-pi-bundle-inner-")
	if err != nil {
		return fmt.Errorf("create inner temporary directory: %w", err)
	}
	retainWork := false
	defer func() {
		if !retainWork {
			_ = os.RemoveAll(work)
		} else if resultErr != nil {
			resultErr = fmt.Errorf("%w; inner staging retained at %s", resultErr, work)
		}
	}()
	innerOutput := filepath.Join(work, "inner")
	if err := PackagePi(ctx, repository, innerOutput); err != nil {
		retainWork = true
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	inner, err := readBundleRegular(filepath.Join(innerOutput, "vgxness-pi-"+piVersion+".tgz"))
	if err != nil {
		return err
	}
	provenance, err := readBundleRegular(filepath.Join(innerOutput, "PROVENANCE.json"))
	if err != nil {
		return err
	}
	checksums, err := readBundleRegular(filepath.Join(innerOutput, "SHA256SUMS"))
	if err != nil {
		return err
	}
	provenanceFields, err := strictStringObject(provenance)
	if err != nil || len(provenanceFields) != 4 {
		return errors.New("invalid inner provenance")
	}
	if provenanceFields["sourceSHA256"] != before {
		return errors.New("inner provenance source identity mismatch")
	}
	bundle := piartifact.Bundle{Inner: inner, Provenance: provenance, Checksums: checksums, Release: piartifact.Release{ReleaseVersion: releaseVersion, Commit: commit, PackageVersion: piartifact.PackageVersion, SourceSHA256: before}}
	archive, err := piartifact.Encode(bundle)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := piSourceIdentity(repository)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("source changed during Pi bundle assembly")
	}
	afterHead, err := repositoryHEAD(ctx, repository)
	if err != nil {
		return err
	}
	if afterHead != commit {
		return errors.New("repository HEAD changed during Pi bundle assembly")
	}
	stage, err := os.MkdirTemp(parent, ".vgxness-pi-bundle-")
	if err != nil {
		return fmt.Errorf("create bundle staging directory: %w", err)
	}
	keep := true
	publishedOutput := false
	defer func() {
		if publishedOutput && resultErr != nil {
			resultErr = fmt.Errorf("%w; published output retained at %s", resultErr, output)
		}
	}()
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		} else if resultErr != nil {
			resultErr = fmt.Errorf("%w; staging retained at %s", resultErr, stage)
		}
	}()
	asset := filepath.Join(stage, filename)
	if err := os.WriteFile(asset, archive, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if err := os.WriteFile(filepath.Join(stage, "SHA256SUMS"), []byte(hex.EncodeToString(sum[:])+"  "+filename+"\n"), 0o644); err != nil {
		return err
	}
	hooks := defaultDurabilityHooks()
	if err := hooks.syncFile(asset); err != nil {
		return fmt.Errorf("sync bundle asset: %w", err)
	}
	if err := hooks.syncFile(filepath.Join(stage, "SHA256SUMS")); err != nil {
		return fmt.Errorf("sync bundle checksum: %w", err)
	}
	if err := hooks.syncDirectory(stage); err != nil {
		return fmt.Errorf("sync bundle staging: %w", err)
	}
	if err := revalidatePathIdentities(ancestors); err != nil || assertNoLinkAncestors(output) != nil {
		if err != nil {
			return err
		}
		return errors.New("linked output ancestor")
	}
	latestSource, err := piSourceIdentity(repository)
	if err != nil {
		return err
	}
	if latestSource != before {
		return errors.New("source changed before Pi bundle publication")
	}
	latestHead, err := repositoryHEAD(ctx, repository)
	if err != nil {
		return err
	}
	if latestHead != commit {
		return errors.New("repository HEAD changed before Pi bundle publication")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	published, err := publishAssetsWithHooks(stage, output, hooks.publish, hooks.syncDirectory)
	publishedOutput = published
	if err != nil {
		if published {
			keep = false
		}
		return fmt.Errorf("publish Pi bundle without overwrite: %w", err)
	}
	keep = false
	readback, err := readBundleRegular(filepath.Join(output, filename))
	if err != nil {
		return fmt.Errorf("published Pi bundle readback: %w", err)
	}
	decoded, err := piartifact.Decode(readback)
	if err != nil {
		return fmt.Errorf("published Pi bundle validation: %w", err)
	}
	if decoded.Release != bundle.Release || !bytes.Equal(readback, archive) {
		return errors.New("published Pi bundle metadata or content mismatch")
	}
	outerChecksums, err := readBundleRegular(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		return fmt.Errorf("published Pi bundle checksum readback: %w", err)
	}
	if !bytes.Equal(outerChecksums, []byte(hex.EncodeToString(sum[:])+"  "+filename+"\n")) {
		return errors.New("published Pi bundle checksum mismatch")
	}
	return nil
}

func readBundleRegular(path string) ([]byte, error) {
	if err := requireRegular(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func repositoryHEAD(ctx context.Context, repository string) (string, error) {
	output, err := exec.CommandContext(ctx, "git", "-C", repository, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("read repository HEAD: %w", err)
	}
	head := strings.TrimSpace(string(output))
	if !commitPattern.MatchString(head) {
		return "", errors.New("invalid repository HEAD")
	}
	return head, nil
}
