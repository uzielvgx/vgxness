package sdd

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"
)

const (
	MaxOpenSpecContentBytes = 4 << 20
	maxOpenSpecHeaderBytes  = 2048
	openSpecHeaderPrefix    = "<!-- vgxness-sdd\n"
	openSpecHeaderSuffix    = "-->\n"
)

var (
	ErrMalformedProjection  = fmt.Errorf("%w: malformed OpenSpec projection", ErrInvalid)
	ErrProjectionIdentity   = fmt.Errorf("%w: OpenSpec projection identity", ErrConflict)
	ErrProjectionTooLarge   = fmt.Errorf("%w: OpenSpec projection exceeds its bound", ErrInvalid)
	ErrUnsafeProjectionPath = fmt.Errorf("%w: unsafe OpenSpec projection path", ErrInvalid)
)

type ProjectionMetadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	ChangeID      string `json:"changeId"`
	Artifact      Phase  `json:"artifact"`
	RevisionID    string `json:"revisionId"`
	ContentDigest Digest `json:"contentDigest"`
	InputDigest   Digest `json:"inputDigest"`
}

type ProjectionDocument struct {
	RelativePath string             `json:"relativePath"`
	Content      []byte             `json:"content"`
	Digest       Digest             `json:"digest"`
	Metadata     ProjectionMetadata `json:"metadata"`
}

type ParsedProjection struct {
	Metadata ProjectionMetadata `json:"metadata"`
	Content  []byte             `json:"content"`
}

type ProjectionInput struct {
	RelativePath string `json:"relativePath"`
	Content      []byte `json:"content,omitempty"`
	Missing      bool   `json:"missing,omitempty"`
	Symlink      bool   `json:"symlink,omitempty"`
}

type DriftState string

const (
	DriftSynced  DriftState = "synced"
	DriftDrifted DriftState = "drifted"
	DriftMissing DriftState = "missing"
)

type ReconciliationOption string

const (
	ReconcileRenderCanonical   ReconciliationOption = "render_canonical_memory_projection"
	ReconcileInspectDifference ReconciliationOption = "inspect_differences"
	ReconcileSaveCandidate     ReconciliationOption = "save_projection_as_candidate_revision"
)

type ProjectionComparison struct {
	State                DriftState             `json:"state"`
	RelativePath         string                 `json:"relativePath"`
	CanonicalDigest      Digest                 `json:"canonicalDigest"`
	ObservedDigest       Digest                 `json:"observedDigest,omitempty"`
	MemoryCanonical      bool                   `json:"memoryCanonical"`
	Options              []ReconciliationOption `json:"options"`
	RequiresSaveRevision bool                   `json:"requiresSaveRevision"`
	CandidateContent     []byte                 `json:"candidateContent,omitempty"`
	CandidateDigest      Digest                 `json:"candidateDigest,omitempty"`
}

type RenderProjectionRequest struct {
	Project    string `json:"project"`
	ChangeID   string `json:"changeId"`
	RevisionID string `json:"revisionId"`
}

func (request RenderProjectionRequest) Validate() error {
	return (GetRevisionRequest{Project: request.Project, ChangeID: request.ChangeID, RevisionID: request.RevisionID}).Validate()
}

type CompareProjectionRequest struct {
	Project    string          `json:"project"`
	ChangeID   string          `json:"changeId"`
	RevisionID string          `json:"revisionId"`
	Input      ProjectionInput `json:"input"`
}

func (request CompareProjectionRequest) Validate() error {
	if err := (RenderProjectionRequest{Project: request.Project, ChangeID: request.ChangeID, RevisionID: request.RevisionID}).Validate(); err != nil {
		return err
	}
	if request.Input.RelativePath == "" || request.Input.Missing && len(request.Input.Content) != 0 || !request.Input.Missing && len(request.Input.Content) == 0 {
		return ErrInvalid
	}
	return nil
}

func OpenSpecProjectionPath(changeID string, artifact Phase) (string, error) {
	if !safeProjectionID(changeID) {
		return "", ErrUnsafeProjectionPath
	}
	name, ok := map[Phase]string{
		PhaseExplore: "research.md", PhaseProposal: "proposal.md", PhaseSpec: "spec.md",
		PhaseDesign: "design.md", PhaseTasks: "tasks.md", PhaseApply: "apply-result.md", PhaseVerify: "verification.md",
	}[artifact]
	if !ok {
		return "", ErrUnsafeProjectionPath
	}
	relative := path.Join("openspec", "changes", changeID, name)
	if !safeRelativeProjectionPath(relative) {
		return "", ErrUnsafeProjectionPath
	}
	return relative, nil
}

