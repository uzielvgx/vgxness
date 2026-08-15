package selfinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vgxness/vgxness/internal/launcher"
)

const (
	gcJournalName     = ".gc-recovery.json"
	gcJournalNextName = ".gc-recovery.next.json"
	gcInventoryMax    = 1024
	gcJournalMaximum  = 4 << 10
)

// GCResult is the action-specific GC audit result. Preview and apply return a
// plan with its ordered candidates and retained versions; apply may also return
// an ordered prefix of observed deletions with an error. Recover returns only
// recovered versions. Callers must not infer fields for another action.
type GCResult struct {
	State State

	// PlanSHA256, Candidates, and Retained describe a preview or apply plan.
	PlanSHA256 string
	Candidates []string
	Retained   []string
	// Deleted is the ordered observed prefix of Candidates, including on apply error.
	Deleted []string
	// Recovered is populated only by recover.
	Recovered []string
	// Changed reports whether the requested action made an observable change.
	Changed bool
}

type gcState string

const (
	gcStatePrepared gcState = "prepared"
	gcStateMoving   gcState = "moving"
	gcStateStaged   gcState = "staged"
	gcStateDeleting gcState = "deleting"
	gcStateDeleted  gcState = "deleted"

	gcSyncPostUnlinkStage     = "post-unlink stage sync"
	gcSyncAfterStageRemoval   = "root sync after stage removal"
	gcSyncAfterJournalRemoval = "final root sync after journal removal"
)

type gcJournal struct {
	SchemaVersion   string  `json:"schemaVersion"`
	State           gcState `json:"state"`
	PlanSHA256      string  `json:"planSha256"`
	CandidateSHA256 string  `json:"candidateSha256"`
	Source          string  `json:"source"`
	Stage           string  `json:"stage"`
}

// GCPreview validates the complete immutable-version inventory without taking
// the install lock or changing the filesystem.
func (service *Service) GCPreview(ctx context.Context, options Options) (GCResult, error) {
	state, err := service.inspect(ctx, options)
	if err != nil {
		return GCResult{}, err
	}
	if state.result.State == StateAbsent {
		return GCResult{}, ErrNoInstallation
	}
	if state.result.State != StateInstalled {
		return GCResult{}, ErrDrift
	}
	anchors, err := openAnchors(state.paths)
	if err != nil {
		return GCResult{}, err
	}
	defer anchors.close()
	if err := gcJournalAbsent(anchors.data); err != nil {
		return GCResult{}, err
	}
	manifestRaw, err := readRegularRoot(anchors.bin, filepath.Base(state.paths.manifest), 64<<10)
	if err != nil || !bytesEqual(manifestRaw, state.manifestRaw) {
		return GCResult{}, ErrDrift
	}
	return gcInventory(ctx, anchors.data, anchors.data.Name(), manifestRaw, state.manifest)
}

