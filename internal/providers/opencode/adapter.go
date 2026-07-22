package opencode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/chronicle"
	"github.com/vgxness/vgxness/internal/gatekeeper"
	"github.com/vgxness/vgxness/internal/providers"
	"github.com/vgxness/vgxness/internal/registry"
	"github.com/vgxness/vgxness/internal/sensitivepaths"
)

const (
	runtimeAgentPrefix    = "vgxness-runtime-"
	runtimeMessage        = "VGXNESS_EXECUTE_SYSTEM_CONTRACT_RETURN_AGENT_RESULT_JSON"
	defaultMinimumVersion = "1.18.4"
	defaultMaxOutputBytes = 8 << 20
	defaultMaxSteps       = 32
)

var ErrInvalidConfig = errors.New("invalid OpenCode adapter config")

// Config describes the installed OpenCode transport. Provider identity and
// capabilities remain registry-owned; this adapter only binds them to a local
// executable and a maximum workspace root.
type Config struct {
	Reference        registry.ProviderReference
	InterfaceVersion string
	Capabilities     []registry.Capability
	Executable       string
	WorkingDirectory string
	DefaultModel     string
	Variant          string
	MinimumVersion   string
	MaxOutputBytes   int
	MaxSteps         int
}

type Adapter struct {
	descriptor     providers.Descriptor
	executable     string
	workspace      string
	defaultModel   string
	variant        string
	minimumVersion string
	maxOutputBytes int
	maxSteps       int
	executor       processExecutor
	now            func() time.Time
}

type processRequest struct {
	Executable  string
	Args        []string
	Directory   string
	Environment []string
	MaxBytes    int
}

type processResult struct {
	Stdout         []byte
	Stderr         []byte
	StdoutOverflow bool
	StderrOverflow bool
}

type processExecutor interface {
	Run(context.Context, processRequest) (processResult, error)
}

type commandExecutor struct{}

func New(config Config) (*Adapter, error) {
	return newAdapter(config, commandExecutor{}, exec.LookPath)
}

func newAdapter(config Config, executor processExecutor, lookPath func(string) (string, error)) (*Adapter, error) {
	if executor == nil || lookPath == nil {
		return nil, ErrInvalidConfig
	}
	if _, production := executor.(commandExecutor); production && !supportsDescendantCancellation() {
		return nil, fmt.Errorf("%w: process-tree cancellation unsupported", ErrInvalidConfig)
	}
	executable := strings.TrimSpace(config.Executable)
	if executable == "" {
		executable = "opencode"
	}
	resolved, err := lookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("%w: executable unavailable", ErrInvalidConfig)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, fmt.Errorf("%w: executable path", ErrInvalidConfig)
	}
	workspace, err := filepath.Abs(config.WorkingDirectory)
	if err != nil || strings.TrimSpace(config.WorkingDirectory) == "" {
		return nil, fmt.Errorf("%w: working directory", ErrInvalidConfig)
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: working directory unavailable", ErrInvalidConfig)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, fmt.Errorf("%w: working directory resolution", ErrInvalidConfig)
	}
	minimumVersion := strings.TrimPrefix(strings.TrimSpace(config.MinimumVersion), "v")
	if minimumVersion == "" {
		minimumVersion = defaultMinimumVersion
	}
	if _, ok := parseVersion(minimumVersion); !ok {
		return nil, fmt.Errorf("%w: minimum version", ErrInvalidConfig)
	}
	maxOutputBytes := config.MaxOutputBytes
	if maxOutputBytes == 0 {
		maxOutputBytes = defaultMaxOutputBytes
	}
	maxSteps := config.MaxSteps
	if maxSteps == 0 {
		maxSteps = defaultMaxSteps
	}
	if maxOutputBytes < 1024 || maxOutputBytes > defaultMaxOutputBytes || maxSteps < 1 || maxSteps > 256 {
		return nil, fmt.Errorf("%w: execution bounds", ErrInvalidConfig)
	}
	descriptor := providers.Descriptor{
		Reference:        config.Reference,
		Source:           registry.SourceReference{Provider: "filesystem", ID: "opencode-cli", Path: resolved},
		InterfaceVersion: config.InterfaceVersion,
		Capabilities:     cloneCapabilities(config.Capabilities),
	}
	if !validDescriptor(descriptor) || strings.TrimSpace(config.DefaultModel) != "" && !validModel(config.DefaultModel) || !validVariant(config.Variant) {
		return nil, ErrInvalidConfig
	}
	return &Adapter{
		descriptor: descriptor, executable: resolved, workspace: filepath.Clean(workspace),
		defaultModel: strings.TrimSpace(config.DefaultModel), variant: strings.TrimSpace(config.Variant),
		minimumVersion: minimumVersion, maxOutputBytes: maxOutputBytes, maxSteps: maxSteps,
		executor: executor, now: time.Now,
	}, nil
}

