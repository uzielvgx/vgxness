package chronicle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/vgxness/vgxness/internal/contracts"
)

const maxRunSnapshotBytes int64 = 8 << 20

// ErrInconsistent means individually valid Chronicle sources disagree.
var ErrInconsistent = errors.New("inconsistent Chronicle state")

// SnapshotStore owns the readable snapshot projection for one event log.
type SnapshotStore struct {
	root        string
	runID       string
	runPath     string
	currentPath string
	log         *EventLog
	writeJSON   snapshotWriteFunc
	removeFile  func(string) error
}

type snapshotWriteFunc func(context.Context, string, []byte, int64, string, string) error

// NewSnapshotStore binds snapshot paths to an existing Chronicle root without
// creating files or directories.
func NewSnapshotStore(root, runID string) (*SnapshotStore, error) {
	log, err := NewEventLog(root, runID)
	if err != nil {
		return nil, err
	}
	return &SnapshotStore{
		root:        log.root,
		runID:       runID,
		runPath:     filepath.Join(log.root, "runs", runID+".json"),
		currentPath: filepath.Join(log.root, "current-run.json"),
		log:         log,
		writeJSON:   atomicWriteJSON,
		removeFile:  os.Remove,
	}, nil
}

// RunPath returns the stable terminal-run snapshot path. Active snapshots are
// immutable, content-addressed files referenced by current-run.json.
func (s *SnapshotStore) RunPath() string { return s.runPath }

// CurrentPath returns the absolute active-run pointer path.
func (s *SnapshotStore) CurrentPath() string { return s.currentPath }

// WriteActive stages an immutable full-run snapshot and atomically commits it by
// replacing current-run.json. A crash leaves either the previous pointer or the
// new pointer referencing a complete snapshot.
func (s *SnapshotStore) WriteActive(ctx context.Context, runDocument []byte, current CurrentRun) error {
	return s.writeActive(ctx, runDocument, current, nil)
}

// WriteActiveContinuation publishes the next state only while the active run
// still points at the task and capsule recovered by the caller. This prevents
// two independent continuation processes from advancing the same run from a
// stale snapshot.
func (s *SnapshotStore) WriteActiveContinuation(ctx context.Context, runDocument []byte, current, expected CurrentRun) error {
	return s.writeActive(ctx, runDocument, current, &expected)
}

func (s *SnapshotStore) writeActive(ctx context.Context, runDocument []byte, current CurrentRun, expected *CurrentRun) error {
	run, runData, err := prepareRunSnapshot(ctx, runDocument)
	if err != nil {
		return err
	}
	current.RunFile = activeRunFile(s.runID, runData)
	current.LogFile = filepath.ToSlash(filepath.Join("logs", s.runID+".jsonl"))
	currentData, err := prepareCurrentSnapshot(ctx, current)
	if err != nil {
		return err
	}
	if err := validateActiveProjection(run, current, s.runID); err != nil {
		return err
	}

	return s.withStateLock(ctx, lockExclusive, func() error {
		return s.withEvents(ctx, lockExclusive, func(events []Event) error {
			if err := validateEventReferences(run, events); err != nil {
				return err
			}
			if len(events) == 0 || current.LastEventID != events[len(events)-1].ID {
				return fmt.Errorf("%w: current snapshot is not at the event-log head", ErrInconsistent)
			}
			if existing, present, err := ReadCurrent(ctx, s.currentPath); err != nil {
				return err
			} else if expected != nil && (!present || existing.ID != expected.ID || existing.TaskID != expected.TaskID || existing.CapsuleID != expected.CapsuleID) {
				return fmt.Errorf("%w: active run advanced before continuation", ErrConflict)
			} else if present && existing.ID != s.runID {
				return fmt.Errorf("%w: another run owns the current pointer", ErrConflict)
			}
			activePath := filepath.Join(s.root, filepath.FromSlash(current.RunFile))
			if err := validateSnapshotTarget(activePath); err != nil {
				return err
			}
			if err := validateSnapshotTarget(s.currentPath); err != nil {
				return err
			}
			if err := s.ensureRunsDirectory(); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			commitCtx := context.WithoutCancel(ctx)
			if err := s.writeImmutableJSON(commitCtx, activePath, runData, maxRunSnapshotBytes, contracts.RunSchemaURI, s.runID); err != nil {
				return err
			}
			return s.writeJSON(commitCtx, s.currentPath, currentData, maxCurrentRunBytes, contracts.CurrentRunSchemaURI, s.runID)
		})
	})
}

