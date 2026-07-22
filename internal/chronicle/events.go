package chronicle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/vgxness/vgxness/internal/contracts"
)

const (
	maxEventBytes = 1 << 20
	maxLogBytes   = 64 << 20
)

var (
	ErrConflict     = errors.New("conflict")
	validRunID      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	errLogNotExists = errors.New("event log does not exist")
)

type fileLockMode uint8

const (
	lockShared fileLockMode = iota
	lockExclusive
)

// Event is the stable identity header and validated JSON body of one run event.
type Event struct {
	SchemaVersion string
	ID            string
	RunID         string
	At            string
	Type          string
	Raw           json.RawMessage
}

// EventLog persists the append-only operational history for one run.
type EventLog struct {
	root  string
	runID string
	path  string
}

// NewEventLog binds a run to its Chronicle log without creating storage.
func NewEventLog(root, runID string) (*EventLog, error) {
	if len(runID) > 240 || !validRunID.MatchString(runID) {
		return nil, errors.New("invalid run ID")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Chronicle root: %w", err)
	}
	if err := validateDirectory(absRoot, "Chronicle root"); err != nil {
		return nil, err
	}
	return &EventLog{
		root:  absRoot,
		runID: runID,
		path:  filepath.Join(absRoot, "logs", runID+".jsonl"),
	}, nil
}

// Path returns the absolute JSONL path owned by this event log.
func (l *EventLog) Path() string { return l.path }

// Append validates and durably appends one event. Once the write begins, it
// completes readback or rollback even if the caller's context is cancelled.
func (l *EventLog) Append(ctx context.Context, document []byte) (Event, error) {
	event, line, err := prepareEvent(ctx, document)
	if err != nil {
		return Event{}, err
	}
	if event.RunID != l.runID {
		return Event{}, fmt.Errorf("%w: event run ID does not match log", ErrConflict)
	}
	if lifecycleEventRequiresHistory(event.Type) {
		file, historyErr := l.openExisting()
		if errors.Is(historyErr, errLogNotExists) {
			return Event{}, fmt.Errorf("%w: %w", ErrConflict, ErrIllegalTaskTransition)
		}
		if historyErr != nil {
			return Event{}, historyErr
		}
		file.Close()
	}
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if err := l.ensureLogsDirectory(); err != nil {
		return Event{}, err
	}

	file, err := l.openForAppend()
	if err != nil {
		return Event{}, err
	}
	defer file.Close()
	if err := lockFile(ctx, file, lockExclusive); err != nil {
		return Event{}, err
	}
	defer unlockFile(file)
	if err := l.verifyOpenFile(file); err != nil {
		return Event{}, err
	}
	if err := syncDirectory(filepath.Dir(l.path)); err != nil {
		return Event{}, err
	}

	existing, size, err := l.readLocked(ctx, file)
	if err != nil {
		return Event{}, err
	}
	for _, prior := range existing {
		if prior.ID == event.ID {
			return Event{}, fmt.Errorf("%w: duplicate event ID", ErrConflict)
		}
	}
	candidate := make([]Event, len(existing)+1)
	copy(candidate, existing)
	candidate[len(existing)] = event
	if _, err := DeriveTaskStates(candidate); err != nil {
		return Event{}, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	if size+int64(len(line)) > maxLogBytes {
		return Event{}, fmt.Errorf("%w: event log exceeds size limit", ErrCorrupt)
	}
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}

	if err := appendAndVerify(file, size, line, event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func lifecycleEventRequiresHistory(eventType string) bool {
	switch eventType {
	case "task.completed", "task.failed", "background.completed", "background.failed", "cancellation.completed":
		return true
	default:
		return false
	}
}

// Read returns every validated event in append order. A missing log is empty;
// an incomplete final line or any invalid record is corruption.
func (l *EventLog) Read(ctx context.Context) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := l.openExisting()
	if errors.Is(err, errLogNotExists) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := lockFile(ctx, file, lockShared); err != nil {
		return nil, err
	}
	defer unlockFile(file)
	if err := l.verifyOpenFile(file); err != nil {
		return nil, err
	}
	events, _, err := l.readLocked(ctx, file)
	return events, err
}

func prepareEvent(ctx context.Context, document []byte) (Event, []byte, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, nil, err
	}
	if len(document) > maxEventBytes {
		return Event{}, nil, eventSizeError()
	}
	if err := contracts.Validate(ctx, contracts.RunEventSchemaURI, document, false); err != nil {
		return Event{}, nil, err
	}
	var references struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	if err := json.Unmarshal(document, &references); err != nil {
		return Event{}, nil, err
	}
	if len(references.Artifact) != 0 && string(references.Artifact) != "null" && !canonicalArtifactReference(references.Artifact) {
		return Event{}, nil, canonicalArtifactError(contracts.RunEventSchemaURI, "/artifact")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, document); err != nil {
		return Event{}, nil, fmt.Errorf("compact event: %w", err)
	}
	if compact.Len() > maxEventBytes {
		return Event{}, nil, eventSizeError()
	}
	event, err := decodeEvent(compact.Bytes())
	if err != nil {
		return Event{}, nil, fmt.Errorf("validated event header: %w", err)
	}
	line := make([]byte, compact.Len()+1)
	copy(line, compact.Bytes())
	line[len(line)-1] = '\n'
	return event, line, nil
}

func decodeEvent(document []byte) (Event, error) {
	var header struct {
		SchemaVersion string `json:"schemaVersion"`
		ID            string `json:"eventId"`
		RunID         string `json:"runId"`
		At            string `json:"at"`
		Type          string `json:"type"`
	}
	if err := json.Unmarshal(document, &header); err != nil {
		return Event{}, err
	}
	return Event{
		SchemaVersion: header.SchemaVersion,
		ID:            header.ID,
		RunID:         header.RunID,
		At:            header.At,
		Type:          header.Type,
		Raw:           append(json.RawMessage(nil), document...),
	}, nil
}