func (a *Adapter) Descriptor() providers.Descriptor {
	if a == nil {
		return providers.Descriptor{}
	}
	return providers.Descriptor{
		Reference: a.descriptor.Reference, Source: a.descriptor.Source,
		InterfaceVersion: a.descriptor.InterfaceVersion, Capabilities: cloneCapabilities(a.descriptor.Capabilities),
	}
}

func (a *Adapter) Health(ctx context.Context) providers.Health {
	checkedAt := time.Now().UTC()
	if a != nil && a.now != nil {
		checkedAt = a.now().UTC()
	}
	if a == nil || a.executor == nil || ctx.Err() != nil {
		return providers.Health{Status: gatekeeper.AdapterUnavailable, CheckedAt: checkedAt}
	}
	result, err := a.executor.Run(ctx, processRequest{
		Executable: a.executable, Args: []string{"--version"}, Directory: a.workspace,
		Environment: runtimeEnvironment(nil, nil, ""), MaxBytes: 4096,
	})
	if err != nil || result.StdoutOverflow || result.StderrOverflow {
		return providers.Health{Status: gatekeeper.AdapterUnavailable, CheckedAt: checkedAt}
	}
	version, ok := parseVersion(strings.TrimSpace(string(result.Stdout)))
	minimum, _ := parseVersion(a.minimumVersion)
	if !ok || version[0] != minimum[0] || compareVersion(version, minimum) < 0 {
		return providers.Health{Status: gatekeeper.AdapterIncompatible, CheckedAt: checkedAt}
	}
	return providers.Health{Status: gatekeeper.AdapterHealthy, CheckedAt: checkedAt}
}