// GCApply applies exactly a previously previewed plan. It revalidates the
// anchored roots, manifest bytes, lock, journal, and whole inventory while
// holding the existing install lock before writing any collection evidence.
func (service *Service) GCApply(ctx context.Context, options Options, expectedPlan string) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	preflight, err := service.GCPreview(ctx, options)
	if err != nil {
		return GCResult{}, err
	}
	if expectedPlan != preflight.PlanSHA256 {
		return GCResult{}, ErrStaleGCPlan
	}
	if service.afterGCPreflight != nil {
		if err := service.afterGCPreflight(); err != nil {
			return GCResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	state, err := service.inspect(ctx, options)
	if err != nil || state.result.State != StateInstalled {
		return GCResult{}, ErrDrift
	}
	anchors, err := openAnchors(state.paths)
	if err != nil {
		return GCResult{}, err
	}
	defer anchors.close()
	lockFile, err := anchors.data.OpenFile(".install.lock", os.O_RDWR, 0)
	if err != nil {
		return GCResult{}, ErrDrift
	}
	lockInfo, err := lockFile.Stat()
	if err != nil || !lockInfo.Mode().IsRegular() {
		return GCResult{}, ErrDrift
	}
	lock, err := acquireFile(ctx, lockFile)
	if err != nil {
		return GCResult{}, err
	}
	defer lock.release()
	if !anchorsStillNamed(anchors, state.paths) {
		return GCResult{}, ErrDrift
	}
	if err := gcJournalAbsent(anchors.data); err != nil {
		return GCResult{}, err
	}
	current, err := service.inspect(ctx, options)
	if err != nil || current.result.State != StateInstalled || !anchorsStillNamed(anchors, state.paths) {
		return GCResult{}, ErrDrift
	}
	manifestRaw, err := readRegularRoot(anchors.bin, filepath.Base(state.paths.manifest), 64<<10)
	if err != nil || !bytesEqual(manifestRaw, current.manifestRaw) {
		return GCResult{}, ErrDrift
	}
	plan, err := gcInventory(ctx, anchors.data, anchors.data.Name(), manifestRaw, current.manifest)
	if err != nil {
		return GCResult{}, err
	}
	if plan.PlanSHA256 != expectedPlan {
		return GCResult{}, ErrStaleGCPlan
	}
	result := cloneGCResult(plan)
	for _, candidate := range plan.Candidates {
		if err := ctx.Err(); err != nil {
			return gcApplyFailure(result, err)
		}
		validate := func() bool {
			fresh, freshErr := service.inspect(ctx, options)
			if freshErr != nil || fresh.result.State != StateInstalled || fresh.manifest.ActiveSHA256 != current.manifest.ActiveSHA256 || fresh.manifest.PreviousSHA256 != current.manifest.PreviousSHA256 {
				return false
			}
			if err := service.afterGCSemanticCheck(); err != nil {
				return false
			}
			if !anchorsStillNamed(anchors, state.paths) || !gcLockStillNamed(anchors.data, lockInfo) {
				return false
			}
			currentRaw, rawErr := readRegularRoot(anchors.bin, filepath.Base(state.paths.manifest), 64<<10)
			return rawErr == nil && bytesEqual(currentRaw, manifestRaw)
		}
		if !validate() {
			return gcApplyFailure(result, ErrDrift)
		}
		unlinked, err := service.gcDelete(ctx, anchors.data, plan.PlanSHA256, candidate, validate)
		if unlinked {
			result.Deleted = append(result.Deleted, candidate)
			result.Changed = true
		}
		if err != nil {
			return gcApplyFailure(result, err)
		}
	}
	return cloneGCResult(result), nil
}

func gcApplyFailure(result GCResult, err error) (GCResult, error) {
	if len(result.Deleted) == 0 {
		return GCResult{}, err
	}
	result.Changed = true
	return cloneGCResult(result), err
}

func gcLockStillNamed(root *os.Root, expected os.FileInfo) bool {
	current, err := root.Lstat(".install.lock")
	return err == nil && current.Mode().IsRegular() && os.SameFile(current, expected)
}

// GCRecover is explicit: normal preview and apply never consume outstanding
// collection evidence. Recovery restores only a verified staged version using
// no-replace publication and never unlinks an executable.
func (service *Service) GCRecover(ctx context.Context, options Options) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	resolved, err := resolvePaths(options)
	if err != nil {
		return GCResult{}, err
	}
	if _, err := os.Lstat(resolved.dataDir); errors.Is(err, os.ErrNotExist) {
		return GCResult{}, ErrNoInstallation
	} else if err != nil {
		return GCResult{}, ErrDrift
	}
	anchors, err := openAnchors(resolved)
	if err != nil {
		return GCResult{}, err
	}
	defer anchors.close()
	lockFile, err := anchors.data.OpenFile(".install.lock", os.O_RDWR, 0)
	if err != nil {
		return GCResult{}, ErrDrift
	}
	lockInfo, err := lockFile.Stat()
	if err != nil || !lockInfo.Mode().IsRegular() {
		_ = lockFile.Close()
		return GCResult{}, ErrDrift
	}
	lock, err := acquireFile(ctx, lockFile)
	if err != nil {
		return GCResult{}, err
	}
	defer lock.release()
	if !anchorsStillNamed(anchors, resolved) {
		return GCResult{}, ErrDrift
	}
	current, err := service.inspect(ctx, options)
	if err != nil || current.result.State != StateInstalled || !anchorsStillNamed(anchors, resolved) {
		return GCResult{}, ErrDrift
	}
	manifestRaw, err := readRegularRoot(anchors.bin, filepath.Base(resolved.manifest), 64<<10)
	if err != nil || !bytesEqual(manifestRaw, current.manifestRaw) {
		return GCResult{}, ErrDrift
	}
	guard := func() error {
		if service.beforeGCRecoveryMutation != nil {
			if err := service.beforeGCRecoveryMutation(); err != nil {
				return errors.Join(ErrGCRecovery, err)
			}
		}
		if !anchorsStillNamed(anchors, resolved) || !gcLockStillNamed(anchors.data, lockInfo) {
			return ErrGCRecovery
		}
		return nil
	}
	return gcRecoverRoot(ctx, anchors.data, guard)
}