func eventSizeError() error {
	return &contracts.ContractError{
		Kind:          "contract.invalid",
		SchemaVersion: "1",
		Code:          "contract.invalid",
		SchemaURI:     contracts.RunEventSchemaURI,
		Message:       "document exceeds size limit",
		Recoverable:   false,
	}
}

func (l *EventLog) ensureLogsDirectory() error {
	if err := validateDirectory(l.root, "Chronicle root"); err != nil {
		return err
	}
	logs := filepath.Join(l.root, "logs")
	err := os.Mkdir(logs, 0o700)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("create Chronicle logs directory: %w", err)
	}
	if err := validateDirectory(logs, "Chronicle logs directory"); err != nil {
		return err
	}
	return syncDirectory(l.root)
}

func validateDirectory(path, name string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s must be a directory", ErrCorrupt, name)
	}
	return nil
}

func (l *EventLog) openForAppend() (*os.File, error) {
	if info, err := os.Lstat(l.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: event log must be a regular file", ErrCorrupt)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect event log: %w", err)
	}
	file, err := openNoFollow(l.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	if err := l.verifyOpenFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (l *EventLog) openExisting() (*os.File, error) {
	if err := validateDirectory(l.root, "Chronicle root"); err != nil {
		return nil, err
	}
	logs := filepath.Join(l.root, "logs")
	if err := validateDirectory(logs, "Chronicle logs directory"); errors.Is(err, fs.ErrNotExist) {
		return nil, errLogNotExists
	} else if err != nil {
		return nil, err
	}
	info, err := os.Lstat(l.path)
	if os.IsNotExist(err) {
		return nil, errLogNotExists
	}
	if err != nil {
		return nil, fmt.Errorf("inspect event log: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: event log must be a regular file", ErrCorrupt)
	}
	file, err := openNoFollow(l.path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	if err := l.verifyOpenFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (l *EventLog) verifyOpenFile(file *os.File) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect open event log: %w", err)
	}
	pathInfo, err := os.Lstat(l.path)
	if err != nil {
		return fmt.Errorf("inspect event log path: %w", err)
	}
	if !opened.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, pathInfo) {
		return fmt.Errorf("%w: event log path changed", ErrCorrupt)
	}
	return nil
}

func (l *EventLog) readLocked(ctx context.Context, file *os.File) ([]Event, int64, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("inspect event log: %w", err)
	}
	size := info.Size()
	if size > maxLogBytes {
		return nil, size, fmt.Errorf("%w: event log exceeds size limit", ErrCorrupt)
	}
	if size == 0 {
		return nil, 0, nil
	}
	last := []byte{0}
	if _, err := file.ReadAt(last, size-1); err != nil {
		return nil, size, fmt.Errorf("read event log boundary: %w", err)
	}
	if last[0] != '\n' {
		return nil, size, fmt.Errorf("%w: event log has an incomplete final record", ErrCorrupt)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, size, fmt.Errorf("seek event log: %w", err)
	}

	scanner := bufio.NewScanner(io.LimitReader(file, maxLogBytes+1))
	scanner.Buffer(make([]byte, 64<<10), maxEventBytes+1)
	events := make([]Event, 0)
	seen := make(map[string]struct{})
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, size, err
		}
		record := append([]byte(nil), scanner.Bytes()...)
		if len(record) == 0 || len(record) > maxEventBytes {
			return nil, size, fmt.Errorf("%w: invalid event record size", ErrCorrupt)
		}
		if err := contracts.Validate(ctx, contracts.RunEventSchemaURI, record, false); err != nil {
			return nil, size, fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		event, err := decodeEvent(record)
		if err != nil {
			return nil, size, fmt.Errorf("%w: malformed event header", ErrCorrupt)
		}
		if event.RunID != l.runID {
			return nil, size, fmt.Errorf("%w: event run ID does not match log", ErrCorrupt)
		}
		if _, duplicate := seen[event.ID]; duplicate {
			return nil, size, fmt.Errorf("%w: duplicate event ID", ErrCorrupt)
		}
		seen[event.ID] = struct{}{}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, size, fmt.Errorf("%w: read event log: %v", ErrCorrupt, err)
	}
	if _, err := DeriveTaskStates(events); err != nil {
		return nil, size, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return events, size, nil
}

func appendAndVerify(file *os.File, originalSize int64, line []byte, expected Event) error {
	written, err := file.Write(line)
	if err == nil && written != len(line) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if err == nil {
		readback := make([]byte, len(line))
		_, err = file.ReadAt(readback, originalSize)
		if err == nil && !bytes.Equal(readback, line) {
			err = errors.New("event readback differs from append")
		}
		if err == nil {
			var actual Event
			actual, err = decodeEvent(bytes.TrimSuffix(readback, []byte{'\n'}))
			if err == nil && !sameEventIdentity(actual, expected) {
				err = errors.New("event readback identity mismatch")
			}
		}
	}
	if err == nil {
		return nil
	}
	rollbackErr := file.Truncate(originalSize)
	if rollbackErr == nil {
		rollbackErr = file.Sync()
	}
	if rollbackErr != nil {
		return fmt.Errorf("append event failed and rollback failed: %w", errors.Join(err, rollbackErr))
	}
	return fmt.Errorf("append event: %w", err)
}

func sameEventIdentity(left, right Event) bool {
	return left.SchemaVersion == right.SchemaVersion && left.ID == right.ID && left.RunID == right.RunID && left.At == right.At && left.Type == right.Type
}