func (a *Adapter) Run(ctx context.Context, invocation providers.Invocation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	packet, workingDirectory, err := a.validateInvocation(invocation)
	if err != nil {
		return nil, err
	}
	permissions, err := buildPermissions(invocation, packet)
	if err != nil {
		return nil, err
	}
	configData, permissionData, err := a.runtimeConfig(invocation, permissions)
	if err != nil {
		return nil, &providers.Failure{Category: providers.FailureInvalidResult}
	}
	model := strings.TrimSpace(invocation.Agent.Model)
	if model == "" {
		model = a.defaultModel
	}
	if model != "" && !validModel(model) {
		return nil, &providers.Failure{Category: providers.FailureIncompatible}
	}
	configDirectory, err := os.MkdirTemp("", "vgxness-opencode-config-")
	if err != nil {
		return nil, &providers.Failure{Category: providers.FailureUnavailable, Recoverable: true}
	}
	defer os.RemoveAll(configDirectory)
	runtimeAgentName := runtimeAgentID(invocation.ExecutionID)
	args := []string{
		"run", "--pure", "--format", "json", "--agent", runtimeAgentName,
		"--dir", workingDirectory, "--title", "vgxness-" + invocation.ExecutionID,
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if a.variant != "" {
		args = append(args, "--variant", a.variant)
	}
	args = append(args, runtimeMessage)
	result, runErr := a.executor.Run(ctx, processRequest{
		Executable: a.executable, Args: args, Directory: workingDirectory,
		Environment: runtimeEnvironment(configData, permissionData, configDirectory), MaxBytes: a.maxOutputBytes,
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runErr != nil {
		return nil, &providers.Failure{Category: providers.FailureUnavailable, Recoverable: true}
	}
	if result.StdoutOverflow || result.StderrOverflow {
		return nil, &providers.Failure{Category: providers.FailureInvalidResult}
	}
	return parseResult(result.Stdout, a.maxOutputBytes)
}

type packetScope struct {
	Context struct {
		TaskID       string   `json:"taskId"`
		AllowedPaths []string `json:"allowedPaths"`
		AllowedTools []string `json:"allowedTools"`
		Inputs       struct {
			Operation string `json:"operation"`
		} `json:"inputs"`
		Scope struct {
			Excluded []string `json:"excluded"`
		} `json:"scope"`
	} `json:"context"`
}

func (a *Adapter) validateInvocation(invocation providers.Invocation) (packetScope, string, error) {
	invalid := func() (packetScope, string, error) {
		return packetScope{}, "", &providers.Failure{Category: providers.FailureInvalidResult}
	}
	if a == nil || a.executor == nil || invocation.ExecutionID == "" || invocation.CorrelationID == "" || invocation.WorkUnitID == "" {
		return invalid()
	}
	if invocation.ExecutionID != invocation.CorrelationID || invocation.Agent.Provider != a.descriptor.Reference {
		return invalid()
	}
	if invocation.Mode != chronicle.TaskForeground && invocation.Mode != chronicle.TaskBackground {
		return invalid()
	}
	if invocation.Operation == "" || !containsOperation(invocation.AuthorizedOperations, invocation.Operation) {
		return invalid()
	}
	if invocation.Mode == chronicle.TaskBackground && invocation.Operation != gatekeeper.ReadFiles {
		return packetScope{}, "", &providers.Failure{Category: providers.FailurePermissionDenied}
	}
	if invocation.Prompt.AgentID != invocation.Agent.ID || invocation.Prompt.PromptRef != invocation.Agent.PromptRef || invocation.Prompt.System == "" {
		return invalid()
	}
	digest := sha256.Sum256([]byte(invocation.Prompt.System))
	if invocation.Prompt.SHA256 != hex.EncodeToString(digest[:]) || !json.Valid([]byte(invocation.Prompt.System)) {
		return invalid()
	}
	var packet packetScope
	decoder := json.NewDecoder(bytes.NewReader(invocation.Packet))
	if err := decoder.Decode(&packet); err != nil || packet.Context.TaskID != invocation.WorkUnitID || len(packet.Context.AllowedPaths) != 1 {
		return invalid()
	}
	if !filepath.IsAbs(packet.Context.AllowedPaths[0]) {
		return packetScope{}, "", &providers.Failure{Category: providers.FailurePermissionDenied}
	}
	workingDirectory, err := filepath.EvalSymlinks(filepath.Clean(packet.Context.AllowedPaths[0]))
	if err != nil || !within(a.workspace, workingDirectory) {
		return packetScope{}, "", &providers.Failure{Category: providers.FailurePermissionDenied}
	}
	info, err := os.Stat(workingDirectory)
	if err != nil || !info.IsDir() {
		return packetScope{}, "", &providers.Failure{Category: providers.FailurePermissionDenied}
	}
	declaredRoot := filepath.Clean(packet.Context.AllowedPaths[0])
	for index, excluded := range packet.Context.Scope.Excluded {
		if !filepath.IsAbs(excluded) || !within(declaredRoot, excluded) {
			return packetScope{}, "", &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		relative, pathErr := filepath.Rel(declaredRoot, filepath.Clean(excluded))
		if pathErr != nil {
			return packetScope{}, "", &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		packet.Context.Scope.Excluded[index] = filepath.Join(workingDirectory, relative)
	}
	return packet, filepath.Clean(workingDirectory), nil
}

func (a *Adapter) runtimeConfig(invocation providers.Invocation, permissions map[string]any) ([]byte, []byte, error) {
	runtimeAgentName := runtimeAgentID(invocation.ExecutionID)
	configuration := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"share":   "disabled",
		"agent": map[string]any{
			runtimeAgentName: map[string]any{
				"description": "Ephemeral VGXNESS provider execution boundary",
				"mode":        "primary", "prompt": invocation.Prompt.System,
				"steps": a.maxSteps, "permission": permissions,
			},
		},
	}
	configData, err := json.Marshal(configuration)
	if err != nil {
		return nil, nil, err
	}
	permissionData, err := json.Marshal(permissions)
	if err != nil {
		return nil, nil, err
	}
	return configData, permissionData, nil
}

func buildPermissions(invocation providers.Invocation, packet packetScope) (map[string]any, error) {
	permissions := map[string]any{
		"*": "deny", "edit": "deny", "bash": "deny", "task": "deny", "skill": "deny",
		"external_directory": "deny", "todowrite": "deny", "webfetch": "deny", "websearch": "deny",
		"lsp": "deny", "question": "deny", "doom_loop": "deny",
	}
	reviewChanges := packet.Context.Inputs.Operation == "review-changes"
	readAllowed := !reviewChanges && invocation.Agent.Permissions.MayReadFiles && containsOperation(invocation.AuthorizedOperations, gatekeeper.ReadFiles) && !deniesAny(invocation.Agent.Permissions, "read")
	if readAllowed {
		permissions["grep"] = "deny"
		permissions["read"] = pathRules(packet.Context.Scope.Excluded)
		if !deniesAny(invocation.Agent.Permissions, "glob") {
			permissions["glob"] = "allow"
		} else {
			permissions["glob"] = "deny"
		}
		if !deniesAny(invocation.Agent.Permissions, "list") {
			permissions["list"] = "allow"
		} else {
			permissions["list"] = "deny"
		}
	} else {
		permissions["read"] = "deny"
		permissions["glob"] = "deny"
		permissions["grep"] = "deny"
		permissions["list"] = "deny"
	}
	if invocation.Mode == chronicle.TaskBackground {
		return permissions, nil
	}
	switch invocation.Operation {
	case gatekeeper.ReadFiles:
	case gatekeeper.WriteFiles, gatekeeper.Configuration, gatekeeper.DestructiveFiles:
		if !invocation.Agent.Permissions.MayWriteFiles || deniesAny(invocation.Agent.Permissions, "edit", "write", "apply_patch") {
			return nil, &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		permissions["edit"] = pathRules(packet.Context.Scope.Excluded)
	case gatekeeper.RunCommand:
		tools := effectiveCommandTools(packet.Context.AllowedTools, invocation.Agent.Permissions)
		if !invocation.Agent.Permissions.MayRunCommands || len(tools) == 0 {
			return nil, &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		permissions["bash"] = commandRules(tools, invocation.Agent.Permissions, false, false, false)
	case gatekeeper.InstallPackage:
		tools := effectiveCommandTools(packet.Context.AllowedTools, invocation.Agent.Permissions)
		if !invocation.Agent.Permissions.MayRunCommands || !invocation.Agent.Permissions.MayInstallPackages || !containsAnyFold(tools, "shell", "bash") {
			return nil, &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		permissions["bash"] = commandRules(tools, invocation.Agent.Permissions, true, false, false)
	case gatekeeper.Commit:
		if !invocation.Agent.Permissions.MayRunCommands || !invocation.Agent.Permissions.MayCommit || !containsFold(packet.Context.AllowedTools, "git") || !toolAllowed("git", invocation.Agent.Permissions) || deniesAny(invocation.Agent.Permissions, "bash", "shell") {
			return nil, &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		permissions["bash"] = commandRules([]string{"git"}, invocation.Agent.Permissions, false, true, false)
	case gatekeeper.Push:
		if !invocation.Agent.Permissions.MayRunCommands || !invocation.Agent.Permissions.MayPush || !invocation.Agent.Permissions.MayUseNetwork || !containsFold(packet.Context.AllowedTools, "git") || !toolAllowed("git", invocation.Agent.Permissions) || deniesAny(invocation.Agent.Permissions, "bash", "shell") {
			return nil, &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		permissions["bash"] = commandRules([]string{"git"}, invocation.Agent.Permissions, false, false, true)
	case gatekeeper.Network:
		if !invocation.Agent.Permissions.MayUseNetwork || deniesAny(invocation.Agent.Permissions, "webfetch", "websearch") {
			return nil, &providers.Failure{Category: providers.FailurePermissionDenied}
		}
		permissions["webfetch"] = "allow"
		permissions["websearch"] = "allow"
	default:
		return nil, &providers.Failure{Category: providers.FailureIncompatible}
	}
	return permissions, nil
}

func pathRules(excluded []string) map[string]string {
	rules := map[string]string{"*": "allow"}
	for _, pattern := range sensitivepaths.OpenCodeDenyPatterns() {
		rules[pattern] = "deny"
	}
	for _, path := range excluded {
		cleaned := filepath.Clean(path)
		rules[cleaned] = "deny"
		rules[filepath.Join(cleaned, "**")] = "deny"
	}
	return rules
}

func commandRules(allowedTools []string, ceiling registry.Permissions, install, commit, push bool) any {
	rules := map[string]string{"*": "deny"}
	if containsFold(allowedTools, "shell") || containsFold(allowedTools, "bash") {
		rules["*"] = "allow"
	}
	if containsFold(allowedTools, "git") {
		rules["git *"] = "allow"
	}
	if install {
		for _, pattern := range []string{"bun add *", "go get *", "npm install *", "pnpm add *", "pip install *", "uv add *", "yarn add *"} {
			rules[pattern] = "allow"
		}
	}
	if !ceiling.MayInstallPackages || !install {
		for _, pattern := range []string{"bun add *", "go get *", "npm install *", "pnpm add *", "pip install *", "uv add *", "yarn add *"} {
			rules[pattern] = "deny"
		}
	}
	if commit {
		rules["git commit*"] = "allow"
	} else {
		rules["git commit*"] = "deny"
	}
	if push {
		rules["git push*"] = "allow"
	} else {
		rules["git push*"] = "deny"
	}
	return rules
}

type jsonEvent struct {
	Type string `json:"type"`
	Part struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"part"`
}

func parseResult(stdout []byte, maxBytes int) ([]byte, error) {
	if len(stdout) == 0 || len(stdout) > maxBytes {
		return nil, &providers.Failure{Category: providers.FailureInvalidResult}
	}
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 64<<10), maxBytes)
	var terminalText string
	foundText := false
	seen := map[string]string{}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event jsonEvent
		if err := json.Unmarshal(line, &event); err != nil || event.Type == "" {
			return nil, &providers.Failure{Category: providers.FailureInvalidResult}
		}
		if event.Type == "error" {
			return nil, &providers.Failure{Category: providers.FailureUnavailable, Recoverable: true}
		}
		if event.Type != "text" || event.Part.Type != "text" || strings.TrimSpace(event.Part.Text) == "" {
			continue
		}
		if event.Part.ID != "" {
			if previous, duplicate := seen[event.Part.ID]; duplicate {
				if previous == strings.TrimSpace(event.Part.Text) {
					continue
				}
				return nil, &providers.Failure{Category: providers.FailureInvalidResult}
			}
			seen[event.Part.ID] = strings.TrimSpace(event.Part.Text)
		}
		terminalText = strings.TrimSpace(event.Part.Text)
		foundText = true
	}
	if scanner.Err() != nil || !foundText {
		return nil, &providers.Failure{Category: providers.FailureInvalidResult}
	}
	result := []byte(terminalText)
	decoder := json.NewDecoder(bytes.NewReader(result))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, &providers.Failure{Category: providers.FailureInvalidResult}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, &providers.Failure{Category: providers.FailureInvalidResult}
	}
	return append([]byte(nil), result...), nil
}

func (commandExecutor) Run(ctx context.Context, request processRequest) (processResult, error) {
	command := exec.CommandContext(ctx, request.Executable, request.Args...)
	configureProcessTree(command)
	command.Dir = request.Directory
	command.Env = request.Environment
	stdout := newCappedBuffer(request.MaxBytes)
	stderr := newCappedBuffer(request.MaxBytes)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result := processResult{
		Stdout: stdout.Bytes(), Stderr: stderr.Bytes(),
		StdoutOverflow: stdout.Overflow(), StderrOverflow: stderr.Overflow(),
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	return result, err
}

type cappedBuffer struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

func newCappedBuffer(limit int) *cappedBuffer { return &cappedBuffer{limit: limit} }

func (b *cappedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.data.Len()
	if remaining > 0 {
		chunk := value
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		_, _ = b.data.Write(chunk)
	}
	if len(value) > remaining {
		b.overflow = true
	}
	return written, nil
}

func (b *cappedBuffer) Bytes() []byte  { return append([]byte(nil), b.data.Bytes()...) }
func (b *cappedBuffer) Overflow() bool { return b.overflow }

func runtimeEnvironment(configData, permissionData []byte, configDirectory string) []string {
	overrides := map[string]string{
		"OPENCODE_AUTO_SHARE": "false", "OPENCODE_DISABLE_AUTOUPDATE": "true",
		"OPENCODE_DISABLE_CLAUDE_CODE":     "true",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS": "false", "OPENCODE_DISABLE_LSP_DOWNLOAD": "true",
		"OPENCODE_DISABLE_PRUNE": "true", "OPENCODE_DISABLE_TERMINAL_TITLE": "true",
		"OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS": "false", "OPENCODE_YOLO": "false",
	}
	removed := map[string]struct{}{
		"OPENCODE_CONFIG": {}, "OPENCODE_CONFIG_CONTENT": {},
		"OPENCODE_CONFIG_DIR": {}, "OPENCODE_PERMISSION": {},
	}
	if configDirectory != "" {
		overrides["OPENCODE_CONFIG_DIR"] = configDirectory
	}
	if len(configData) != 0 {
		overrides["OPENCODE_CONFIG_CONTENT"] = string(configData)
	}
	if len(permissionData) != 0 {
		overrides["OPENCODE_PERMISSION"] = string(permissionData)
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if _, discard := removed[name]; found && discard {
			continue
		}
		if _, replaced := overrides[name]; found && replaced {
			continue
		}
		environment = append(environment, entry)
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func validDescriptor(descriptor providers.Descriptor) bool {
	if descriptor.Reference.Provider != "opencode" || descriptor.Reference.ID == "" || descriptor.Reference.Version == "" || descriptor.InterfaceVersion == "" || len(descriptor.Capabilities) == 0 {
		return false
	}
	for _, capability := range descriptor.Capabilities {
		if capability.Capability == "" || capability.Version == "" {
			return false
		}
	}
	return true
}

func validModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	if len(model) > 512 {
		return false
	}
	provider, modelID, found := strings.Cut(model, "/")
	return found && provider != "" && modelID != "" && !strings.HasPrefix(model, "-") && !strings.Contains(modelID, "/") && !strings.ContainsAny(model, " \t\r\n\x00")
}

func validVariant(variant string) bool {
	return !strings.HasPrefix(variant, "-") && !strings.ContainsAny(variant, " \t\r\n\x00")
}

func containsOperation(values []gatekeeper.OperationClass, target gatekeeper.OperationClass) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func containsAnyFold(values []string, targets ...string) bool {
	for _, target := range targets {
		if containsFold(values, target) {
			return true
		}
	}
	return false
}

func deniesAny(permissions registry.Permissions, tools ...string) bool {
	for _, tool := range tools {
		if containsFold(permissions.DeniedTools, tool) {
			return true
		}
	}
	return false
}

func toolAllowed(tool string, permissions registry.Permissions) bool {
	if deniesAny(permissions, tool) {
		return false
	}
	return len(permissions.AllowedTools) == 0 || containsFold(permissions.AllowedTools, tool)
}

func effectiveCommandTools(packetTools []string, permissions registry.Permissions) []string {
	effective := make([]string, 0, 3)
	for _, tool := range []string{"shell", "bash", "git"} {
		if containsFold(packetTools, tool) && toolAllowed(tool, permissions) && !deniesAny(permissions, "bash") {
			effective = append(effective, tool)
		}
	}
	return effective
}

func runtimeAgentID(executionID string) string {
	digest := sha256.Sum256([]byte(executionID))
	return runtimeAgentPrefix + hex.EncodeToString(digest[:6])
}

func within(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func cloneCapabilities(capabilities []registry.Capability) []registry.Capability {
	data, _ := json.Marshal(capabilities)
	var clone []registry.Capability
	_ = json.Unmarshal(data, &clone)
	return clone
}

type semanticVersion [3]int

func parseVersion(value string) (semanticVersion, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if fields := strings.Fields(value); len(fields) > 0 {
		value = fields[0]
	}
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}
	var version semanticVersion
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semanticVersion{}, false
		}
		version[index] = number
	}
	return version, true
}

func compareVersion(left, right semanticVersion) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