func gcInventory(ctx context.Context, root *os.Root, dataDir string, manifestRaw []byte, manifest launcher.Manifest) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	entries, err := readRootDir(root, "versions", gcInventoryMax)
	if err != nil || len(entries) > gcInventoryMax {
		return GCResult{}, ErrDrift
	}
	protected := map[string]struct{}{manifest.ActiveSHA256: {}}
	if manifest.PreviousSHA256 != "" {
		protected[manifest.PreviousSHA256] = struct{}{}
	}
	result := GCResult{State: StateInstalled}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return GCResult{}, err
		}
		name := entry.Name()
		if !validGCDigest(name) || entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || gcVerifyVersion(root, name) != nil {
			return GCResult{}, ErrDrift
		}
		if _, keep := protected[name]; keep {
			result.Retained = append(result.Retained, name)
		} else {
			result.Candidates = append(result.Candidates, name)
		}
	}
	if len(result.Retained) != len(protected) {
		return GCResult{}, ErrDrift
	}
	sort.Strings(result.Retained)
	sort.Strings(result.Candidates)
	result.PlanSHA256 = gcPlanSHA256(dataDir, manifestRaw, manifest, result.Candidates)
	return cloneGCResult(result), nil
}

func gcPlanSHA256(dataDir string, manifestRaw []byte, manifest launcher.Manifest, candidates []string) string {
	dataHash := sha256.Sum256([]byte(filepath.ToSlash(filepath.Clean(dataDir))))
	manifestHash := sha256.Sum256(manifestRaw)
	previous := manifest.PreviousSHA256
	if previous == "" {
		previous = "-"
	}
	var plan strings.Builder
	plan.WriteString("vgxness-self-gc-plan-v1\n")
	plan.WriteString("data-dir-sha256=")
	plan.WriteString(hex.EncodeToString(dataHash[:]))
	plan.WriteByte('\n')
	plan.WriteString("manifest-sha256=")
	plan.WriteString(hex.EncodeToString(manifestHash[:]))
	plan.WriteByte('\n')
	plan.WriteString("active=")
	plan.WriteString(manifest.ActiveSHA256)
	plan.WriteByte('\n')
	plan.WriteString("previous=")
	plan.WriteString(previous)
	plan.WriteByte('\n')
	plan.WriteString(fmt.Sprintf("candidate-count=%d\n", len(candidates)))
	for _, candidate := range candidates {
		plan.WriteString("candidate=")
		plan.WriteString(candidate)
		plan.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(plan.String()))
	return hex.EncodeToString(sum[:])
}

func (service *Service) gcDelete(ctx context.Context, root *os.Root, planSHA256, digest string, validate func() bool) (bool, error) {
	unlinked := false
	err := service.gcDeleteObserved(ctx, root, planSHA256, digest, validate, &unlinked)
	return unlinked, err
}

