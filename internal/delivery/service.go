package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/contracts"
	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

type Service struct {
	workspace string
	now       func() time.Time
}

func New(workspace string) (*Service, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve workspace", ErrInvalid)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	return &Service{workspace: filepath.Clean(abs), now: time.Now}, nil
}

func (service *Service) Issue(ctx context.Context, options config.Options, request IssueRequest) (Receipt, error) {
	manifest, err := normalizeManifest(ctx, request.Manifest)
	if err != nil {
		return Receipt{}, err
	}
	target, err := captureTarget(ctx, service.workspace, request.BaseRef)
	if err != nil {
		return Receipt{}, err
	}
	if len(target.Paths) == 0 {
		return Receipt{}, fmt.Errorf("%w: target has no changes", ErrInvalid)
	}
	bindings, err := manifestBindings(manifest)
	if err != nil {
		return Receipt{}, err
	}
	identity := struct {
		Target   TargetSnapshot `json:"target"`
		Bindings Bindings       `json:"bindings"`
	}{target, bindings}
	receiptID, err := digestJSON(identity)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{
		Kind: "delivery.review-receipt", SchemaVersion: SchemaVersion,
		ReceiptID: receiptID, IssuedAt: service.now().UTC().Format(time.RFC3339Nano),
		Target: target, Bindings: bindings, Manifest: manifest,
	}
	if err := validateDocument(ctx, contracts.DeliveryReceiptSchemaURI, receipt); err != nil {
		return Receipt{}, err
	}
	store, err := openStore(ctx, options, true)
	if err != nil {
		return Receipt{}, err
	}
	if err := store.withLock(func() error {
		var issueErr error
		receipt, issueErr = store.issue(ctx, receipt, service.now())
		return issueErr
	}); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (service *Service) Status(ctx context.Context, options config.Options) (Status, error) {
	store, err := openStore(ctx, options, false)
	if err != nil {
		return Status{}, err
	}
	var result Status
	err = store.withLock(func() error {
		var readErr error
		result, readErr = store.readStatus(ctx)
		return readErr
	})
	return result, err
}

func (service *Service) Validate(ctx context.Context, options config.Options, request ValidateRequest) (Validation, error) {
	if !validGate(request.Gate) {
		return Validation{}, fmt.Errorf("%w: unknown gate", ErrInvalid)
	}
	manifest, err := normalizeManifest(ctx, request.Manifest)
	if err != nil {
		return Validation{}, err
	}
	store, err := openStore(ctx, options, false)
	if err != nil {
		return Validation{}, err
	}
	var validation Validation
	err = store.withLock(func() error {
		status, readErr := store.readStatus(ctx)
		if readErr != nil {
			return readErr
		}
		validation = Validation{Gate: request.Gate, ReceiptID: status.Receipt.ReceiptID, State: status.Current.State, Target: status.Receipt.Target}
		if status.Current.State != "active" {
			return ErrInvalidated
		}
		baseRef := request.BaseRef
		if baseRef == "" {
			baseRef = status.Receipt.Target.BaseRevision
		}
		liveTarget, targetErr := captureTarget(ctx, service.workspace, baseRef)
		if targetErr != nil {
			return targetErr
		}
		bindings, bindingErr := manifestBindings(manifest)
		if bindingErr != nil {
			return bindingErr
		}
		if !reflect.DeepEqual(liveTarget, status.Receipt.Target) || bindings != status.Receipt.Bindings {
			reason := "content-bound target or manifest changed"
			current, invalidateErr := store.invalidate(ctx, status.Current, reason, service.now())
			if invalidateErr != nil {
				return invalidateErr
			}
			validation.State = current.State
			return fmt.Errorf("%w: %s", ErrInvalidated, reason)
		}
		validation.State = "valid"
		return nil
	})
	return validation, err
}

func (service *Service) Invalidate(ctx context.Context, options config.Options, reason string) (Current, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 512 || strings.ContainsAny(reason, "\x00\r\n") {
		return Current{}, fmt.Errorf("%w: invalid invalidation reason", ErrInvalid)
	}
	store, err := openStore(ctx, options, false)
	if err != nil {
		return Current{}, err
	}
	var current Current
	err = store.withLock(func() error {
		status, readErr := store.readStatus(ctx)
		if readErr != nil {
			return readErr
		}
		current, readErr = store.invalidate(ctx, status.Current, reason, service.now())
		return readErr
	})
	return current, err
}

func normalizeManifest(ctx context.Context, manifest Manifest) (Manifest, error) {
	if manifest.SchemaVersion == "" {
		manifest.SchemaVersion = SchemaVersion
	}
	if err := validateDocument(ctx, contracts.DeliveryManifestSchemaURI, manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	identities := []Identity{manifest.Context.Policy, manifest.Context.Prompt, manifest.Context.Registry, manifest.Context.Provider, manifest.Context.Model}
	for _, check := range manifest.Evidence.Checks {
		identities = append(identities, check.Toolchain...)
		if check.ExitCode != 0 {
			return Manifest{}, fmt.Errorf("%w: evidence check did not pass", ErrInvalid)
		}
		started, startedErr := time.Parse(time.RFC3339, check.StartedAt)
		finished, finishedErr := time.Parse(time.RFC3339, check.FinishedAt)
		if startedErr != nil || finishedErr != nil || finished.Before(started) {
			return Manifest{}, fmt.Errorf("%w: invalid evidence interval", ErrInvalid)
		}
	}
	for _, identity := range identities {
		if !validDigest(identity.SHA256) {
			return Manifest{}, fmt.Errorf("%w: identity digest must be lowercase SHA-256", ErrInvalid)
		}
	}
	seenChecks := map[string]struct{}{}
	for index := range manifest.Evidence.Checks {
		check := &manifest.Evidence.Checks[index]
		if _, exists := seenChecks[check.ID]; exists {
			return Manifest{}, fmt.Errorf("%w: duplicate evidence check", ErrInvalid)
		}
		seenChecks[check.ID] = struct{}{}
		if !validDigest(check.OutputSHA256) {
			return Manifest{}, fmt.Errorf("%w: evidence digest must be lowercase SHA-256", ErrInvalid)
		}
		sort.Slice(check.Toolchain, func(i, j int) bool { return check.Toolchain[i].ID < check.Toolchain[j].ID })
		for i := 1; i < len(check.Toolchain); i++ {
			if check.Toolchain[i-1].ID == check.Toolchain[i].ID {
				return Manifest{}, fmt.Errorf("%w: duplicate toolchain identity", ErrInvalid)
			}
		}
	}
	sort.Slice(manifest.Evidence.Checks, func(i, j int) bool { return manifest.Evidence.Checks[i].ID < manifest.Evidence.Checks[j].ID })
	sort.Strings(manifest.Review.Lenses)
	for i := 1; i < len(manifest.Review.Lenses); i++ {
		if manifest.Review.Lenses[i-1] == manifest.Review.Lenses[i] {
			return Manifest{}, fmt.Errorf("%w: duplicate review lens", ErrInvalid)
		}
	}
	sort.Slice(manifest.Review.Findings, func(i, j int) bool {
		if manifest.Review.Findings[i].Severity != manifest.Review.Findings[j].Severity {
			return manifest.Review.Findings[i].Severity < manifest.Review.Findings[j].Severity
		}
		return manifest.Review.Findings[i].Code < manifest.Review.Findings[j].Code
	})
	if manifest.Review.Verdict != "approved" {
		return Manifest{}, fmt.Errorf("%w: review verdict must be approved", ErrInvalid)
	}
	expectedLenses := map[string]int{"low": 0, "medium": 1, "high": 4}[manifest.Review.Risk]
	if len(manifest.Review.Lenses) != expectedLenses {
		return Manifest{}, fmt.Errorf("%w: review lenses do not match risk class", ErrInvalid)
	}
	for _, finding := range manifest.Review.Findings {
		if finding.Severity == "blocker" || finding.Severity == "critical" {
			return Manifest{}, fmt.Errorf("%w: unresolved %s finding", ErrInvalid, finding.Severity)
		}
	}
	return manifest, nil
}

func manifestBindings(manifest Manifest) (Bindings, error) {
	contextDigest, err := digestJSON(manifest.Context)
	if err != nil {
		return Bindings{}, err
	}
	evidenceDigest, err := digestJSON(manifest.Evidence)
	if err != nil {
		return Bindings{}, err
	}
	reviewDigest, err := digestJSON(manifest.Review)
	if err != nil {
		return Bindings{}, err
	}
	return Bindings{ContextSHA256: contextDigest, EvidenceSHA256: evidenceDigest, ReviewSHA256: reviewDigest}, nil
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode delivery identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateDocument(ctx context.Context, schema string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return contracts.Validate(ctx, schema, encoded, false)
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func validGate(gate Gate) bool {
	return gate == GatePostApply || gate == GatePreCommit || gate == GatePrePush || gate == GatePrePR
}

func validateTargetSnapshot(target TargetSnapshot) error {
	if !sort.StringsAreSorted(target.Paths) {
		return fmt.Errorf("%w: target paths are not canonical", ErrCorrupt)
	}
	for index, path := range target.Paths {
		if index > 0 && target.Paths[index-1] == path {
			return fmt.Errorf("%w: duplicate target path", ErrCorrupt)
		}
		if filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path {
			return fmt.Errorf("%w: invalid target path", ErrCorrupt)
		}
		if sensitivepaths.IsSensitive(path) {
			return fmt.Errorf("%w: receipt includes a sensitive path", ErrCorrupt)
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(target.Paths, "\x00")))
	if hex.EncodeToString(digest[:]) != target.PathsSHA256 {
		return fmt.Errorf("%w: target path digest mismatch", ErrCorrupt)
	}
	return nil
}
