package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/contracts"
	"github.com/vgxness/vgxness/internal/orchestrator"
)

const maxDeliveryDocument = 1 << 20

type store struct {
	root, receipts, current, lock string
}

func openStore(ctx context.Context, options config.Options, create bool) (store, error) {
	var (
		paths config.Paths
		err   error
	)
	if create {
		paths, err = config.Prepare(ctx, options)
	} else {
		paths, err = config.PathsFor(options)
	}
	if err != nil {
		return store{}, err
	}
	root := filepath.Join(paths.Root, "delivery")
	for _, directory := range []string{root, filepath.Join(root, "receipts")} {
		if create {
			if err := os.MkdirAll(directory, 0o700); err != nil {
				return store{}, fmt.Errorf("prepare delivery storage: %w", err)
			}
		}
		info, err := os.Lstat(directory)
		if os.IsNotExist(err) && !create {
			return store{}, ErrNotFound
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return store{}, fmt.Errorf("%w: unsafe delivery directory", ErrCorrupt)
		}
	}
	return store{root: root, receipts: filepath.Join(root, "receipts"), current: filepath.Join(root, "current.json"), lock: filepath.Join(root, ".lock")}, nil
}

func (storage store) withLock(action func() error) error {
	lock, err := orchestrator.AcquireFileLock(storage.lock)
	if err != nil {
		return fmt.Errorf("lock delivery authority: %w", err)
	}
	defer lock.Release()
	return action()
}

func (storage store) issue(ctx context.Context, receipt Receipt, now time.Time) (Receipt, error) {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return Receipt{}, err
	}
	path := filepath.Join(storage.receipts, receipt.ReceiptID+".json")
	if existing, readErr := readFile(path); readErr == nil {
		if err := contracts.Validate(ctx, contracts.DeliveryReceiptSchemaURI, existing, false); err != nil {
			return Receipt{}, fmt.Errorf("%w: existing receipt failed validation", ErrCorrupt)
		}
		var persisted Receipt
		if err := json.Unmarshal(existing, &persisted); err != nil || persisted.ReceiptID != receipt.ReceiptID || !reflect.DeepEqual(persisted.Target, receipt.Target) || persisted.Bindings != receipt.Bindings || !bytes.Equal(mustJSON(persisted.Manifest), mustJSON(receipt.Manifest)) {
			return Receipt{}, fmt.Errorf("%w: immutable receipt ID already exists", ErrConflict)
		}
		receipt, encoded = persisted, existing
	} else if !os.IsNotExist(readErr) {
		return Receipt{}, readErr
	} else if err := atomicWrite(ctx, path, encoded, contracts.DeliveryReceiptSchemaURI, false); err != nil {
		return Receipt{}, err
	}
	digest := sha256.Sum256(encoded)
	current := Current{SchemaVersion: SchemaVersion, ReceiptID: receipt.ReceiptID, ReceiptSHA256: hex.EncodeToString(digest[:]), State: "active", UpdatedAt: now.UTC().Format(time.RFC3339Nano)}
	if err := writeCurrent(ctx, storage.current, current); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func (storage store) readStatus(ctx context.Context) (Status, error) {
	currentData, err := readFile(storage.current)
	if os.IsNotExist(err) {
		return Status{}, ErrNotFound
	}
	if err != nil {
		return Status{}, fmt.Errorf("%w: current pointer unavailable", ErrCorrupt)
	}
	if err := contracts.Validate(ctx, contracts.DeliveryCurrentSchemaURI, currentData, false); err != nil {
		return Status{}, fmt.Errorf("%w: current pointer: %v", ErrCorrupt, err)
	}
	var current Current
	if err := json.Unmarshal(currentData, &current); err != nil || !validDigest(current.ReceiptID) || !validDigest(current.ReceiptSHA256) {
		return Status{}, fmt.Errorf("%w: invalid current pointer", ErrCorrupt)
	}
	receiptData, err := readFile(filepath.Join(storage.receipts, current.ReceiptID+".json"))
	if err != nil {
		return Status{}, fmt.Errorf("%w: receipt unavailable", ErrCorrupt)
	}
	digest := sha256.Sum256(receiptData)
	if hex.EncodeToString(digest[:]) != current.ReceiptSHA256 {
		return Status{}, fmt.Errorf("%w: receipt digest mismatch", ErrCorrupt)
	}
	if err := contracts.Validate(ctx, contracts.DeliveryReceiptSchemaURI, receiptData, false); err != nil {
		return Status{}, fmt.Errorf("%w: receipt: %v", ErrCorrupt, err)
	}
	var receipt Receipt
	if err := json.Unmarshal(receiptData, &receipt); err != nil || receipt.ReceiptID != current.ReceiptID {
		return Status{}, fmt.Errorf("%w: receipt identity mismatch", ErrCorrupt)
	}
	if err := validateTargetSnapshot(receipt.Target); err != nil {
		return Status{}, err
	}
	bindings, err := manifestBindings(receipt.Manifest)
	if err != nil || bindings != receipt.Bindings {
		return Status{}, fmt.Errorf("%w: receipt binding mismatch", ErrCorrupt)
	}
	identity := struct {
		Target   TargetSnapshot `json:"target"`
		Bindings Bindings       `json:"bindings"`
	}{receipt.Target, receipt.Bindings}
	expectedID, err := digestJSON(identity)
	if err != nil || expectedID != receipt.ReceiptID {
		return Status{}, fmt.Errorf("%w: receipt content identity mismatch", ErrCorrupt)
	}
	return Status{Current: current, Receipt: receipt}, nil
}

func (storage store) invalidate(ctx context.Context, current Current, reason string, now time.Time) (Current, error) {
	current.State = "invalidated"
	current.Reason = reason
	current.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	if err := writeCurrent(ctx, storage.current, current); err != nil {
		return Current{}, err
	}
	return current, nil
}

func writeCurrent(ctx context.Context, path string, current Current) error {
	encoded, err := json.Marshal(current)
	if err != nil {
		return err
	}
	return atomicWrite(ctx, path, encoded, contracts.DeliveryCurrentSchemaURI, true)
}

func atomicWrite(ctx context.Context, path string, data []byte, schema string, replace bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) > maxDeliveryDocument {
		return fmt.Errorf("%w: delivery document exceeds size limit", ErrInvalid)
	}
	if err := contracts.Validate(ctx, schema, data, false); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !replace || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%w: unsafe delivery target", ErrConflict)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".delivery-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write delivery document: %w", err)
	}
	readback, err := readFile(temporaryPath)
	if err != nil || !bytes.Equal(readback, data) {
		return fmt.Errorf("%w: delivery write readback failed", ErrCorrupt)
	}
	if replace {
		if err := os.Rename(temporaryPath, path); err != nil {
			return err
		}
	} else {
		if err := os.Link(temporaryPath, path); err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("%w: immutable delivery target already exists", ErrConflict)
			}
			return err
		}
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr = directory.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	final, err := readFile(path)
	if err != nil || !bytes.Equal(final, data) {
		return fmt.Errorf("%w: final delivery readback failed", ErrCorrupt)
	}
	return contracts.Validate(context.Background(), schema, final, false)
}

func readFile(path string) ([]byte, error) {
	file, err := openDeliveryFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxDeliveryDocument {
		return nil, fmt.Errorf("%w: unsafe delivery file", ErrCorrupt)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDeliveryDocument+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDeliveryDocument {
		return nil, fmt.Errorf("%w: delivery file exceeds size limit", ErrCorrupt)
	}
	return data, nil
}