func (service *Service) gcDeleteObserved(ctx context.Context, root *os.Root, planSHA256, digest string, validate func() bool, unlinked *bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validGCDigest(digest) || gcVerifyVersion(root, digest) != nil {
		return ErrDrift
	}
	stage, err := rootTemporaryName(".gc-stage-")
	if err != nil {
		return err
	}
	journal := gcJournal{SchemaVersion: "v1", State: gcStatePrepared, PlanSHA256: planSHA256, CandidateSHA256: digest, Source: filepath.Join("versions", digest), Stage: stage}
	if err := writeGCJournal(root, journal); err != nil {
		return ErrGCRecovery
	}
	if err := service.afterGC(journal.State); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := advanceGCJournal(root, &journal, gcStateMoving); err != nil {
		return err
	}
	if err := service.afterGC(journal.State); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if !validate() {
		return ErrGCRecovery
	}
	if err := publishRootDirectoryNoReplace(root, journal.Source, journal.Stage); err != nil {
		return ErrGCRecovery
	}
	if err := syncRoot(root); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := advanceGCJournal(root, &journal, gcStateStaged); err != nil {
		return err
	}
	if err := service.afterGC(journal.State); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := advanceGCJournal(root, &journal, gcStateDeleting); err != nil {
		return err
	}
	if err := service.afterGC(journal.State); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if !validate() {
		return ErrGCRecovery
	}
	stageRoot, err := openGCDirectory(root, journal.Stage)
	if err != nil || gcVerifyStage(stageRoot, digest) != nil {
		if stageRoot != nil {
			_ = stageRoot.Close()
		}
		return ErrGCRecovery
	}
	if err := service.afterGCDeleteOpen(); err != nil {
		_ = stageRoot.Close()
		return errors.Join(ErrGCRecovery, err)
	}
	if !validate() {
		_ = stageRoot.Close()
		return ErrGCRecovery
	}
	if err := stageRoot.Remove(executableName()); err != nil {
		_ = stageRoot.Close()
		return ErrGCRecovery
	}
	*unlinked = true
	if err := service.syncGC(gcSyncPostUnlinkStage, stageRoot); err != nil {
		_ = stageRoot.Close()
		return errors.Join(ErrGCRecovery, err)
	}
	empty, err := gcEmptyDirectory(stageRoot)
	_ = stageRoot.Close()
	if err != nil || !empty || gcPathAbsent(root, journal.Source) != nil {
		return ErrGCRecovery
	}
	if err := advanceGCJournal(root, &journal, gcStateDeleted); err != nil {
		return err
	}
	if err := service.afterGC(journal.State); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := root.Remove(journal.Stage); err != nil {
		return ErrGCRecovery
	}
	if err := service.syncGC(gcSyncAfterStageRemoval, root); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := removeGCJournal(root); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	if err := service.syncGC(gcSyncAfterJournalRemoval, root); err != nil {
		return errors.Join(ErrGCRecovery, err)
	}
	return nil
}

func (service *Service) syncGC(point string, root *os.Root) error {
	if service.gcSync != nil {
		return service.gcSync(point, root)
	}
	return syncRoot(root)
}

func (service *Service) afterGC(state gcState) error {
	if service.afterGCJournal != nil {
		return service.afterGCJournal(state)
	}
	return nil
}

func (service *Service) afterGCDeleteOpen() error {
	if service.afterGCDeleteOpened != nil {
		return service.afterGCDeleteOpened()
	}
	return nil
}

func (service *Service) afterGCSemanticCheck() error {
	if service.afterGCSemanticValidation != nil {
		return service.afterGCSemanticValidation()
	}
	return nil
}