// Finalize stages the stable terminal snapshot and commits it by removing this
// run's active pointer. Recover completes that removal after a crash.
func (s *SnapshotStore) Finalize(ctx context.Context, runDocument []byte) error {
	run, runData, err := prepareRunSnapshot(ctx, runDocument)
	if err != nil {
		return err
	}
	if !terminalStatus(run.Status) {
		return fmt.Errorf("%w: final snapshot is not terminal", ErrInconsistent)
	}
	return s.withStateLock(ctx, lockExclusive, func() error {
		return s.withEvents(ctx, lockExclusive, func(events []Event) error {
			if err := validateEventReferences(run, events); err != nil {
				return err
			}
			if err := validateTerminalHead(run, events); err != nil {
				return err
			}
			current, present, err := ReadCurrent(ctx, s.currentPath)
			if err != nil {
				return err
			}
			if present {
				if current.ID != s.runID {
					return fmt.Errorf("%w: another run owns the current pointer", ErrConflict)
				}
				if !containsEvent(events, current.LastEventID) {
					return fmt.Errorf("%w: current pointer references an unknown event", ErrInconsistent)
				}
				if err := validateFinalizingPointer(run, current); err != nil {
					return err
				}
			}
			if err := validateSnapshotTarget(s.runPath); err != nil {
				return err
			}
			if err := s.ensureRunsDirectory(); err != nil {
				return err
			}
			commitCtx := context.WithoutCancel(ctx)
			if err := s.writeJSON(commitCtx, s.runPath, runData, maxRunSnapshotBytes, contracts.RunSchemaURI, s.runID); err != nil {
				return err
			}
			if !present {
				return nil
			}
			if err := s.removeFile(s.currentPath); err != nil {
				return fmt.Errorf("remove current-run pointer: %w", err)
			}
			return syncDirectory(s.root)
		})
	})
}

func activeRunFile(runID string, data []byte) string {
	digest := sha256.Sum256(data)
	return filepath.ToSlash(filepath.Join("runs", fmt.Sprintf("%s.%x.json", runID, digest)))
}

func (s *SnapshotStore) writeImmutableJSON(ctx context.Context, path string, data []byte, limit int64, schemaURI, expectedID string) error {
	if err := validateSnapshotReadback(ctx, path, data, limit, schemaURI, expectedID); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate immutable snapshot: %w", err)
	}
	return s.writeJSON(ctx, path, data, limit, schemaURI, expectedID)
}

func prepareRunSnapshot(ctx context.Context, document []byte) (runIdentity, []byte, error) {
	if err := ctx.Err(); err != nil {
		return runIdentity{}, nil, err
	}
	if int64(len(document)) > maxRunSnapshotBytes {
		return runIdentity{}, nil, snapshotSizeError(contracts.RunSchemaURI)
	}
	if err := contracts.Validate(ctx, contracts.RunSchemaURI, document, false); err != nil {
		return runIdentity{}, nil, err
	}
	data, err := readableJSON(document)
	if err != nil {
		return runIdentity{}, nil, fmt.Errorf("format run snapshot: %w", err)
	}
	if int64(len(data)) > maxRunSnapshotBytes {
		return runIdentity{}, nil, snapshotSizeError(contracts.RunSchemaURI)
	}
	run, err := decodeRunIdentity(data)
	if err != nil {
		return runIdentity{}, nil, fmt.Errorf("decode validated run snapshot: %w", err)
	}
	for index, artifact := range run.Artifacts {
		if !canonicalArtifactReference(artifact) {
			return runIdentity{}, nil, canonicalArtifactError(contracts.RunSchemaURI, fmt.Sprintf("/artifacts/%d", index))
		}
	}
	if err := validateRunIdentity(run); err != nil {
		return runIdentity{}, nil, err
	}
	return run, data, nil
}

func prepareCurrentSnapshot(ctx context.Context, current CurrentRun) ([]byte, error) {
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode current-run snapshot: %w", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > maxCurrentRunBytes {
		return nil, snapshotSizeError(contracts.CurrentRunSchemaURI)
	}
	if err := contracts.Validate(ctx, contracts.CurrentRunSchemaURI, data, false); err != nil {
		return nil, err
	}
	if !valid(current) {
		return nil, fmt.Errorf("%w: invalid current-run semantics", ErrInconsistent)
	}
	return data, nil
}

