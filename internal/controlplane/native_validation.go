package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vgxness/vgxness/internal/bridge"
)

const (
	nativeValidationOutputLimit = 256 << 10
	nativeFormatTimeout         = 30 * time.Second
	nativeTestTimeout           = 2 * time.Minute
	nativeVetTimeout            = 90 * time.Second
)

type nativeValidationSnapshot struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Created bool   `json:"created"`
	Deleted bool   `json:"deleted,omitempty"`
}

type nativeFormattedFile struct {
	path    string
	content string
	before  bridge.NativeEditResult
}

// ValidateNative runs one closed validation operation for the child that owns
// a prepared write ticket. It never accepts an executable, flags, environment,
// or working directory from the caller.
func (service *Service) ValidateNative(ctx context.Context, workspace string, input bridge.NativeValidationRequest) (bridge.Response, error) {
	if err := bridge.ValidateNativeValidation(input); err != nil {
		return bridge.Response{}, err
	}
	root, paths, document, release, err := service.openNativeTicket(ctx, workspace, input.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	ticketReleased := false
	releaseTicket := func() {
		if !ticketReleased {
			release()
			ticketReleased = true
		}
	}
	defer releaseTicket()
	if document.State != "prepared" || document.Input.Operation != bridge.WriteFiles && document.Input.Operation != bridge.RepairSystem ||
		document.Input.ChildSessionID != input.ChildSessionID || document.Edit == nil ||
		service.now().UTC().After(parseNativeDeadline(document.Deadline)) ||
		len(document.Validations) >= nativeMaxValidations {
		return bridge.Response{}, bridge.ErrDenied
	}
	leaseGuard, err := acquireOwnedNativeLeaseGuard(paths.Root, document.TicketID)
	if err != nil {
		return bridge.Response{}, err
	}
	guardReleased := false
	releaseGuard := func() {
		if !guardReleased {
			leaseGuard.Release()
			guardReleased = true
		}
	}
	defer releaseGuard()
	if err := verifyNativeEditWorkspace(document); err != nil {
		return bridge.Response{}, err
	}

	requestData, err := json.Marshal(struct {
		Request      bridge.NativeValidationRequest `json:"request"`
		BaseRevision string                         `json:"baseRevision"`
		Edits        []nativeValidationSnapshot     `json:"edits"`
	}{
		Request: input, BaseRevision: document.Edit.BaseRevision,
		Edits: nativeValidationSnapshots(document.Edits),
	})
	if err != nil {
		return bridge.Response{}, bridge.ErrExecution
	}
	started := service.now().UTC()
	var result bridge.NativeValidationResult
	switch input.Operation {
	case bridge.NativeValidationFormat:
		result, err = runNativeFormat(ctx, document)
	case bridge.NativeValidationTest, bridge.NativeValidationVet:
		result, err = runNativeGoValidation(ctx, document, input)
	default:
		return bridge.Response{}, bridge.ErrInvalid
	}
	if err != nil {
		return bridge.Response{}, err
	}
	finished := service.now().UTC()
	result.Operation = input.Operation
	result.StartedAt = started.Format(time.RFC3339Nano)
	result.FinishedAt = finished.Format(time.RFC3339Nano)
	result.OutputSHA256 = nativeSHA256([]byte(result.Output))

	if input.Operation == bridge.NativeValidationFormat {
		for _, change := range result.Changes {
			replaceNativeEditReceipt(document.Edits, change)
		}
		if document.Input.Operation == bridge.RepairSystem && len(result.Changes) > 0 {
			// Formatting changes the candidate bytes. Earlier test and vet
			// receipts must not authorize the newly formatted result.
			document.Validations = nil
		}
	}
	receipt := bridge.NativeValidationReceipt{
		Operation: input.Operation, Packages: append([]string(nil), result.Packages...),
		InputSHA256: nativeSHA256(requestData), OutputSHA256: result.OutputSHA256,
		Success: result.Success, ExitCode: result.ExitCode,
		StartedAt: result.StartedAt, FinishedAt: result.FinishedAt,
	}
	document.Validations = append(document.Validations, receipt)
	if err := writeNativeTicket(paths.Root, document); err != nil {
		return bridge.Response{}, fmt.Errorf("%w: persist native validation receipt", bridge.ErrExecution)
	}
	releaseGuard()
	releaseTicket()
	service.dispatchValidationCompleted(ctx, document.TicketID, receipt, len(result.Changes))
	return bridge.Response{
		ProtocolVersion: bridge.ProtocolVersion, OK: true, Bridge: "healthy", Provider: "opencode",
		Workspace: root, RunID: document.RunID, TaskID: document.TaskID,
		Status: "validating", Validation: &result,
	}, nil
}

func verifyNativeEditWorkspace(document nativeTicketDocument) error {
	info, err := os.Lstat(document.Edit.Root)
	identity, ok := nativeFileIdentity(info)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ok ||
		identity != document.Edit.RootIdentity {
		return bridge.ErrDenied
	}
	return nil
}

func nativeValidationSnapshots(edits []bridge.NativeEditResult) []nativeValidationSnapshot {
	latest := make(map[string]nativeValidationSnapshot, len(edits))
	for _, edit := range edits {
		item := latest[edit.Path]
		if item.Path == "" {
			item.Path, item.Created = edit.Path, edit.Created
		}
		item.SHA256 = edit.SHA256
		item.Deleted = edit.Deleted
		latest[edit.Path] = item
	}
	result := make([]nativeValidationSnapshot, 0, len(latest))
	for _, item := range latest {
		if item.Created && item.Deleted {
			// A file created and deleted within one ticket has no effect on
			// the committed validation base.
			continue
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func runNativeFormat(ctx context.Context, document nativeTicketDocument) (bridge.NativeValidationResult, error) {
	latest := make(map[string]bridge.NativeEditResult, len(document.Edits))
	for _, edit := range document.Edits {
		previous := latest[edit.Path]
		edit.Created = edit.Created || previous.Created
		if previous.PreviousSHA256 != "" {
			edit.PreviousSHA256 = previous.PreviousSHA256
		}
		latest[edit.Path] = edit
	}
	paths := make([]string, 0, len(latest))
	for path := range latest {
		if filepath.Ext(path) == ".go" && !latest[path].Deleted {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return bridge.NativeValidationResult{}, bridge.ErrDenied
	}
	binary, err := trustedGoTool("gofmt")
	if err != nil {
		return bridge.NativeValidationResult{}, err
	}
	formatContext, cancel := context.WithTimeout(ctx, nativeFormatTimeout)
	defer cancel()
	formatted := make([]nativeFormattedFile, 0, len(paths))
	var diagnostic bytes.Buffer
	for _, path := range paths {
		read, err := secureNativeRead(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeReadRequest{
			Path: path, Limit: bridge.MaxNativeEditBytes,
		})
		if err != nil || read.Truncated || read.SHA256 != latest[path].SHA256 {
			return bridge.NativeValidationResult{}, bridge.ErrDenied
		}
		command := exec.CommandContext(formatContext, binary)
		configureNativeValidationProcess(command)
		command.Stdin = strings.NewReader(read.Content)
		var stdout cappedBuffer
		stdout.limit = bridge.MaxNativeEditBytes
		stderr := cappedBuffer{limit: 4096}
		command.Stdout, command.Stderr = &stdout, &stderr
		runErr := command.Run()
		if runErr != nil || stdout.overflow {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = "gofmt failed"
			}
			diagnostic.WriteString(path + ": " + message)
			return bridge.NativeValidationResult{
				Success: false, ExitCode: nativeExitCode(runErr), Output: boundedValidationText(diagnostic.String()),
			}, nil
		}
		content := stdout.String()
		if content != read.Content {
			formatted = append(formatted, nativeFormattedFile{path: path, content: content, before: latest[path]})
		}
	}
	changes := make([]bridge.NativeEditResult, 0, len(formatted))
	for _, file := range formatted {
		change, err := secureNativeEdit(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeEditRequest{
			Path: file.path, Content: file.content, ExpectedSHA256: file.before.SHA256,
		})
		if err != nil {
			return bridge.NativeValidationResult{}, err
		}
		change.Created = file.before.Created
		change.PreviousSHA256 = file.before.PreviousSHA256
		changes = append(changes, change)
	}
	output := fmt.Sprintf("formatted %d Go file(s); %d changed", len(paths), len(changes))
	return bridge.NativeValidationResult{Success: true, ExitCode: 0, Output: output, Changes: changes}, nil
}

func replaceNativeEditReceipt(edits []bridge.NativeEditResult, replacement bridge.NativeEditResult) {
	for index := len(edits) - 1; index >= 0; index-- {
		if edits[index].Path == replacement.Path {
			edits[index] = replacement
			return
		}
	}
}

func runNativeGoValidation(ctx context.Context, document nativeTicketDocument, input bridge.NativeValidationRequest) (bridge.NativeValidationResult, error) {
	packages := append([]string(nil), input.Packages...)
	if len(packages) == 0 {
		packages = []string{"./..."}
	}
	validationRoot, cleanup, err := materializeNativeValidationWorkspace(ctx, document)
	if err != nil {
		return bridge.NativeValidationResult{}, err
	}
	defer cleanup()
	binary, err := trustedGoTool("go")
	if err != nil {
		return bridge.NativeValidationResult{}, err
	}
	timeout := nativeTestTimeout
	args := []string{"test", "-count=1"}
	if input.Operation == bridge.NativeValidationVet {
		timeout = nativeVetTimeout
		args = []string{"vet"}
	}
	args = append(args, packages...)
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, binary, args...)
	configureNativeValidationProcess(command)
	command.Dir = validationRoot
	command.Env, err = nativeGoEnvironment(validationRoot)
	if err != nil {
		return bridge.NativeValidationResult{}, err
	}
	output := cappedBuffer{limit: nativeValidationOutputLimit}
	command.Stdout, command.Stderr = &output, &output
	runErr := command.Run()
	text := output.String()
	if output.overflow {
		text += "\n[output truncated by VGXNESS]"
	}
	text = sanitizeNativeValidationOutput(text, validationRoot)
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		text = boundedValidationText(text + "\nvalidation timed out")
	}
	return bridge.NativeValidationResult{
		Packages: packages, Success: runErr == nil && !output.overflow,
		ExitCode: nativeExitCode(runErr), Output: boundedValidationText(text),
	}, nil
}

func materializeNativeValidationWorkspace(ctx context.Context, document nativeTicketDocument) (string, func(), error) {
	root, err := os.MkdirTemp("", ".vgxness-validation-*")
	if err != nil {
		return "", nil, bridge.ErrExecution
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	if err := os.Chmod(root, 0o700); err != nil {
		cleanup()
		return "", nil, bridge.ErrExecution
	}
	if err := materializeNativeGitArchive(ctx, document.Edit.Root, root, document.Edit.BaseRevision); err != nil {
		cleanup()
		return "", nil, err
	}
	info, err := os.Lstat(root)
	identity, ok := nativeFileIdentity(info)
	if err != nil || !ok {
		cleanup()
		return "", nil, bridge.ErrExecution
	}
	latest := nativeValidationSnapshots(document.Edits)
	for _, edit := range latest {
		read, err := secureNativeRead(document.Edit.Root, document.Edit.RootIdentity, bridge.NativeReadRequest{
			Path: edit.Path, Limit: bridge.MaxNativeEditBytes,
		})
		if edit.Deleted {
			if !errors.Is(err, os.ErrNotExist) {
				cleanup()
				return "", nil, bridge.ErrDenied
			}
		} else if err != nil || read.Truncated || read.SHA256 != edit.SHA256 {
			cleanup()
			return "", nil, bridge.ErrDenied
		}
		request := bridge.NativeEditRequest{Path: edit.Path, Content: read.Content, Create: edit.Created, Delete: edit.Deleted}
		if !edit.Created {
			base, readErr := secureNativeRead(root, identity, bridge.NativeReadRequest{
				Path: edit.Path, Limit: bridge.MaxNativeEditBytes,
			})
			if readErr != nil || base.Truncated {
				cleanup()
				return "", nil, bridge.ErrDenied
			}
			request.ExpectedSHA256 = base.SHA256
		}
		if _, err := secureNativeEdit(root, identity, request); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return root, cleanup, nil
}

func trustedGoTool(name string) (string, error) {
	if name != "go" && name != "gofmt" {
		return "", bridge.ErrInvalid
	}
	candidates := make([]string, 0, 2)
	if root := runtime.GOROOT(); filepath.IsAbs(root) {
		candidates = append(candidates, filepath.Join(root, "bin", name))
	}
	if goExecutable, err := exec.LookPath("go"); err == nil {
		if absolute, absErr := filepath.Abs(goExecutable); absErr == nil {
			if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
				candidates = append(candidates, filepath.Join(filepath.Dir(resolved), name))
			}
		}
	}
	for _, candidate := range candidates {
		resolved, err := filepath.EvalSymlinks(filepath.Clean(candidate))
		if err != nil || !filepath.IsAbs(resolved) {
			continue
		}
		info, err := os.Stat(resolved)
		if err == nil && info.Mode().IsRegular() {
			return resolved, nil
		}
	}
	return "", bridge.ErrUnavailable
}

func nativeGoEnvironment(validationRoot string) ([]string, error) {
	cache := filepath.Join(validationRoot, ".vgxness-cache")
	home := filepath.Join(cache, "home")
	buildCache := filepath.Join(cache, "build")
	temporary := filepath.Join(cache, "tmp")
	for _, path := range []string{home, buildCache, temporary} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, bridge.ErrExecution
		}
	}
	moduleCache := filepath.Join(cache, "modules")
	if userHome, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(userHome, "go", "pkg", "mod")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			moduleCache = candidate
		}
	}
	return []string{
		"HOME=" + home,
		"TMPDIR=" + temporary,
		"GOCACHE=" + buildCache,
		"GOMODCACHE=" + moduleCache,
		"GOENV=off",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOFLAGS=-mod=readonly -buildvcs=false",
		"CGO_ENABLED=0",
		"PATH=" + filepath.Join(runtime.GOROOT(), "bin") + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"LC_ALL=C",
	}, nil
}

func nativeExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func sanitizeNativeValidationOutput(value, validationRoot string) string {
	value = strings.ReplaceAll(value, validationRoot, "<validation-worktree>")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		value = strings.ReplaceAll(value, home, "<home>")
	}
	return boundedValidationText(value)
}

func boundedValidationText(value string) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= nativeValidationOutputLimit {
		return value
	}
	value = value[:nativeValidationOutputLimit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