func gcRecoverRoot(ctx context.Context, root *os.Root, guard func() error) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	journal, err := readGCJournal(root)
	if errors.Is(err, os.ErrNotExist) {
		return GCResult{State: StateInstalled}, nil
	}
	if err != nil {
		return GCResult{}, ErrGCRecovery
	}
	sourceInfo, sourceErr := root.Lstat(journal.Source)
	stageInfo, stageErr := root.Lstat(journal.Stage)
	sourceExact := sourceErr == nil && sourceInfo.IsDir() && sourceInfo.Mode()&os.ModeSymlink == 0 && gcVerifyVersion(root, journal.CandidateSHA256) == nil
	stageExact := stageErr == nil && stageInfo.IsDir() && stageInfo.Mode()&os.ModeSymlink == 0 && gcVerifyStageAt(root, journal.Stage, journal.CandidateSHA256) == nil
	stageEmpty := stageErr == nil && stageInfo.IsDir() && stageInfo.Mode()&os.ModeSymlink == 0 && gcEmptyAt(root, journal.Stage)
	if sourceErr != nil && !errors.Is(sourceErr, os.ErrNotExist) || stageErr != nil && !errors.Is(stageErr, os.ErrNotExist) {
		return GCResult{}, ErrGCRecovery
	}
	removeJournal := func(recovered string) (GCResult, error) {
		if err := ctx.Err(); err != nil {
			return GCResult{}, errors.Join(ErrGCRecovery, err)
		}
		if err := removeGCJournal(root, guard); err != nil || syncRoot(root) != nil {
			return GCResult{}, ErrGCRecovery
		}
		return GCResult{State: StateInstalled, Recovered: []string{recovered}, Changed: true}, nil
	}
	restore := func() (GCResult, error) {
		if !stageExact || sourceErr == nil || !errors.Is(sourceErr, os.ErrNotExist) {
			return GCResult{}, ErrGCRecovery
		}
		if err := ctx.Err(); err != nil {
			return GCResult{}, errors.Join(ErrGCRecovery, err)
		}
		if err := guard(); err != nil {
			return GCResult{}, err
		}
		if err := publishRootDirectoryNoReplace(root, journal.Stage, journal.Source); err != nil || syncRoot(root) != nil {
			return GCResult{}, ErrGCRecovery
		}
		return removeJournal(journal.CandidateSHA256)
	}
	switch journal.State {
	case gcStatePrepared:
		if sourceExact && errors.Is(stageErr, os.ErrNotExist) {
			return removeJournal(journal.CandidateSHA256)
		}
	case gcStateMoving:
		if sourceExact && errors.Is(stageErr, os.ErrNotExist) {
			return removeJournal(journal.CandidateSHA256)
		}
		if errors.Is(sourceErr, os.ErrNotExist) && stageExact {
			return restore()
		}
	case gcStateStaged:
		if sourceExact && errors.Is(stageErr, os.ErrNotExist) {
			return removeJournal(journal.CandidateSHA256)
		}
		if errors.Is(sourceErr, os.ErrNotExist) && stageExact {
			return restore()
		}
	case gcStateDeleting:
		if sourceExact && errors.Is(stageErr, os.ErrNotExist) {
			return removeJournal(journal.CandidateSHA256)
		}
		if errors.Is(sourceErr, os.ErrNotExist) && stageExact {
			return restore()
		}
		if errors.Is(sourceErr, os.ErrNotExist) && stageEmpty {
			if err := ctx.Err(); err != nil {
				return GCResult{}, errors.Join(ErrGCRecovery, err)
			}
			if err := advanceGCJournal(root, &journal, gcStateDeleted, guard); err != nil {
				return GCResult{}, err
			}
			if err := ctx.Err(); err != nil {
				return GCResult{}, errors.Join(ErrGCRecovery, err)
			}
			if err := guard(); err != nil {
				return GCResult{}, err
			}
			if err := root.Remove(journal.Stage); err != nil || syncRoot(root) != nil {
				return GCResult{}, ErrGCRecovery
			}
			return removeJournal(journal.CandidateSHA256)
		}
	case gcStateDeleted:
		if errors.Is(sourceErr, os.ErrNotExist) && (errors.Is(stageErr, os.ErrNotExist) || stageEmpty) {
			if stageEmpty {
				if err := ctx.Err(); err != nil {
					return GCResult{}, errors.Join(ErrGCRecovery, err)
				}
				if err := guard(); err != nil {
					return GCResult{}, err
				}
				if err := root.Remove(journal.Stage); err != nil || syncRoot(root) != nil {
					return GCResult{}, ErrGCRecovery
				}
			}
			return removeJournal(journal.CandidateSHA256)
		}
	}
	return GCResult{}, ErrGCRecovery
}

func gcJournalAbsent(root *os.Root) error {
	for _, name := range []string{gcJournalName, gcJournalNextName} {
		_, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		return ErrGCRecovery
	}
	return nil
}

func gcVerifyVersion(root *os.Root, digest string) error {
	return gcVerifyStageAt(root, filepath.Join("versions", digest), digest)
}

func gcVerifyStageAt(root *os.Root, name, digest string) error {
	directory, err := openGCDirectory(root, name)
	if err != nil {
		return err
	}
	defer directory.Close()
	return gcVerifyStage(directory, digest)
}

func gcVerifyStage(root *os.Root, digest string) error {
	entries, err := readRootDir(root, ".", 1)
	if err != nil || len(entries) != 1 || entries[0].Name() != executableName() || entries[0].Type()&os.ModeSymlink != 0 || !entries[0].Type().IsRegular() {
		return ErrDrift
	}
	data, err := readRegularRoot(root, executableName(), launcher.MaxBinarySize)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return ErrDrift
	}
	return nil
}