func RenderOpenSpecProjection(revision Revision) (ProjectionDocument, error) {
	if err := validateProjectionRevision(revision); err != nil {
		return ProjectionDocument{}, err
	}
	relative, err := OpenSpecProjectionPath(revision.ChangeID, revision.Artifact)
	if err != nil {
		return ProjectionDocument{}, err
	}
	metadata := ProjectionMetadata{SchemaVersion: 1, ChangeID: revision.ChangeID, Artifact: revision.Artifact, RevisionID: revision.ID, ContentDigest: revision.Digest, InputDigest: revision.InputDigest}
	header := fmt.Sprintf("%sschemaVersion: 1\nchangeId: %s\nartifact: %s\nrevisionId: %s\ncontentDigest: %s\ninputDigest: %s\n%s", openSpecHeaderPrefix, metadata.ChangeID, metadata.Artifact, metadata.RevisionID, metadata.ContentDigest, metadata.InputDigest, openSpecHeaderSuffix)
	content := make([]byte, 0, len(header)+len(revision.Content))
	content = append(content, header...)
	content = append(content, revision.Content...)
	return ProjectionDocument{RelativePath: relative, Content: content, Digest: ContentDigest(content), Metadata: metadata}, nil
}

func ParseOpenSpecProjection(document []byte) (ParsedProjection, error) {
	if len(document) > MaxOpenSpecContentBytes+maxOpenSpecHeaderBytes {
		return ParsedProjection{}, ErrProjectionTooLarge
	}
	if !bytes.HasPrefix(document, []byte(openSpecHeaderPrefix)) {
		return ParsedProjection{}, ErrMalformedProjection
	}
	headerEnd := bytes.Index(document, []byte("\n"+openSpecHeaderSuffix))
	if headerEnd < len(openSpecHeaderPrefix) || headerEnd > maxOpenSpecHeaderBytes {
		return ParsedProjection{}, ErrMalformedProjection
	}
	header := string(document[len(openSpecHeaderPrefix):headerEnd])
	contentOffset := headerEnd + len("\n"+openSpecHeaderSuffix)
	content := append([]byte(nil), document[contentOffset:]...)
	if len(content) > MaxOpenSpecContentBytes {
		return ParsedProjection{}, ErrProjectionTooLarge
	}
	values := make(map[string]string, 6)
	for _, line := range strings.Split(header, "\n") {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || values[parts[0]] != "" {
			return ParsedProjection{}, ErrMalformedProjection
		}
		values[parts[0]] = parts[1]
	}
	if len(values) != 6 || values["schemaVersion"] != "1" {
		return ParsedProjection{}, ErrMalformedProjection
	}
	metadata := ProjectionMetadata{
		SchemaVersion: 1, ChangeID: values["changeId"], Artifact: Phase(values["artifact"]), RevisionID: values["revisionId"],
		ContentDigest: Digest(values["contentDigest"]), InputDigest: Digest(values["inputDigest"]),
	}
	if !safeProjectionID(metadata.ChangeID) || !safeProjectionID(metadata.RevisionID) || !metadata.Artifact.Valid() || metadata.Artifact == PhaseComplete || !metadata.ContentDigest.Valid() || !metadata.InputDigest.Valid() {
		return ParsedProjection{}, ErrMalformedProjection
	}
	if ContentDigest(content) != metadata.ContentDigest {
		return ParsedProjection{}, ErrDigestMismatch
	}
	return ParsedProjection{Metadata: metadata, Content: content}, nil
}

func InspectOpenSpecProjection(expected Revision, input ProjectionInput) (ParsedProjection, error) {
	if err := validateProjectionRevision(expected); err != nil {
		return ParsedProjection{}, err
	}
	relative, err := OpenSpecProjectionPath(expected.ChangeID, expected.Artifact)
	if err != nil {
		return ParsedProjection{}, err
	}
	if input.Symlink || !safeRelativeProjectionPath(input.RelativePath) {
		return ParsedProjection{}, ErrUnsafeProjectionPath
	}
	if input.RelativePath != relative {
		return ParsedProjection{}, ErrProjectionIdentity
	}
	if input.Missing || len(input.Content) == 0 {
		return ParsedProjection{}, ErrNotFound
	}
	parsed, err := ParseOpenSpecProjection(input.Content)
	if err != nil {
		return ParsedProjection{}, err
	}
	if parsed.Metadata.ChangeID != expected.ChangeID || parsed.Metadata.Artifact != expected.Artifact || parsed.Metadata.RevisionID != expected.ID {
		return ParsedProjection{}, ErrProjectionIdentity
	}
	if parsed.Metadata.InputDigest != expected.InputDigest {
		return ParsedProjection{}, ErrInputsChanged
	}
	return parsed, nil
}