func readableJSON(document []byte) ([]byte, error) {
	var output bytes.Buffer
	if err := json.Indent(&output, document, "", "  "); err != nil {
		return nil, err
	}
	output.WriteByte('\n')
	return output.Bytes(), nil
}

func snapshotSizeError(schemaURI string) error {
	return &contracts.ContractError{
		Kind:          "contract.invalid",
		SchemaVersion: "1",
		Code:          "contract.invalid",
		SchemaURI:     schemaURI,
		Message:       "document exceeds size limit",
		Recoverable:   false,
	}
}

func canonicalArtifactReference(raw []byte) bool {
	var artifact struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal(raw, &artifact) == nil && artifact.Kind == "artifact.reference"
}

func canonicalArtifactError(schemaURI, pointer string) error {
	return &contracts.ContractError{
		Kind:          "contract.invalid",
		SchemaVersion: "1",
		Code:          "contract.invalid",
		SchemaURI:     schemaURI,
		Pointer:       pointer,
		Message:       "canonical writer requires artifact.reference",
		Recoverable:   false,
	}
}

func (s *SnapshotStore) withEvents(ctx context.Context, operation fileLockMode, action func([]Event) error) error {
	file, err := s.log.openExisting()
	if errors.Is(err, errLogNotExists) {
		return fmt.Errorf("%w: event log is missing", ErrInconsistent)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if err := lockFile(ctx, file, operation); err != nil {
		return err
	}
	defer unlockFile(file)
	if err := s.log.verifyOpenFile(file); err != nil {
		return err
	}
	events, _, err := s.log.readLocked(ctx, file)
	if err != nil {
		return err
	}
	return action(events)
}

func (s *SnapshotStore) withStateLock(ctx context.Context, operation fileLockMode, action func() error) error {
	if err := validateDirectory(s.root, "Chronicle root"); err != nil {
		return err
	}
	root, err := openStateLock(s.root)
	if err != nil {
		return fmt.Errorf("open Chronicle root lock: %w", err)
	}
	defer root.Close()
	if err := lockFile(ctx, root, operation); err != nil {
		return err
	}
	defer unlockFile(root)
	return action()
}

func (s *SnapshotStore) ensureRunsDirectory() error {
	if err := validateDirectory(s.root, "Chronicle root"); err != nil {
		return err
	}
	runs := filepath.Join(s.root, "runs")
	err := os.Mkdir(runs, 0o700)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("create Chronicle runs directory: %w", err)
	}
	if err := validateDirectory(runs, "Chronicle runs directory"); err != nil {
		return err
	}
	return syncDirectory(s.root)
}

func atomicWriteJSON(ctx context.Context, path string, data []byte, limit int64, schemaURI, expectedID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := contracts.Validate(ctx, schemaURI, data, false); err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return snapshotSizeError(schemaURI)
	}
	if err := validateSnapshotTarget(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create snapshot temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		var written int
		written, err = temporary.Write(data)
		if err == nil && written != len(data) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write snapshot temporary file: %w", err)
	}
	if err := validateSnapshotReadback(ctx, temporaryPath, data, limit, schemaURI, expectedID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace snapshot: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	return validateSnapshotReadback(context.Background(), path, data, limit, schemaURI, expectedID)
}

func validateSnapshotTarget(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: snapshot target must be a regular file", ErrCorrupt)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect snapshot target: %w", err)
	}
	return nil
}

func validateSnapshotReadback(ctx context.Context, path string, expected []byte, limit int64, schemaURI, expectedID string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%w: snapshot permissions must be 0600", ErrCorrupt)
	}
	data, err := readRegularFile(path, limit)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, expected) {
		return fmt.Errorf("%w: snapshot readback differs from write", ErrCorrupt)
	}
	if err := contracts.Validate(ctx, schemaURI, data, false); err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &identity); err != nil || identity.ID != expectedID {
		return fmt.Errorf("%w: snapshot readback identity mismatch", ErrCorrupt)
	}
	return nil
}

func readRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: snapshot must be a regular file", ErrCorrupt)
	}
	file, err := openNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open snapshot: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%w: snapshot path changed", ErrCorrupt)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: snapshot exceeds size limit", ErrCorrupt)
	}
	return data, nil
}