func gcEmptyAt(root *os.Root, name string) bool {
	directory, err := openGCDirectory(root, name)
	if err != nil {
		return false
	}
	defer directory.Close()
	empty, err := gcEmptyDirectory(directory)
	return err == nil && empty
}
func gcEmptyDirectory(root *os.Root) (bool, error) {
	entries, err := readRootDir(root, ".", 0)
	return err == nil && len(entries) == 0, err
}
func gcPathAbsent(root *os.Root, name string) error {
	_, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return ErrDrift
}

func writeGCJournal(root *os.Root, journal gcJournal) error {
	return writeGCJournalNamed(root, gcJournalName, journal)
}

func writeGCJournalNamed(root *os.Root, name string, journal gcJournal, guards ...func() error) error {
	data, err := marshalGCJournal(journal)
	if err != nil {
		return err
	}
	temporary, err := rootTemporaryName(".gc-journal-")
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if err := gcGuard(guards); err != nil {
		return err
	}
	if err := writeRootFile(root, temporary, data, 0o600); err != nil {
		return err
	}
	if err := gcGuard(guards); err != nil {
		return err
	}
	if err := root.Link(temporary, name); err != nil {
		return err
	}
	return syncRoot(root)
}

func advanceGCJournal(root *os.Root, journal *gcJournal, state gcState, guards ...func() error) error {
	current, err := readGCJournalAt(root, gcJournalName)
	if err != nil || current != *journal {
		return ErrGCRecovery
	}
	journal.State = state
	if err := writeGCJournalNamed(root, gcJournalNextName, *journal, guards...); err != nil {
		return ErrGCRecovery
	}
	before, err := root.Lstat(gcJournalName)
	if err != nil || before.Mode().IsRegular() == false {
		return ErrGCRecovery
	}
	verified, err := readGCJournalAt(root, gcJournalName)
	if err != nil || verified != current {
		return ErrGCRecovery
	}
	after, err := root.Lstat(gcJournalName)
	if err != nil || !os.SameFile(before, after) {
		return ErrGCRecovery
	}
	if err := gcGuard(guards); err != nil {
		return err
	}
	if err := root.Remove(gcJournalName); err != nil {
		return ErrGCRecovery
	}
	if err := gcGuard(guards); err != nil {
		return err
	}
	if err := root.Link(gcJournalNextName, gcJournalName); err != nil {
		return ErrGCRecovery
	}
	canonical, err := root.Lstat(gcJournalName)
	next, nextErr := root.Lstat(gcJournalNextName)
	if err != nil || nextErr != nil || !os.SameFile(canonical, next) {
		return ErrGCRecovery
	}
	if err := gcGuard(guards); err != nil {
		return err
	}
	if err := root.Remove(gcJournalNextName); err != nil {
		return ErrGCRecovery
	}
	if err := syncRoot(root); err != nil {
		return ErrGCRecovery
	}
	return nil
}

func marshalGCJournal(journal gcJournal) ([]byte, error) {
	if err := validateGCJournal(journal); err != nil {
		return nil, err
	}
	data, err := json.Marshal(journal)
	if err != nil || len(data)+1 > gcJournalMaximum {
		return nil, ErrGCRecovery
	}
	return append(data, '\n'), nil
}

func readGCJournal(root *os.Root) (gcJournal, error) {
	canonical, canonicalErr := readGCJournalAt(root, gcJournalName)
	next, nextErr := readGCJournalAt(root, gcJournalNextName)
	if errors.Is(canonicalErr, os.ErrNotExist) && errors.Is(nextErr, os.ErrNotExist) {
		return gcJournal{}, os.ErrNotExist
	}
	if canonicalErr == nil && errors.Is(nextErr, os.ErrNotExist) {
		return canonical, nil
	}
	if nextErr == nil && errors.Is(canonicalErr, os.ErrNotExist) {
		return next, nil
	}
	canonicalInfo, canonicalInfoErr := root.Lstat(gcJournalName)
	nextInfo, nextInfoErr := root.Lstat(gcJournalNextName)
	if canonicalErr != nil || nextErr != nil || canonicalInfoErr != nil || nextInfoErr != nil || !sameGCJournalIdentity(canonical, next) {
		return gcJournal{}, ErrGCRecovery
	}
	if canonical == next && os.SameFile(canonicalInfo, nextInfo) {
		return canonical, nil
	}
	if !gcStateSuccessor(canonical.State, next.State) {
		return gcJournal{}, ErrGCRecovery
	}
	return next, nil
}