func CompareOpenSpecProjection(expected Revision, backend Backend, input ProjectionInput) (ProjectionComparison, error) {
	if backend != BackendOpenSpec && backend != BackendHybrid {
		return ProjectionComparison{}, ErrInvalid
	}
	canonical, err := RenderOpenSpecProjection(expected)
	if err != nil {
		return ProjectionComparison{}, err
	}
	base := ProjectionComparison{RelativePath: canonical.RelativePath, CanonicalDigest: canonical.Digest, MemoryCanonical: backend == BackendHybrid, Options: []ReconciliationOption{}}
	if input.Symlink || !safeRelativeProjectionPath(input.RelativePath) {
		return ProjectionComparison{}, ErrUnsafeProjectionPath
	}
	if input.RelativePath != canonical.RelativePath {
		return ProjectionComparison{}, ErrProjectionIdentity
	}
	if input.Missing {
		if len(input.Content) != 0 {
			return ProjectionComparison{}, ErrInvalid
		}
		base.State = DriftMissing
		base.Options = []ReconciliationOption{ReconcileRenderCanonical}
		return base, nil
	}
	parsed, err := InspectOpenSpecProjection(expected, input)
	if err != nil {
		return ProjectionComparison{}, err
	}
	base.ObservedDigest = ContentDigest(input.Content)
	if bytes.Equal(input.Content, canonical.Content) {
		base.State = DriftSynced
		return base, nil
	}
	base.State = DriftDrifted
	base.Options = []ReconciliationOption{ReconcileRenderCanonical, ReconcileInspectDifference, ReconcileSaveCandidate}
	base.RequiresSaveRevision = true
	base.CandidateContent = append([]byte(nil), parsed.Content...)
	base.CandidateDigest = parsed.Metadata.ContentDigest
	return base, nil
}

func validateProjectionRevision(revision Revision) error {
	if revision.Status != RevisionAccepted || !safeProjectionID(revision.ID) || !safeProjectionID(revision.ArtifactID) {
		return ErrInvalid
	}
	if len(revision.Content) > MaxOpenSpecContentBytes {
		return ErrProjectionTooLarge
	}
	if len(revision.Content) == 0 || !revision.Digest.Valid() || ContentDigest(revision.Content) != revision.Digest {
		return ErrDigestMismatch
	}
	if !revision.InputDigest.Valid() || InputRevisionDigest(revision.Inputs) != revision.InputDigest {
		return ErrInputsChanged
	}
	_, err := OpenSpecProjectionPath(revision.ChangeID, revision.Artifact)
	return err
}

func safeProjectionID(value string) bool {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || index > 0 && (character == '-' || character == '_' || character == '.') {
			continue
		}
		return false
	}
	return true
}

func safeRelativeProjectionPath(value string) bool {
	return value != "" && len(value) <= 512 && !strings.Contains(value, "\\") && !strings.HasPrefix(value, "/") && path.Clean(value) == value && strings.HasPrefix(value, "openspec/changes/")
}

func (service Service) RenderProjection(ctx context.Context, request RenderProjectionRequest) (ProjectionDocument, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return ProjectionDocument{}, err
	}
	revision, err := service.repository.GetRevision(ctx, GetRevisionRequest{Project: request.Project, ChangeID: request.ChangeID, RevisionID: request.RevisionID})
	if err != nil {
		return ProjectionDocument{}, err
	}
	if revision.Project != request.Project || revision.ChangeID != request.ChangeID || revision.ID != request.RevisionID {
		return ProjectionDocument{}, ErrProjectionIdentity
	}
	return RenderOpenSpecProjection(revision)
}

func (service Service) CompareProjection(ctx context.Context, request CompareProjectionRequest) (ProjectionComparison, error) {
	if err := check(ctx, request.Validate(), service.repository); err != nil {
		return ProjectionComparison{}, err
	}
	change, err := service.repository.GetChange(ctx, GetChangeRequest{Project: request.Project, ID: request.ChangeID})
	if err != nil {
		return ProjectionComparison{}, err
	}
	revision, err := service.repository.GetRevision(ctx, GetRevisionRequest{Project: request.Project, ChangeID: request.ChangeID, RevisionID: request.RevisionID})
	if err != nil {
		return ProjectionComparison{}, err
	}
	if change.Project != request.Project || change.ID != request.ChangeID || revision.Project != request.Project || revision.ChangeID != request.ChangeID || revision.ID != request.RevisionID {
		return ProjectionComparison{}, ErrProjectionIdentity
	}
	return CompareOpenSpecProjection(revision, change.Backend, request.Input)
}