func readGCJournalAt(root *os.Root, name string) (gcJournal, error) {
	data, err := readRegularRoot(root, name, gcJournalMaximum)
	if err != nil {
		return gcJournal{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return gcJournal{}, ErrGCRecovery
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return gcJournal{}, ErrGCRecovery
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return gcJournal{}, ErrGCRecovery
		}
		seen[name] = true
		var discard json.RawMessage
		if err := decoder.Decode(&discard); err != nil {
			return gcJournal{}, ErrGCRecovery
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return gcJournal{}, ErrGCRecovery
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return gcJournal{}, ErrGCRecovery
	}
	required := []string{"schemaVersion", "state", "planSha256", "candidateSha256", "source", "stage"}
	if len(seen) != len(required) {
		return gcJournal{}, ErrGCRecovery
	}
	for _, key := range required {
		if !seen[key] {
			return gcJournal{}, ErrGCRecovery
		}
	}
	var journal gcJournal
	if err := json.Unmarshal(data, &journal); err != nil || validateGCJournal(journal) != nil {
		return gcJournal{}, ErrGCRecovery
	}
	return journal, nil
}

// sameGCJournalIdentity intentionally excludes State so consecutive evidence can advance it.
func sameGCJournalIdentity(left, right gcJournal) bool {
	return left.SchemaVersion == right.SchemaVersion && left.PlanSHA256 == right.PlanSHA256 && left.CandidateSHA256 == right.CandidateSHA256 && left.Source == right.Source && left.Stage == right.Stage
}
func gcStateSuccessor(left, right gcState) bool {
	return (left == gcStatePrepared && right == gcStateMoving) || (left == gcStateMoving && right == gcStateStaged) || (left == gcStateStaged && right == gcStateDeleting) || (left == gcStateDeleting && right == gcStateDeleted)
}

func removeGCJournal(root *os.Root, guards ...func() error) error {
	for _, name := range []string{gcJournalName, gcJournalNextName} {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return ErrGCRecovery
		}
		if _, err := readGCJournalAt(root, name); err != nil {
			return ErrGCRecovery
		}
		current, err := root.Lstat(name)
		if err != nil || !os.SameFile(info, current) {
			return ErrGCRecovery
		}
		if err := gcGuard(guards); err != nil {
			return err
		}
		if err := root.Remove(name); err != nil {
			return ErrGCRecovery
		}
	}
	return nil
}

func validateGCJournal(journal gcJournal) error {
	if journal.SchemaVersion != "v1" || !validGCState(journal.State) || !validGCDigest(journal.PlanSHA256) || !validGCDigest(journal.CandidateSHA256) || journal.Source != filepath.Join("versions", journal.CandidateSHA256) || !validGCStage(journal.Stage) {
		return ErrGCRecovery
	}
	return nil
}

func validGCState(value gcState) bool {
	return value == gcStatePrepared || value == gcStateMoving || value == gcStateStaged || value == gcStateDeleting || value == gcStateDeleted
}
func validGCStage(value string) bool {
	suffix := strings.TrimPrefix(value, ".gc-stage-")
	return suffix != value && len(suffix) == 24 && filepath.Base(value) == value && isLowerHex(suffix)
}
func validGCDigest(value string) bool { return len(value) == 64 && isLowerHex(value) }
func isLowerHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func cloneGCResult(value GCResult) GCResult {
	value.Candidates = append([]string(nil), value.Candidates...)
	value.Retained = append([]string(nil), value.Retained...)
	value.Deleted = append([]string(nil), value.Deleted...)
	value.Recovered = append([]string(nil), value.Recovered...)
	return value
}
func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
func gcGuard(guards []func() error) error {
	for _, guard := range guards {
		if guard != nil {
			if err := guard(); err != nil {
				return err
			}
		}
	}
	return nil
}

func readRootDir(root *os.Root, name string, maximum int) ([]os.DirEntry, error) {
	directory, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(maximum + 1)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil || len(entries) > maximum {
		return nil, ErrDrift
	}
	return entries, nil
}
func openGCDirectory(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, ErrDrift
	}
	directory, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := directory.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = directory.Close()
		return nil, ErrDrift
	}
	return directory, nil
}
