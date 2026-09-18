package opencode

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"

	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/orchestration"
)

// OrchestrationContractIdentity identifies the provider-neutral policy used by
// this provider without changing OpenCode's native prompt or tool semantics.
func OrchestrationContractIdentity() string { return orchestration.ContractIdentity }

const (
	managerAgentName  = "vgxness-manager.md"
	exploreAgentName  = "explore.md"
	generalAgentName  = "general.md"
	verifierAgentName = "vgxness-verifier.md"

	memoryPluginName             = "vgxness.ts"
	memoryLifecyclePluginName    = "vgxness-memory-lifecycle.ts"
	autonomousStackedPRSkillName = "vgxness-autonomous-stacked-pr"
	defaultAgentName             = "vgxness-manager"
	reinstallCheckpointMoved     = "moved"
	reinstallCheckpointPublished = "published"
	reinstallCheckpointVerified  = "verified"
	defaultAgentConfigName       = "opencode.json"
	defaultAgentStateName        = "default-agent.json"
	maxDefaultAgentBytes         = 4 * 1024
	maxArtifactBytes             = 512 * 1024
)

type Integration struct {
	now                       func() time.Time
	executable                string
	prospectiveLauncher       bool
	reinstallCheckpoint       func(string, string) error
	afterDefaultAgentSnapshot func()
	afterReinstallAnchorPath  func(string)
	afterReinstallStaging     func([]installedArtifact)
	afterRetirement           func() error
	afterInstallRoot          func(*rootTransaction)
	afterUninstallRoot        func(*rootTransaction)
	afterMCPRepairRoot        func(*rootTransaction)
}

var currentExecutable = os.Executable

type artifact struct {
	path                        string
	retainedRoot                string
	content                     []byte
	backup                      string
	present                     bool
	exact                       bool
	upgrade                     bool
	prior                       []byte
	predecessors                [][]byte
	regenerations               [][]byte
	recognize                   func([]byte) bool
	defaultAgent                *defaultAgentState
	defaultAgentSnapshotPresent bool
	defaultState                bool
}

type defaultAgentState struct {
	SchemaVersion       int             `json:"schema_version,omitempty"`
	ConfigExisted       bool            `json:"config_existed"`
	DefaultAgentExisted bool            `json:"default_agent_existed"`
	DefaultAgent        json.RawMessage `json:"default_agent,omitempty"`
	MCPExisted          bool            `json:"mcp_existed,omitempty"`
	MCP                 json.RawMessage `json:"mcp,omitempty"`
	MCPOwned            bool            `json:"mcp_owned,omitempty"`
	PermissionExisted   bool            `json:"permission_existed,omitempty"`
	Permission          json.RawMessage `json:"permission,omitempty"`
	PermissionOwned     bool            `json:"permission_owned,omitempty"`
	Drifted             bool            `json:"-"`
}

type inspection struct {
	result    integration.Result
	artifacts []artifact
	retired   []retiredArtifact
}

type retiredArtifact struct {
	path      string
	content   []byte
	recognize func([]byte) bool
}

type installedArtifact struct {
	path          string
	temporary     string
	temporaryInfo os.FileInfo
	staging       string
	stagingInfo   os.FileInfo
	content       []byte
}

type rootInstalledArtifact struct {
	name        string
	staged      rootStagedArtifact
	published   os.FileInfo
	publication rootPublication
	backup      *rootArtifactBackup
	retained    bool
}

type rootRetiredArtifact struct {
	name   string
	backup rootArtifactBackup
}

type rootBackedUpArtifact struct {
	name   string
	backup rootArtifactBackup
}

type rootDefaultAgentUninstall struct {
	replacement *rootInstalledArtifact
	removal     *rootBackedUpArtifact
}

func NewIntegration() *Integration {
	executable, _ := currentExecutable()
	return newIntegration(executable, os.Getenv("VGXNESS_LAUNCHER"))
}

func newIntegration(executable, configuredLauncher string) *Integration {
	if executable != "" {
		executable, _ = filepath.Abs(executable)
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
	}
	if stable := trustedLauncher(executable, configuredLauncher); stable != "" {
		executable = stable
	}
	return &Integration{now: time.Now, executable: executable}
}

func NewManagedIntegration(executable string) (*Integration, error) {
	managed, err := validateManagedLauncher(executable)
	if err != nil {
		return nil, fmt.Errorf("%w: managed VGXNESS launcher", integration.ErrInvalid)
	}
	return &Integration{now: time.Now, executable: managed}, nil
}

// NewPreviewIntegration binds a prospective managed launcher without claiming
// filesystem ownership. It is safe to use before shared launcher publication.
func NewPreviewIntegration(executable string) (*Integration, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" || !filepath.IsAbs(executable) || executable != filepath.Clean(executable) {
		return nil, fmt.Errorf("%w: preview VGXNESS launcher", integration.ErrInvalid)
	}
	return &Integration{now: time.Now, executable: executable, prospectiveLauncher: true}, nil
}

func validateManagedLauncher(candidate string) (string, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || !filepath.IsAbs(candidate) {
		return "", launcher.ErrInvalid
	}
	candidate = filepath.Clean(candidate)
	manifest, err := launcher.Load(candidate)
	if err != nil {
		return "", err
	}
	launcherDigest, err := launcher.FileSHA256(candidate)
	if err != nil || launcherDigest != manifest.LauncherSHA256 {
		return "", launcher.ErrInvalid
	}
	activeDigest, err := launcher.FileSHA256(manifest.ActivePath)
	if err != nil || activeDigest != manifest.ActiveSHA256 {
		return "", launcher.ErrInvalid
	}
	return candidate, nil
}

func (service *Integration) validateMutableLauncher() error {
	managed, err := validateManagedLauncher(service.executable)
	if err != nil {
		return fmt.Errorf("%w: managed VGXNESS launcher: %w", integration.ErrInvalid, err)
	}
	service.executable = managed
	return nil
}

func trustedLauncher(activeExecutable, candidate string) string {
	candidate, err := validateManagedLauncher(candidate)
	if err != nil {
		return ""
	}
	manifest, _ := launcher.Load(candidate)
	activeInfo, activeErr := os.Stat(activeExecutable)
	managedInfo, managedErr := os.Stat(manifest.ActivePath)
	if activeErr != nil || managedErr != nil || !os.SameFile(activeInfo, managedInfo) {
		return ""
	}
	return candidate
}

func (service *Integration) Preview(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options)
	if err != nil {
		return integration.Result{}, err
	}
	state.result.Changed = state.result.State == integration.StateAbsent || state.result.State == integration.StatePartial
	state.result.RestartRequired = state.result.Changed
	return state.result, nil
}

func (service *Integration) Status(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options)
	return state.result, err
}

func (service *Integration) ManagedLayout(ctx context.Context, options integration.Options) (integration.ManagedLayout, error) {
	state, err := service.inspect(ctx, options)
	if err != nil {
		return integration.ManagedLayout{}, err
	}
	root, err := integrationConfigDirectory(options)
	if err != nil {
		return integration.ManagedLayout{}, err
	}
	return managedLayout(root, state.artifacts)
}

func managedLayout(root string, artifacts []artifact) (integration.ManagedLayout, error) {
	layout := integration.ManagedLayout{Root: root, Artifacts: make([]integration.ManagedArtifact, 0, len(artifacts))}
	for _, item := range artifacts {
		if item.defaultAgent != nil {
			continue
		}
		relative, err := filepath.Rel(root, item.path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return integration.ManagedLayout{}, fmt.Errorf("%w: managed OpenCode artifact path", integration.ErrInvalid)
		}
		layout.Artifacts = append(layout.Artifacts, integration.ManagedArtifact{
			RelativePath: filepath.ToSlash(relative),
			SHA256:       artifactSHA256(item.content),
		})
	}
	sort.Slice(layout.Artifacts, func(i, j int) bool { return layout.Artifacts[i].RelativePath < layout.Artifacts[j].RelativePath })
	hash := sha256.New()
	previous := ""
	for _, item := range layout.Artifacts {
		if item.RelativePath == previous {
			return integration.ManagedLayout{}, fmt.Errorf("%w: duplicate managed OpenCode artifact", integration.ErrInvalid)
		}
		_, _ = io.WriteString(hash, item.RelativePath)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, item.SHA256)
		_, _ = hash.Write([]byte{'\n'})
		previous = item.RelativePath
	}
	layout.AggregateSHA256 = hex.EncodeToString(hash.Sum(nil))
	return layout, nil
}

// Reinstall atomically regenerates the recognized managed set without touching
// unrelated OpenCode files or creating the legacy uninstall backup directory.
func (service *Integration) Reinstall(ctx context.Context, options integration.Options) (_ integration.Result, returnErr error) {
	if err := service.validateMutableLauncher(); err != nil {
		return integration.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	configDirectory, err := integrationConfigDirectory(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := openRootTransaction(configDirectory, false)
	if err != nil {
		return integration.Result{}, fmt.Errorf("%w: open OpenCode config root: %v", integration.ErrConflict, err)
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	transaction, err := service.preflightReinstall(ctx, options, configDirectory, root)
	if err != nil {
		return integration.Result{}, err
	}
	rollback := true
	defer func() {
		returnErr = transaction.finish(returnErr, rollback)
	}()
	if err := transaction.stage(); err != nil {
		return integration.Result{}, err
	}
	if err := transaction.publish(ctx); err != nil {
		return integration.Result{}, err
	}
	if err := transaction.retireAndVerify(); err != nil {
		return integration.Result{}, err
	}
	rollback = false
	transaction.state.result.State = integration.StateInstalled
	transaction.state.result.Changed, transaction.state.result.RestartRequired = true, true
	return transaction.state.result, nil
}

func (service *Integration) Install(ctx context.Context, options integration.Options) (_ integration.Result, returnErr error) {
	if err := service.validateMutableLauncher(); err != nil {
		return integration.Result{}, err
	}
	configDirectory, err := integrationConfigDirectory(options)
	if err != nil {
		return integration.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	if _, err := requestedModelPlan(options, configDirectory); err != nil {
		return integration.Result{}, err
	}
	root, err := openRootTransaction(configDirectory, true)
	if err != nil {
		return integration.Result{}, fmt.Errorf("%w: open OpenCode config root: %v", integration.ErrConflict, err)
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	if service.afterInstallRoot != nil {
		service.afterInstallRoot(root)
	}
	if pending, err := service.ReinstallPending(ctx, options); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, integration.ErrInvalid) {
			return integration.Result{}, err
		}
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	} else if pending {
		return integration.Result{}, fmt.Errorf("%w: interrupted OpenCode reinstall evidence is present", integration.ErrRecovery)
	}
	state, err := service.inspectWithV1Migration(ctx, options, true)
	if err != nil {
		return integration.Result{}, err
	}
	if state.result.State == integration.StateInstalled {
		return state.result, nil
	}
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed OpenCode artifacts", integration.ErrConflict)
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	held, err := root.HeldAtPath()
	if err != nil || !held {
		return integration.Result{}, fmt.Errorf("%w: OpenCode config root changed after preflight", integration.ErrConflict)
	}
	created := make([]rootInstalledArtifact, 0, len(state.artifacts))
	retired := make([]rootRetiredArtifact, 0, len(state.retired))
	rollback := true
	defer func() {
		if rollback {
			returnErr = errors.Join(returnErr, rollbackRootInstall(root, retired, created))
		} else {
			for _, item := range retired {
				returnErr = errors.Join(returnErr, root.cleanupBackup(item.backup))
			}
			for _, item := range created {
				if item.retained && item.backup != nil {
					returnErr = errors.Join(returnErr, root.releaseBackup(*item.backup))
				}
				returnErr = errors.Join(returnErr, root.CleanupStaged(item.staged))
			}
		}
	}()
	for _, item := range state.artifacts {
		if item.exact {
			continue
		}
		name, relativeErr := root.Relative(item.path)
		if relativeErr != nil {
			return integration.Result{}, fmt.Errorf("%w: OpenCode artifact outside config root", integration.ErrInvalid)
		}
		installed, installErr := installRootArtifact(ctx, root, name, item)
		if installErr != nil {
			return integration.Result{}, installErr
		}
		created = append(created, installed)
	}
	for _, item := range state.retired {
		name, relativeErr := root.Relative(item.path)
		if relativeErr != nil {
			return integration.Result{}, fmt.Errorf("%w: retired OpenCode artifact outside config root", integration.ErrInvalid)
		}
		retiredItem, retireErr := retireRootArtifact(root, name, item)
		if retireErr != nil {
			return integration.Result{}, retireErr
		}
		retired = append(retired, retiredItem)
		if service.afterRetirement != nil {
			if err := service.afterRetirement(); err != nil {
				return integration.Result{}, err
			}
		}
	}
	if err := verifyRootInstall(root, state); err != nil {
		return integration.Result{}, fmt.Errorf("read back OpenCode integration artifacts: %w", integration.ErrDrift)
	}
	rollback = false
	state.result.State = integration.StateInstalled
	state.result.Changed = len(created) != 0 || len(retired) != 0
	state.result.RestartRequired = state.result.Changed
	return state.result, nil
}

func (service *Integration) Uninstall(ctx context.Context, options integration.Options) (_ integration.Result, returnErr error) {
	if err := service.validateMutableLauncher(); err != nil {
		return integration.Result{}, err
	}
	configDirectory, err := integrationConfigDirectory(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := openRootTransaction(configDirectory, false)
	if errors.Is(err, os.ErrNotExist) {
		state, inspectErr := service.inspect(ctx, options)
		if inspectErr != nil {
			return integration.Result{}, inspectErr
		}
		if state.result.State != integration.StateAbsent {
			return integration.Result{}, fmt.Errorf("%w: OpenCode config root appeared during preflight", integration.ErrConflict)
		}
		return state.result, nil
	}
	if err != nil {
		return integration.Result{}, fmt.Errorf("%w: open OpenCode config root: %v", integration.ErrConflict, err)
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	if service.afterUninstallRoot != nil {
		service.afterUninstallRoot(root)
	}
	if pending, err := service.reinstallPendingAtRoot(ctx, options, root); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, integration.ErrInvalid) {
			return integration.Result{}, err
		}
		return integration.Result{}, errors.Join(integration.ErrRecovery, err)
	} else if pending {
		return integration.Result{}, fmt.Errorf("%w: interrupted OpenCode reinstall evidence is present", integration.ErrRecovery)
	}
	state, err := service.inspect(ctx, options)
	if err != nil {
		return integration.Result{}, err
	}
	held, err := root.HeldAtPath()
	if err != nil || !held {
		return integration.Result{}, fmt.Errorf("%w: OpenCode config root changed after preflight", integration.ErrConflict)
	}
	if state.result.State == integration.StateAbsent {
		return state.result, nil
	}
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed OpenCode artifacts", integration.ErrDrift)
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	backupDirectory := ".vgxness-backups"
	if err := root.EnsureDirectory(backupDirectory); err != nil {
		return integration.Result{}, fmt.Errorf("prepare OpenCode integration backup: %w", err)
	}
	now := time.Now().UTC()
	if service != nil && service.now != nil {
		now = service.now().UTC()
	}
	stamp := fmt.Sprintf("%s.%09d", now.Format("20060102T150405"), now.Nanosecond())
	backupPaths := make(map[string]string, len(state.artifacts))
	for _, item := range state.artifacts {
		name, relativeErr := root.Relative(item.path)
		if relativeErr != nil {
			return integration.Result{}, fmt.Errorf("%w: OpenCode artifact outside config root", integration.ErrInvalid)
		}
		backupPaths[item.path] = filepath.Join(backupDirectory, item.backup+"."+stamp+filepath.Ext(name))
	}
	for _, item := range state.artifacts {
		if item.defaultAgent != nil {
			continue
		}
		if !item.exact && !item.upgrade {
			continue
		}
		if _, statErr := root.Lstat(backupPaths[item.path]); statErr == nil {
			return integration.Result{}, fmt.Errorf("%w: backup already exists", integration.ErrConflict)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return integration.Result{}, fmt.Errorf("inspect OpenCode integration backup: %w", statErr)
		}
	}
	backups := make([]rootBackedUpArtifact, 0, len(state.artifacts))
	retired := make([]rootRetiredArtifact, 0, len(state.retired))
	var defaultChange rootDefaultAgentUninstall
	rollback := true
	defer func() {
		if rollback {
			for index := len(retired) - 1; index >= 0; index-- {
				returnErr = errors.Join(returnErr, recoveryFailure("restore retired OpenCode artifact", root.RestoreObservedAnchor(retired[index].backup, retired[index].name)))
			}
			for index := len(backups) - 1; index >= 0; index-- {
				returnErr = errors.Join(returnErr, recoveryFailure("restore uninstalled OpenCode artifact", root.RestoreObservedAnchor(backups[index].backup, backups[index].name)))
			}
			returnErr = errors.Join(returnErr, recoveryFailure("restore default-agent configuration", defaultChange.rollback(root)))
		}
	}()
	for _, item := range state.retired {
		name, relativeErr := root.Relative(item.path)
		if relativeErr != nil {
			return integration.Result{}, fmt.Errorf("%w: retired OpenCode artifact outside config root", integration.ErrInvalid)
		}
		retiredItem, retireErr := retireRootArtifact(root, name, item)
		if retireErr != nil {
			return integration.Result{}, retireErr
		}
		retired = append(retired, retiredItem)
		if service.afterRetirement != nil {
			if err := service.afterRetirement(); err != nil {
				return integration.Result{}, err
			}
		}
	}
	removeManaged := func(item artifact) error {
		if !item.exact && !item.upgrade {
			return nil
		}
		expected := item.content
		if item.upgrade {
			expected = item.prior
		}
		name, relativeErr := root.Relative(item.path)
		if relativeErr != nil {
			return fmt.Errorf("%w: OpenCode artifact outside config root", integration.ErrInvalid)
		}
		backup, backupErr := root.BackupAs(name, backupPaths[item.path], expected)
		if backupErr != nil {
			return fmt.Errorf("backup OpenCode integration artifact: %w", backupErr)
		}
		backups = append(backups, rootBackedUpArtifact{name: name, backup: backup})
		if err := root.RemoveExact(name, backup.info, expected); err != nil {
			return fmt.Errorf("sync OpenCode integration removal: %w", err)
		}
		if _, statErr := root.Lstat(name); !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("%w: integration artifact changed during uninstall", integration.ErrConflict)
		}
		return ctx.Err()
	}
	var defaultState *artifact
	for _, item := range state.artifacts {
		if item.defaultState {
			copy := item
			defaultState = &copy
			continue
		}
		if item.defaultAgent != nil {
			name, relativeErr := root.Relative(item.path)
			if relativeErr != nil {
				return integration.Result{}, fmt.Errorf("%w: default-agent configuration outside config root", integration.ErrInvalid)
			}
			change, err := uninstallDefaultAgentAtRoot(ctx, root, name, item, service.executable)
			if err != nil {
				return integration.Result{}, err
			}
			defaultChange = change
			continue
		}
		if err := removeManaged(item); err != nil {
			return integration.Result{}, err
		}
	}
	if defaultState != nil {
		if err := removeManaged(*defaultState); err != nil {
			return integration.Result{}, err
		}
	}
	for _, item := range retired {
		if _, err := root.Lstat(item.name); !errors.Is(err, os.ErrNotExist) {
			return integration.Result{}, fmt.Errorf("read back OpenCode uninstall artifacts: %w", integration.ErrDrift)
		}
	}
	for _, item := range state.artifacts {
		if item.defaultAgent != nil || item.defaultState {
			continue
		}
		name, relativeErr := root.Relative(item.path)
		if relativeErr != nil {
			return integration.Result{}, fmt.Errorf("%w: OpenCode artifact outside config root", integration.ErrInvalid)
		}
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return integration.Result{}, fmt.Errorf("read back OpenCode uninstall artifacts: %w", integration.ErrDrift)
		}
	}
	rollback = false
	for _, item := range retired {
		returnErr = errors.Join(returnErr, recoveryFailure("clean up retired OpenCode artifact", root.cleanupBackup(item.backup)))
	}
	returnErr = errors.Join(returnErr, recoveryFailure("clean up default-agent predecessor", defaultChange.cleanup(root)))
	for _, item := range backups {
		returnErr = errors.Join(returnErr, recoveryFailure("release uninstalled OpenCode backup", root.releaseBackup(item.backup)))
	}
	state.result.State = integration.StateAbsent
	state.result.Changed = len(backups) != 0 || len(retired) != 0 || defaultChange.replacement != nil || defaultChange.removal != nil
	state.result.RestartRequired = state.result.Changed
	for _, item := range backups {
		if filepath.Join(configDirectory, item.name) == state.result.Path {
			state.result.BackupPath = filepath.Join(configDirectory, item.backup.name)
		}
		if filepath.Join(configDirectory, item.name) == state.result.ToolPath {
			state.result.ToolBackupPath = filepath.Join(configDirectory, item.backup.name)
		}
	}
	return state.result, nil
}

func uninstallDefaultAgentAtRoot(ctx context.Context, root *rootTransaction, name string, item artifact, executable string) (rootDefaultAgentUninstall, error) {
	if item.defaultAgent == nil {
		return rootDefaultAgentUninstall{}, integration.ErrInvalid
	}
	current, _, err := root.ReadRegularInfo(name)
	if errors.Is(err, os.ErrNotExist) && !item.defaultAgent.ConfigExisted {
		return rootDefaultAgentUninstall{}, nil
	}
	if err != nil {
		return rootDefaultAgentUninstall{}, fmt.Errorf("inspect OpenCode default-agent configuration: %w", err)
	}
	replacement, changed, remove, err := withoutDefaultAgent(current, *item.defaultAgent, executable)
	if err != nil || !changed {
		return rootDefaultAgentUninstall{}, err
	}
	if remove {
		anchor, err := root.Anchor(name, current)
		if err != nil {
			return rootDefaultAgentUninstall{}, err
		}
		if err := root.RemoveExact(name, anchor.info, current); err != nil {
			return rootDefaultAgentUninstall{}, errors.Join(err, recoveryFailure("restore default-agent configuration", root.RestoreObservedAnchor(anchor, name)))
		}
		return rootDefaultAgentUninstall{removal: &rootBackedUpArtifact{name: name, backup: anchor}}, nil
	}
	installed, err := replaceRootArtifact(ctx, root, name, replacement, current)
	if err != nil {
		return rootDefaultAgentUninstall{}, err
	}
	return rootDefaultAgentUninstall{replacement: &installed}, nil
}

func replaceRootArtifact(ctx context.Context, root *rootTransaction, name string, replacement, current []byte) (rootInstalledArtifact, error) {
	if err := ctx.Err(); err != nil {
		return rootInstalledArtifact{}, err
	}
	staged, err := root.StageArtifact(name, replacement, 0o600)
	if err != nil {
		return rootInstalledArtifact{}, fmt.Errorf("stage OpenCode default-agent configuration: %w", err)
	}
	installed := rootInstalledArtifact{name: name, staged: staged}
	anchor, err := root.Anchor(name, current)
	if err != nil {
		return rootInstalledArtifact{}, errors.Join(err, root.CleanupStaged(staged))
	}
	installed.backup = &anchor
	if err := root.RemoveExact(name, anchor.info, current); err != nil {
		return rootInstalledArtifact{}, errors.Join(err, recoveryFailure("restore default-agent configuration", root.RestoreObservedAnchor(anchor, name)), recoveryFailure("clean up default-agent staging", root.CleanupStaged(staged)))
	}
	publication, err := root.PublishStaged(staged, name)
	if err != nil {
		if publication.state == rootPublicationPending {
			if rollbackErr := root.RollbackPendingPublication(publication, name, staged.content); rollbackErr != nil {
				return rootInstalledArtifact{}, errors.Join(err, recoveryFailure("remove uncertain default-agent publication", rollbackErr), fmt.Errorf("%w: default-agent predecessor anchor retained at %q", integration.ErrRecovery, filepath.Join(root.path, anchor.name)), root.releaseBackup(anchor), recoveryFailure("clean up default-agent staging", root.CleanupStaged(staged)))
			}
		}
		return rootInstalledArtifact{}, errors.Join(err, recoveryFailure("restore default-agent configuration", root.RestoreObservedAnchor(anchor, name)), recoveryFailure("clean up default-agent staging", root.CleanupStaged(staged)))
	}
	installed.published = publication.info
	return installed, nil
}

func (change rootDefaultAgentUninstall) rollback(root *rootTransaction) error {
	if change.replacement != nil {
		return rollbackRootReinstalledArtifact(root, *change.replacement)
	}
	if change.removal != nil {
		return root.RestoreObservedAnchor(change.removal.backup, change.removal.name)
	}
	return nil
}

func (change rootDefaultAgentUninstall) cleanup(root *rootTransaction) error {
	if change.replacement != nil {
		var err error
		if change.replacement.backup != nil {
			err = errors.Join(err, root.cleanupBackup(*change.replacement.backup))
		}
		return errors.Join(err, root.CleanupStaged(change.replacement.staged))
	}
	if change.removal != nil {
		return root.cleanupBackup(change.removal.backup)
	}
	return nil
}

func (service *Integration) inspect(ctx context.Context, options integration.Options) (inspection, error) {
	return service.inspectWithV1Migration(ctx, options, false)
}

func (service *Integration) inspectWithV1Migration(ctx context.Context, options integration.Options, migrateInstalledV1 bool) (inspection, error) {
	if err := ctx.Err(); err != nil {
		return inspection{}, err
	}
	configDirectory, err := integrationConfigDirectory(options)
	if err != nil {
		return inspection{}, err
	}
	managerPath := filepath.Join(configDirectory, "agents", managerAgentName)
	legacyPluginPath := filepath.Join(configDirectory, "plugins", memoryPluginName)
	lifecyclePluginPath := filepath.Join(configDirectory, "plugins", memoryLifecyclePluginName)
	pluginContent, err := memoryLifecyclePluginContentForInspection(service.executable, service.prospectiveLauncher)
	if err != nil {
		return inspection{}, err
	}
	manifestPath := filepath.Join(configDirectory, "vgxness", modelPlanManifestName)
	skillPath := filepath.Join(configDirectory, "skills", autonomousStackedPRSkillName, "SKILL.md")
	defaultAgentPath := filepath.Join(configDirectory, defaultAgentConfigName)
	defaultAgentStatePath := filepath.Join(configDirectory, "vgxness", defaultAgentStateName)
	defaultAgentConfig, defaultAgentStateContent, defaultAgentState, defaultAgentSnapshot, defaultAgentSnapshotPresent, err := defaultAgentArtifacts(defaultAgentPath, defaultAgentStatePath, service.executable)
	if err != nil {
		return inspection{}, err
	}
	if service.afterDefaultAgentSnapshot != nil {
		service.afterDefaultAgentSnapshot()
	}
	plan, err := requestedModelPlanForMigration(options, configDirectory, migrateInstalledV1)
	if err != nil {
		return inspection{}, err
	}
	result := integration.Result{
		Provider: "opencode", State: integration.StateAbsent, Path: managerPath, ArtifactSHA256: artifactSHA256(plan.agents[managerAgentName]),
		ManifestPath: manifestPath, ManifestSHA256: artifactSHA256(plan.manifest),
		DefaultAgent: defaultAgentName, DefaultAgentPath: defaultAgentPath,
		DirectoryDurability: directoryDurability(),
	}
	if plan.configV3 != nil {
		result.ModelSchemaVersion = 3
		result.ModelProvider = plan.resolvedV3.Provider
		assignments, assignmentErr := resultModelAssignments(plan.resolvedV3.Assignments)
		if assignmentErr != nil {
			return inspection{}, assignmentErr
		}
		result.ModelAssignments = assignments
	} else if plan.configV2 != nil {
		result.ModelSchemaVersion = 2
		result.ModelPlan = plan.configV2.ActivePlan
		result.ModelProvider = plan.resolvedV2.Provider
		efficient := plan.configV2.Slots[modelplan.CapabilityEfficient]
		balanced := plan.configV2.Slots[modelplan.CapabilityBalanced]
		frontier := plan.configV2.Slots[modelplan.CapabilityFrontier]
		result.ModelEfficient, result.ModelEfficientEffort, result.ModelEfficientVariant, result.ModelEfficientSource, result.ModelEfficientAvailability = efficient.Reference, efficient.RequestedEffort, efficient.Variant, efficient.Source, efficient.Availability
		result.ModelBalanced, result.ModelBalancedEffort, result.ModelBalancedVariant, result.ModelBalancedSource, result.ModelBalancedAvailability = balanced.Reference, balanced.RequestedEffort, balanced.Variant, balanced.Source, balanced.Availability
		result.ModelFrontier, result.ModelFrontierEffort, result.ModelFrontierVariant, result.ModelFrontierSource, result.ModelFrontierAvailability = frontier.Reference, frontier.RequestedEffort, frontier.Variant, frontier.Source, frontier.Availability
		result.ModelVariantsSpecified = efficient.VariantSpecified || balanced.VariantSpecified || frontier.VariantSpecified
	} else {
		result.ModelSchemaVersion = 1
		result.ModelPlan, result.ModelProvider = plan.config.ActivePlan, plan.resolved.Provider
		result.ModelEfficient, result.ModelBalanced, result.ModelFrontier = plan.config.Efficient, plan.config.Balanced, plan.config.Frontier
	}
	if result.ModelAssignments == nil {
		assignments, assignmentErr := legacyResultModelAssignments(plan)
		if assignmentErr != nil {
			return inspection{}, assignmentErr
		}
		result.ModelAssignments = assignments
	}
	if defaultAgentState.Drifted {
		result.State = integration.StateDrifted
		return inspection{result: result}, nil
	}
	if foreign, err := foreignPersistentMCP(defaultAgentConfig, service.executable, defaultAgentState.MCPOwned); err != nil {
		return inspection{}, err
	} else if foreign {
		result.State = integration.StateDrifted
		return inspection{result: result}, nil
	}
	exists, drifted, containerErr := inspectDirectory(configDirectory)
	if containerErr != nil {
		return inspection{}, fmt.Errorf("inspect OpenCode integration directory: %w", containerErr)
	}
	_, installedPlanBytes, installedPlanOK := installedModelPlan(configDirectory)

	receipt, receiptBytes, receiptErr := readInstallationReceipt(configDirectory)
	if receiptErr != nil {
		return inspection{}, receiptErr
	}
	regeneration := func(path string) [][]byte {
		if receiptBytes != nil {
			name, err := filepath.Rel(configDirectory, path)
			if err != nil {
				return nil
			}
			data, err := readRegularFile(path)
			if err == nil && receipt.Matches(filepath.ToSlash(name), data) {
				return [][]byte{data}
			}
			return nil
		}
		if installedPlanOK && len(installedPlanBytes[path]) != 0 {
			return [][]byte{installedPlanBytes[path]}
		}
		return nil
	}
	state := inspection{result: result, artifacts: make([]artifact, 0, 12)}
	for _, identity := range ModelAgentInventoryV3() {
		name := strings.TrimPrefix(identity.ArtifactKey, "agents/")
		content := plan.agents[name]
		if len(content) == 0 {
			return inspection{}, integration.ErrInvalid
		}
		path := filepath.Join(configDirectory, filepath.FromSlash(identity.ArtifactKey))
		state.artifacts = append(state.artifacts, artifact{path: path, content: content, backup: strings.TrimSuffix(name, ".md"), regenerations: regeneration(path)})
	}
	state.artifacts = append(state.artifacts, artifact{path: lifecyclePluginPath, content: pluginContent, backup: "vgxness-memory-lifecycle-plugin", regenerations: regeneration(lifecyclePluginPath)})
	state.artifacts = append(state.artifacts, artifact{path: manifestPath, content: plan.manifest, backup: "vgxness-model-plan", regenerations: regeneration(manifestPath)}, artifact{path: defaultAgentStatePath, content: defaultAgentStateContent, backup: "vgxness-default-agent-state", defaultState: true, recognize: isLegacyDefaultAgentState}, artifact{path: defaultAgentPath, content: defaultAgentConfig, backup: "vgxness-default-agent", prior: defaultAgentSnapshot, defaultAgent: &defaultAgentState, defaultAgentSnapshotPresent: defaultAgentSnapshotPresent})

	receiptContent, receiptErr := currentReceipt(plan, pluginContent)
	if receiptErr != nil {
		return inspection{}, receiptErr
	}
	receiptPrior := [][]byte{}
	if receiptBytes != nil {
		receiptPrior = append(receiptPrior, receiptBytes)
	}
	state.artifacts = append(state.artifacts, artifact{path: receiptArtifactPath(configDirectory), content: receiptContent, backup: "vgxness-installation-receipt", regenerations: receiptPrior})
	for index := range state.artifacts {
		state.artifacts[index].retainedRoot = configDirectory
	}
	retained, retainedErr := retainedPredecessorInventory(configDirectory)
	if retainedErr != nil || retained.evidenceCount != 0 {
		state.result.RetainedPredecessorCount = retained.evidenceCount
		state.result.RetainedPredecessorPath = retainedPredecessorRoot(configDirectory)
	}
	if retainedErr != nil {
		state.result.State = integration.StateDrifted
		return state, nil
	}
	retirementCandidates := []retiredArtifact{
		retiredArtifact{path: skillPath, recognize: func([]byte) bool { return false }},
		retiredArtifact{path: legacyPluginPath, recognize: func([]byte) bool { return false }},
	}
	retired, retirementErr := inspectRetiredArtifacts(retirementCandidates...)
	if retirementErr != nil {
		state.result.State = integration.StateDrifted
		return state, nil
	}
	state.retired = retired
	state.result.ArtifactCount = len(state.artifacts)
	if drifted {
		state.result.State = integration.StateDrifted
		return state, nil
	}
	if !exists {
		return state, nil
	}
	exact, present := 0, 0
	for index := range state.artifacts {
		item := &state.artifacts[index]
		directoryExists, directoryDrifted, directoryErr := inspectDirectory(filepath.Dir(item.path))
		if directoryErr != nil {
			return inspection{}, fmt.Errorf("inspect OpenCode integration directory: %w", directoryErr)
		}
		if directoryDrifted {
			state.result.State = integration.StateDrifted
			return state, nil
		}
		if !directoryExists {
			continue
		}
		current, readErr := readRegularFile(item.path)
		if errors.Is(readErr, os.ErrNotExist) {
			if item.defaultAgent != nil && item.defaultAgentSnapshotPresent {
				state.result.State = integration.StateDrifted
				return state, nil
			}
			continue
		}
		if readErr != nil {
			if errors.Is(readErr, integration.ErrDrift) {
				state.result.State = integration.StateDrifted
				return state, nil
			}
			return inspection{}, fmt.Errorf("inspect OpenCode integration artifact: %w", readErr)
		}
		item.present = true
		if receiptBytes != nil {
			relative, relErr := filepath.Rel(configDirectory, item.path)
			if relErr != nil {
				return inspection{}, relErr
			}
			relative = filepath.ToSlash(relative)
			if _, managed := receipt.Files[relative]; managed && !receipt.Matches(relative, current) {
				state.result.State = integration.StateDrifted
				return state, nil
			}
		}
		present++
		if item.defaultAgent != nil && (!item.defaultAgentSnapshotPresent || !bytes.Equal(current, item.prior)) {
			state.result.State = integration.StateDrifted
			return state, nil
		}
		if item.defaultAgent != nil {
			item.exact = sameJSONValue(current, item.content)
		} else {
			item.exact = bytes.Equal(current, item.content)
		}
		if !item.exact {
			if installedPlanOK && len(installedPlanBytes[item.path]) != 0 && !bytes.Equal(current, installedPlanBytes[item.path]) && !isManagedPredecessor(current, item.content, item.predecessors, item.recognize) {
				state.result.State = integration.StateDrifted
				return state, nil
			}
			if item.defaultState && isLegacyDefaultAgentState(current) {
				item.upgrade = true
				item.prior = append([]byte(nil), current...)
				continue
			}
			if item.defaultAgent != nil {
				item.upgrade = true
				item.prior = append([]byte(nil), current...)
				continue
			}
			regenerated := false
			for _, prior := range item.regenerations {
				if bytes.Equal(current, prior) {
					regenerated = true
					break
				}
			}
			if !regenerated && !isManagedPredecessor(current, item.content, item.predecessors, item.recognize) {
				state.result.State = integration.StateDrifted
				return state, nil
			}
			item.upgrade = true
			item.prior = append([]byte(nil), current...)
			continue
		}
		exact++
	}
	switch present {
	case 0:
		state.result.State = integration.StateAbsent
	case len(state.artifacts):
		if exact == len(state.artifacts) {
			state.result.State = integration.StateInstalled
		} else {
			state.result.State = integration.StatePartial
		}
	default:
		state.result.State = integration.StatePartial
	}
	if len(state.retired) != 0 && state.result.State == integration.StateInstalled {
		state.result.State = integration.StatePartial
	}
	return state, nil
}

func resultModelAssignments(resolved []modelplan.OpenCodeAgentAssignmentV3) (*[integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3, error) {
	current := make([]modelplan.OpenCodeAgentAssignmentV3, 0, 7)
	for _, row := range resolved {
		if !strings.HasPrefix(row.ArtifactKey, "agents/vgxness-sdd-") {
			current = append(current, row)
		}
	}
	resolved = current
	if len(resolved) != integration.ModelAssignmentCount {
		return nil, fmt.Errorf("%w: resolved OpenCode v3 assignment count", integration.ErrInvalid)
	}
	assignments := new([integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3)
	copy(assignments[:], resolved)
	return assignments, nil
}

func legacyResultModelAssignments(plan modelPlanBundle) (*[integration.ModelAssignmentCount]modelplan.OpenCodeAgentAssignmentV3, error) {
	rows := make([]modelplan.OpenCodeAgentAssignmentV3, 0, len(modelAgentInventoryV3))
	for _, identity := range modelAgentInventoryV3 {
		row := modelplan.OpenCodeAgentAssignmentV3{ArtifactKey: identity.ArtifactKey, Role: identity.Role, Class: identity.Class}
		if plan.resolvedV2 != nil && plan.configV2 != nil {
			resolved, err := modelplan.ResolveOpenCodePlanV2(*plan.configV2)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve OpenCode v2 model plan", integration.ErrInvalid)
			}
			assignment, ok := resolved.Roles[identity.Role]
			if !ok {
				return nil, fmt.Errorf("%w: missing OpenCode v2 role assignment", integration.ErrInvalid)
			}
			slot, ok := plan.configV2.Slots[assignment.Capability]
			if !ok {
				return nil, fmt.Errorf("%w: missing OpenCode v2 slot", integration.ErrInvalid)
			}
			row.Provider, row.Model = assignment.Provider, assignment.Model
			row.RequestedEffort, row.Effort, row.Variant, row.Degradation = assignment.RequestedEffort, assignment.Effort, assignment.Variant, assignment.Degradation
			row.VariantSpecified = slot.VariantSpecified
			row.Source, row.Availability = slot.Source, slot.Availability
		} else {
			resolved, err := modelplan.ResolveOpenCodePlan(plan.config)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve OpenCode v1 model plan", integration.ErrInvalid)
			}
			assignment, ok := resolved.Roles[identity.Role]
			if !ok {
				return nil, fmt.Errorf("%w: missing OpenCode v1 role assignment", integration.ErrInvalid)
			}
			row.Provider, row.Model = plan.resolved.Provider, assignment.Model
			row.RequestedEffort, row.Effort, row.Variant, row.Degradation = assignment.RequestedEffort, assignment.Effort, assignment.Variant, assignment.Degradation
			row.Source, row.Availability = modelplan.ModelSlotCustom, modelplan.ModelSlotUnknown
		}
		rows = append(rows, row)
	}
	return resultModelAssignments(rows)
}

func inspectRetiredArtifacts(candidates ...retiredArtifact) ([]retiredArtifact, error) {
	retired := make([]retiredArtifact, 0, len(candidates))
	for _, candidate := range candidates {
		content, err := readRegularFile(candidate.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if candidate.recognize == nil || !candidate.recognize(content) {
			return nil, integration.ErrDrift
		}
		candidate.content = content
		retired = append(retired, candidate)
	}
	return retired, nil
}

func defaultAgentArtifacts(configPath, statePath, executable string) ([]byte, []byte, defaultAgentState, []byte, bool, error) {
	config, exists, snapshot, err := readOpenCodeConfig(configPath)
	if err != nil {
		return nil, nil, defaultAgentState{}, nil, false, err
	}
	stateData, err := readRegularFile(statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, defaultAgentState{}, nil, false, fmt.Errorf("inspect OpenCode default-agent state: %w", err)
	}
	state := defaultAgentState{}
	if err == nil {
		if err := json.Unmarshal(stateData, &state); err != nil || !validDefaultAgentState(state) {
			return nil, nil, defaultAgentState{}, nil, false, fmt.Errorf("%w: OpenCode default-agent state", integration.ErrDrift)
		}
	} else {
		state.ConfigExisted = exists
		state.DefaultAgent, state.DefaultAgentExisted = config["default_agent"]
		state.SchemaVersion = 1
		if err := captureManagedConfigSnapshot(config, &state); err != nil {
			return nil, nil, defaultAgentState{}, nil, false, err
		}
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
		if err := captureManagedConfigSnapshot(config, &state); err != nil {
			return nil, nil, defaultAgentState{}, nil, false, err
		}
	}
	content, err := withManagedOpenCodeConfig(config, exists, &state, executable)
	if err != nil {
		if !errors.Is(err, integration.ErrDrift) {
			return nil, nil, defaultAgentState{}, nil, false, err
		}
		state.Drifted = true
		content = snapshot
	}
	stateData, err = json.Marshal(state)
	if err != nil {
		return nil, nil, defaultAgentState{}, nil, false, fmt.Errorf("encode OpenCode default-agent state: %w", err)
	}
	return content, append(stateData, '\n'), state, snapshot, exists, nil
}

func readOpenCodeConfig(path string) (map[string]json.RawMessage, bool, []byte, error) {
	data, err := readRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]json.RawMessage), false, nil, nil
	}
	if err != nil {
		return nil, false, nil, fmt.Errorf("inspect OpenCode configuration: %w", err)
	}
	values := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return nil, false, nil, fmt.Errorf("%w: opencode.json must contain a JSON object", integration.ErrInvalid)
	}
	return values, true, data, nil
}

func validDefaultAgentState(state defaultAgentState) bool {
	if state.SchemaVersion != 0 && state.SchemaVersion != 1 {
		return false
	}
	if state.SchemaVersion == 0 && (state.MCPExisted || len(state.MCP) != 0 || state.MCPOwned || state.PermissionExisted || len(state.Permission) != 0 || state.PermissionOwned) {
		return false
	}
	if !state.DefaultAgentExisted {
		if len(state.DefaultAgent) != 0 {
			return false
		}
	}
	if state.DefaultAgentExisted && (len(state.DefaultAgent) == 0 || len(state.DefaultAgent) > maxDefaultAgentBytes || !json.Valid(state.DefaultAgent)) {
		return false
	}
	if state.MCPExisted != (len(state.MCP) != 0) || state.MCPOwned && state.MCPExisted || state.MCPExisted && !json.Valid(state.MCP) {
		return false
	}
	if state.PermissionExisted != (len(state.Permission) != 0) || state.PermissionOwned && state.PermissionExisted || state.PermissionExisted && !json.Valid(state.Permission) {
		return false
	}
	return true
}

func isLegacyDefaultAgentState(data []byte) bool {
	var state defaultAgentState
	return json.Unmarshal(data, &state) == nil && state.SchemaVersion == 0 && validDefaultAgentState(state)
}

func managedMCPConfig(executable string) (json.RawMessage, error) {
	if strings.TrimSpace(executable) == "" {
		return nil, fmt.Errorf("%w: managed executable", integration.ErrInvalid)
	}
	entry := struct {
		Type    string   `json:"type"`
		Command []string `json:"command"`
		Enabled bool     `json:"enabled"`
	}{Type: "local", Command: []string{executable, "mcp", "--full"}, Enabled: true}
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("encode OpenCode MCP configuration: %w", err)
	}
	return data, nil
}

func openCodeMCP(values map[string]json.RawMessage) (json.RawMessage, bool, error) {
	raw, ok := values["mcp"]
	if !ok {
		return nil, false, nil
	}
	servers := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
		return nil, false, fmt.Errorf("%w: opencode.json mcp must contain an object", integration.ErrInvalid)
	}
	entry, exists := servers["vgxness"]
	return entry, exists, nil
}

func openCodePermission(values map[string]json.RawMessage) (json.RawMessage, bool, error) {
	raw, ok := values["permission"]
	if !ok {
		return nil, false, nil
	}
	rules := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &rules); err != nil || rules == nil {
		return nil, false, fmt.Errorf("%w: opencode.json permission must contain an object", integration.ErrConflict)
	}
	rule, exists := rules["vgxness_*"]
	return rule, exists, nil
}

func captureManagedConfigSnapshot(values map[string]json.RawMessage, state *defaultAgentState) error {
	if state == nil {
		return integration.ErrInvalid
	}
	if mcp, present, err := openCodeMCP(values); err != nil {
		return err
	} else if present {
		state.MCP, state.MCPExisted = append([]byte(nil), mcp...), true
	}
	if rule, present, err := openCodePermission(values); err != nil {
		return err
	} else if present {
		state.Permission, state.PermissionExisted = append([]byte(nil), rule...), true
	}
	return nil
}

func sameJSONValue(left, right []byte) bool {
	var leftValue, rightValue any
	leftErr := json.Unmarshal(left, &leftValue)
	rightErr := json.Unmarshal(right, &rightValue)
	if leftErr != nil || rightErr != nil {
		return false
	}
	leftBytes, leftErr := json.Marshal(leftValue)
	rightBytes, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func foreignPersistentMCP(config []byte, executable string, owned bool) (bool, error) {
	values, _, err := readOpenCodeConfigFromBytes(config)
	if err != nil {
		return false, err
	}
	entry, exists, err := openCodeMCP(values)
	if err != nil || !exists {
		return false, err
	}
	managed, err := managedMCPConfig(executable)
	if err != nil || sameJSONValue(entry, managed) {
		return false, err
	}
	return !owned || !sameJSONValue(entry, managedReadOnlyMCPConfig(executable)), nil
}

func withManagedOpenCodeConfig(values map[string]json.RawMessage, exists bool, state *defaultAgentState, executable string) ([]byte, error) {
	if state == nil {
		return nil, integration.ErrInvalid
	}
	if !exists {
		if !state.ConfigExisted {
			state.MCPOwned, state.PermissionOwned = false, false
		}
		schema, _ := json.Marshal("https://opencode.ai/config.json")
		values["$schema"] = schema
	}
	defaultAgent, _ := json.Marshal(defaultAgentName)
	values["default_agent"] = defaultAgent
	managed, err := managedMCPConfig(executable)
	if err != nil {
		return nil, err
	}
	if err := applyManagedMCP(values, state, managed, executable); err != nil {
		return nil, err
	}
	if err := applyManagedPermission(values, state); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode OpenCode configuration: %w", err)
	}
	return append(encoded, '\n'), nil
}

func applyManagedMCP(values map[string]json.RawMessage, state *defaultAgentState, managed json.RawMessage, executable string) error {
	entry, present, err := openCodeMCP(values)
	if err != nil {
		return err
	}
	if state.MCPOwned {
		if !present || (!sameJSONValue(entry, managed) && !sameJSONValue(entry, managedReadOnlyMCPConfig(executable))) {
			return integration.ErrDrift
		}
		if !sameJSONValue(entry, managed) {
			servers := map[string]json.RawMessage{}
			if err := json.Unmarshal(values["mcp"], &servers); err != nil {
				return integration.ErrDrift
			}
			servers["vgxness"] = managed
			raw, err := json.Marshal(servers)
			if err != nil {
				return err
			}
			values["mcp"] = raw
		}
		return nil
	}
	if state.MCPExisted {
		if !present || !sameJSONValue(entry, state.MCP) {
			return integration.ErrDrift
		}
		return nil
	}
	if present {
		return integration.ErrConflict
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := values["mcp"]; ok && json.Unmarshal(raw, &servers) != nil {
		return integration.ErrInvalid
	}
	servers["vgxness"] = managed
	raw, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	values["mcp"] = raw
	state.MCPOwned = true
	return nil
}

func managedReadOnlyMCPConfig(executable string) json.RawMessage {
	data, _ := json.Marshal(struct {
		Type    string   `json:"type"`
		Command []string `json:"command"`
		Enabled bool     `json:"enabled"`
	}{"local", []string{executable, "mcp"}, true})
	return data
}

func applyManagedPermission(values map[string]json.RawMessage, state *defaultAgentState) error {
	rule, present, err := openCodePermission(values)
	if err != nil {
		return err
	}
	managed := json.RawMessage(`"deny"`)
	if state.PermissionOwned {
		if !present || !sameJSONValue(rule, managed) {
			return integration.ErrDrift
		}
		return nil
	}
	if state.PermissionExisted {
		if !present || !sameJSONValue(rule, state.Permission) {
			return integration.ErrDrift
		}
		if !sameJSONValue(rule, managed) {
			return integration.ErrConflict
		}
		return nil
	}
	if present {
		return integration.ErrConflict
	}
	rules := map[string]json.RawMessage{}
	if raw, ok := values["permission"]; ok && json.Unmarshal(raw, &rules) != nil {
		return integration.ErrConflict
	}
	rules["vgxness_*"] = managed
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	values["permission"] = raw
	state.PermissionOwned = true
	return nil
}

func defaultAgentIsManaged(config []byte) bool {
	values := make(map[string]json.RawMessage)
	if json.Unmarshal(config, &values) != nil || values == nil {
		return false
	}
	return bytes.Equal(values["default_agent"], []byte(`"vgxness-manager"`))
}

func withoutDefaultAgent(config []byte, state defaultAgentState, executable string) ([]byte, bool, bool, error) {
	values, _, err := readOpenCodeConfigFromBytes(config)
	if err != nil {
		return nil, false, false, err
	}
	if err := withoutManagedMCP(values, state, executable); err != nil {
		return nil, false, false, err
	}
	if err := withoutManagedPermission(values, state); err != nil {
		return nil, false, false, err
	}
	if !defaultAgentIsManaged(config) {
		encoded, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			return nil, false, false, fmt.Errorf("encode OpenCode configuration: %w", err)
		}
		return append(encoded, '\n'), true, false, nil
	}
	if state.DefaultAgentExisted {
		values["default_agent"] = state.DefaultAgent
	} else {
		delete(values, "default_agent")
		if !state.ConfigExisted && len(values) == 1 && bytes.Equal(values["$schema"], []byte(`"https://opencode.ai/config.json"`)) {
			return nil, true, true, nil
		}
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, false, false, fmt.Errorf("encode OpenCode configuration: %w", err)
	}
	return append(encoded, '\n'), true, false, nil
}

func withoutManagedMCP(values map[string]json.RawMessage, state defaultAgentState, executable string) error {
	entry, present, err := openCodeMCP(values)
	if err != nil {
		return err
	}
	if state.MCPOwned {
		managed, err := managedMCPConfig(executable)
		if err != nil {
			return err
		}
		if !present || (!sameJSONValue(entry, managed) && !sameJSONValue(entry, managedReadOnlyMCPConfig(executable))) {
			return integration.ErrDrift
		}
		servers := map[string]json.RawMessage{}
		if err := json.Unmarshal(values["mcp"], &servers); err != nil {
			return integration.ErrDrift
		}
		delete(servers, "vgxness")
		if len(servers) == 0 {
			delete(values, "mcp")
			return nil
		}
		raw, err := json.Marshal(servers)
		if err != nil {
			return err
		}
		values["mcp"] = raw
		return nil
	}
	if state.MCPExisted && (!present || !sameJSONValue(entry, state.MCP)) {
		return integration.ErrDrift
	}
	return nil
}

func withoutManagedPermission(values map[string]json.RawMessage, state defaultAgentState) error {
	rule, present, err := openCodePermission(values)
	if err != nil {
		return err
	}
	managed := json.RawMessage(`"deny"`)
	if state.PermissionOwned {
		if !present || !sameJSONValue(rule, managed) {
			return integration.ErrDrift
		}
		rules := map[string]json.RawMessage{}
		if err := json.Unmarshal(values["permission"], &rules); err != nil {
			return integration.ErrDrift
		}
		delete(rules, "vgxness_*")
		if len(rules) == 0 {
			delete(values, "permission")
			return nil
		}
		raw, err := json.Marshal(rules)
		if err != nil {
			return err
		}
		values["permission"] = raw
		return nil
	}
	if state.PermissionExisted && (!present || !sameJSONValue(rule, state.Permission)) {
		return integration.ErrDrift
	}
	return nil
}

func readOpenCodeConfigFromBytes(data []byte) (map[string]json.RawMessage, bool, error) {
	values := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return nil, false, fmt.Errorf("%w: opencode.json must contain a JSON object", integration.ErrInvalid)
	}
	return values, true, nil
}

func installRootArtifact(ctx context.Context, root *rootTransaction, name string, item artifact) (rootInstalledArtifact, error) {
	if err := ctx.Err(); err != nil {
		return rootInstalledArtifact{}, err
	}
	staged, err := root.StageArtifact(name, item.content, 0o600)
	if err != nil {
		return rootInstalledArtifact{}, fmt.Errorf("stage OpenCode integration artifact: %w", err)
	}
	installed := rootInstalledArtifact{name: name, staged: staged}
	if item.upgrade {
		anchorDirectory := filepath.Join("vgxness", retainedPredecessorDirectory, retainedAnchorDirectory)
		if err := root.EnsureDirectory(anchorDirectory); err != nil {
			return rootInstalledArtifact{}, errors.Join(fmt.Errorf("%w: prepare retained predecessor directory", integration.ErrConflict), root.CleanupStaged(staged))
		}
		backup, backupErr := root.BackupIn(name, anchorDirectory, item.prior)
		if backupErr != nil {
			return rootInstalledArtifact{}, errors.Join(fmt.Errorf("%w: protect OpenCode integration predecessor", integration.ErrConflict), root.CleanupStaged(staged))
		}
		installed.backup = &backup
		if err := persistRootRetainedPredecessor(root, name, backup, item.prior); err != nil {
			return rootInstalledArtifact{}, errors.Join(retainedPredecessorPersistError(filepath.Join(root.path, "vgxness", retainedPredecessorDirectory), filepath.Join(root.path, backup.name), err), root.releaseBackup(backup), root.CleanupStaged(staged))
		}
		installed.retained = true
		if err := root.RemoveExact(name, backup.info, item.prior); err != nil {
			return rootInstalledArtifact{}, errors.Join(fmt.Errorf("remove OpenCode integration predecessor: %w", err), root.releaseBackup(backup), root.CleanupStaged(staged))
		}
	}
	publication, err := root.PublishStaged(staged, name)
	if err != nil {
		var recoveryErr error
		if publication.state == rootPublicationPending {
			if rollbackErr := root.RollbackPendingPublication(publication, name, staged.content); rollbackErr != nil {
				recoveryErr = errors.Join(recoveryErr, recoveryFailure("remove uncertain OpenCode integration publication", rollbackErr))
				if installed.backup != nil {
					recoveryErr = errors.Join(recoveryErr, root.releaseBackup(*installed.backup))
				}
				return rootInstalledArtifact{}, errors.Join(fmt.Errorf("publish OpenCode integration artifact: %w", err), recoveryErr, root.CleanupStaged(staged))
			}
		}
		if installed.backup != nil {
			if installed.retained {
				recoveryErr = recoveryFailure("restore retained OpenCode integration predecessor", root.RestoreRetainedBackup(*installed.backup, name))
			} else {
				recoveryErr = recoveryFailure("restore OpenCode integration predecessor", root.RestoreBackup(*installed.backup, name))
			}
		}
		return rootInstalledArtifact{}, errors.Join(fmt.Errorf("publish OpenCode integration artifact: %w", err), recoveryErr, root.CleanupStaged(staged))
	}
	installed.published = publication.info
	return installed, nil
}

func rollbackRootInstall(root *rootTransaction, retired []rootRetiredArtifact, created []rootInstalledArtifact) error {
	var recoveryErr error
	for index := len(retired) - 1; index >= 0; index-- {
		recoveryErr = errors.Join(recoveryErr, recoveryFailure("restore retired OpenCode artifact", root.RestoreBackup(retired[index].backup, retired[index].name)))
	}
	for index := len(created) - 1; index >= 0; index-- {
		recoveryErr = errors.Join(recoveryErr, recoveryFailure("restore OpenCode integration artifact", rollbackRootInstalledArtifact(root, created[index])))
	}
	return recoveryErr
}

func rollbackRootInstalledArtifact(root *rootTransaction, item rootInstalledArtifact) error {
	var err error
	if item.publication.state == rootPublicationPending {
		if rollbackErr := root.RollbackPendingPublication(item.publication, item.name, item.staged.content); rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
			if item.backup != nil {
				err = errors.Join(err, root.releaseBackup(*item.backup))
			}
		} else if item.backup != nil {
			if item.retained {
				err = errors.Join(err, root.RestoreRetainedBackup(*item.backup, item.name))
			} else {
				err = errors.Join(err, root.RestoreBackup(*item.backup, item.name))
			}
		}
	} else if item.published != nil && item.retained && item.backup != nil {
		data, current, readErr := root.ReadRegularInfo(item.name)
		if readErr != nil || !os.SameFile(current, item.published) || !bytes.Equal(data, item.staged.content) {
			err = errors.Join(err, fmt.Errorf("published artifact changed before rollback"), root.releaseBackup(*item.backup))
		} else if removeErr := root.RemoveExact(item.name, current, item.staged.content); removeErr != nil {
			err = errors.Join(err, removeErr, root.releaseBackup(*item.backup))
		} else {
			err = errors.Join(err, root.RestoreRetainedBackup(*item.backup, item.name))
		}
	} else if item.published != nil {
		err = errors.Join(err, root.RollbackPublished(item.staged, item.name, item.published, item.backup))
	} else if item.backup != nil && !item.retained {
		err = errors.Join(err, root.RestoreBackup(*item.backup, item.name))
	}
	return errors.Join(err, root.CleanupStaged(item.staged))
}

func rollbackRootReinstalledArtifact(root *rootTransaction, item rootInstalledArtifact) error {
	var err error
	if item.publication.state == rootPublicationPending {
		if rollbackErr := root.RollbackPendingPublication(item.publication, item.name, item.staged.content); rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
			if item.backup != nil {
				err = errors.Join(err, root.releaseBackup(*item.backup))
			}
			return errors.Join(err, root.CleanupStaged(item.staged))
		}
	} else if item.published != nil {
		err = errors.Join(err, root.RemoveExact(item.name, item.published, item.staged.content))
	}
	if item.backup != nil {
		err = errors.Join(err, root.RestoreObservedAnchor(*item.backup, item.name))
	}
	return errors.Join(err, root.CleanupStaged(item.staged))
}

func persistRootRetainedPredecessor(root *rootTransaction, target string, backup rootArtifactBackup, predecessor []byte) error {
	directory := filepath.Join("vgxness", retainedPredecessorDirectory)
	if err := root.EnsureDirectory(directory); err != nil {
		return err
	}
	operation := make([]byte, 16)
	if _, err := rand.Read(operation); err != nil {
		return err
	}
	markerRoot := root.path
	if runtime.GOOS == "darwin" && strings.HasPrefix(markerRoot, "/private/var/") {
		markerRoot = strings.TrimPrefix(markerRoot, "/private")
	}
	marker := retainedPredecessorMarker{Version: retainedPredecessorVersion, Operation: hex.EncodeToString(operation), Root: markerRoot, Target: filepath.Join(markerRoot, target), Anchor: filepath.Join(markerRoot, backup.name), SHA256: artifactSHA256(predecessor)}
	body, err := json.Marshal(marker)
	if err != nil || len(body)+1 > maxRetainedPredecessorBytes {
		return fmt.Errorf("encode retained predecessor")
	}
	file, err := root.CreateTempIn(directory, ".vgxness-retained-*.tmp")
	if err != nil {
		return err
	}
	if err := writeAndSyncRootFile(file, append(body, '\n')); err != nil {
		return err
	}
	if err := root.Publish(file.Name(), filepath.Join(directory, marker.Operation+".json")); err != nil {
		return err
	}
	return root.SyncDirectory(directory)
}

func retireRootArtifact(root *rootTransaction, name string, item retiredArtifact) (rootRetiredArtifact, error) {
	backup, err := root.Backup(name, item.content)
	if err != nil {
		return rootRetiredArtifact{}, fmt.Errorf("%w: protect retired OpenCode artifact", integration.ErrConflict)
	}
	if err := root.RemoveExact(name, backup.info, item.content); err != nil {
		return rootRetiredArtifact{}, errors.Join(fmt.Errorf("retire OpenCode artifact: %w", err), recoveryFailure("restore retired OpenCode artifact", root.RestoreBackup(backup, name)))
	}
	return rootRetiredArtifact{name: name, backup: backup}, nil
}

func verifyRootInstall(root *rootTransaction, state inspection) error {
	for _, item := range state.artifacts {
		name, err := root.Relative(item.path)
		if err != nil {
			return err
		}
		data, err := root.ReadRegular(name)
		if err != nil || (item.defaultAgent == nil && !bytes.Equal(data, item.content)) || (item.defaultAgent != nil && !sameJSONValue(data, item.content)) {
			return fmt.Errorf("artifact readback failed")
		}
	}
	for _, item := range state.retired {
		name, err := root.Relative(item.path)
		if err != nil {
			return err
		}
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("retired artifact remains")
		}
	}
	return nil
}

func retainedPredecessorPersistError(markerPath, backup string, err error) error {
	if markerPath != "" {
		return fmt.Errorf("%w: persist OpenCode integration predecessor; marker retained at %q and backup retained at %q: %v", integration.ErrConflict, markerPath, backup, err)
	}
	return fmt.Errorf("%w: persist OpenCode integration predecessor: %v", integration.ErrConflict, err)
}

// memoryLifecyclePluginContent validates bytes for a future installer without
// registering or activating the lifecycle adapter.
func memoryLifecyclePluginContent(executable string) ([]byte, error) {
	if strings.TrimSpace(executable) == "" || !filepath.IsAbs(executable) {
		return nil, fmt.Errorf("%w: VGXNESS executable path", integration.ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(executable))
	if err != nil {
		return nil, fmt.Errorf("%w: VGXNESS executable unavailable", integration.ErrInvalid)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: VGXNESS executable is not a regular file", integration.ErrInvalid)
	}
	return renderMemoryLifecyclePlugin(resolved), nil
}

func memoryLifecyclePluginContentForInspection(executable string, prospective bool) ([]byte, error) {
	if !prospective {
		return memoryLifecyclePluginContent(executable)
	}
	if strings.TrimSpace(executable) != executable || !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return nil, fmt.Errorf("%w: preview VGXNESS launcher", integration.ErrInvalid)
	}
	return renderMemoryLifecyclePlugin(executable), nil
}

func renderMemoryLifecyclePlugin(resolved string) []byte {
	quoted, _ := json.Marshal(resolved)
	return []byte(`import { spawn } from "node:child_process"
import { isAbsolute } from "node:path"

// managed-by: vgxness; artifact: opencode-plugin/vgxness-memory-lifecycle; version: 1
const VGXNESS_EXECUTABLE = ` + string(quoted) + `
const MAX_INPUT_BYTES = 64 * 1024
const MAX_OUTPUT_BYTES = 8 * 1024
const MAX_CONTEXT_BYTES = 4 * 1024
const MAX_SESSIONS = 128
const TIMEOUT_MS = 5_000
const CLEANUP_MS = 1_000
const identifier = value => /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,239}$/.test(String(value ?? "")) ? String(value) : ""
const bounded = (value, limit) => {
  const text = value, suffix = "\n[truncated by VGXNESS]"
  if (Buffer.byteLength(text) <= limit) return text
  let result = "", room = limit - Buffer.byteLength(suffix)
  for (const character of text) { if (Buffer.byteLength(result) + Buffer.byteLength(character) > room) break; result += character }
  return result + suffix
}
const untrusted = value => bounded(value, MAX_CONTEXT_BYTES).replace(/<\s*\/\s*(UNTRUSTED\s+DATA|VGXNESS\s+LIFECYCLE)\s*>/gi, "<\\/$1>")

export const VGXNESSMemoryLifecyclePlugin = async ({ directory }) => {
  const sessions = new Map(); let disposed = false, nextGeneration = 0
  const invoke = (operation, payload) => new Promise((resolve, reject) => {
    if ((disposed && operation !== "end") || !isAbsolute(directory)) return reject(new Error("VGXNESS lifecycle unavailable"))
    const input = JSON.stringify({ schemaVersion: 1, operation, workspace: directory, ...payload })
    if (Buffer.byteLength(input) > MAX_INPUT_BYTES) return reject(new Error("VGXNESS lifecycle request exceeded its bound"))
    const child = spawn(VGXNESS_EXECUTABLE, ["memory", "hook", "--stdin"], { cwd: directory, shell: false, stdio: ["pipe", "pipe", "pipe"], env: { HOME: process.env.HOME, USERPROFILE: process.env.USERPROFILE, TMPDIR: process.env.TMPDIR, SystemRoot: process.env.SystemRoot } })
    let stdout = "", stdoutBytes = 0, stderrBytes = 0, settled = false, failure, cleanupTimer
    const finish = (error, value) => { if (settled) return; settled = true; clearTimeout(timer); if (cleanupTimer) clearTimeout(cleanupTimer); error ? reject(error) : resolve(value) }
    const stop = () => { try { return child.kill("SIGKILL") !== false } catch { return false } }
    const release = () => { for (const stream of [child.stdin, child.stdout, child.stderr]) try { stream?.destroy?.() } catch {}; try { child.unref?.() } catch {} }
    const fail = error => { if (settled || failure) return; failure = error; if (!stop()) try { child.unref?.() } catch {}; release(); cleanupTimer = setTimeout(() => { release(); finish(failure) }, CLEANUP_MS) }
    const timer = setTimeout(() => fail(new Error("VGXNESS lifecycle timed out")), TIMEOUT_MS)
    child.stdout.setEncoding("utf8"); child.stderr.setEncoding("utf8")
    child.stdout.on("data", chunk => { if (settled || failure) return; const chunkBytes = Buffer.byteLength(chunk); if (stdoutBytes + chunkBytes > MAX_OUTPUT_BYTES) return fail(new Error("VGXNESS lifecycle response exceeded its bound")); stdoutBytes += chunkBytes; stdout += chunk })
    child.stderr.on("data", chunk => { stderrBytes += Buffer.byteLength(chunk); if (stderrBytes > MAX_OUTPUT_BYTES) fail(new Error("VGXNESS lifecycle failure exceeded its bound")) })
    child.on("error", () => fail(new Error("VGXNESS lifecycle unavailable")))
    child.on("close", code => { if (settled) return; if (failure) return finish(failure); if (code !== 0) return finish(new Error("VGXNESS lifecycle failed")); try { const result = JSON.parse(stdout); if (result?.schemaVersion !== 1) throw new Error(); finish(undefined, result) } catch { finish(new Error("VGXNESS lifecycle response is invalid")) } })
    child.stdin.on("error", () => fail(new Error("VGXNESS lifecycle input failed")))
    try { child.stdin.end(input) } catch { fail(new Error("VGXNESS lifecycle input failed")) }
  })
  const live = state => !!state?.handle && !!state?.leaseToken
  const activeReceipt = (state, receipt) => receipt?.state === "active" && receipt?.session_handle === state.handle && receipt?.lease_token === state.leaseToken
  const committedCompletion = (state, receipt) => receipt?.state === "completed" && receipt?.session_handle === state.handle && typeof receipt?.final_observation_id === "string" && receipt.final_observation_id.trim() !== ""
  const terminalStartReceipt = receipt => {
    const handle = identifier(receipt?.session_handle)
    if (!handle) return undefined
    if (receipt?.state === "completed") return typeof receipt?.final_observation_id === "string" && receipt.final_observation_id.trim() !== "" ? { terminal: true, handle } : undefined
    return receipt?.state === "interrupted" || receipt?.state === "cancelled" ? { terminal: true, handle } : undefined
  }
  const end = async state => {
    if (!live(state)) return
    const terminalState = state.summaryCompleted ? "completed" : "interrupted"
    const receipt = await invoke("end", { session_handle: state.handle, lease_token: state.leaseToken, external_id: state.externalID, state: terminalState })
    if (terminalState === "completed" && !committedCompletion(state, receipt)) throw new Error("VGXNESS lifecycle completion was not committed")
  }
  const acquire = (externalID, strict = false) => {
    const existing = sessions.get(externalID)
    if (live(existing)) return Promise.resolve(existing)
    if (existing?.pending) return existing.pending
    const placeholder = { externalID, generation: ++nextGeneration }
    sessions.set(externalID, placeholder)
    placeholder.pending = (async () => {
      try {
        const result = await invoke("start", { provider: "opencode", external_id: externalID }), handle = identifier(result?.session_handle), leaseToken = identifier(result?.lease_token)
        const terminal = terminalStartReceipt(result)
        if (terminal) { if (sessions.get(externalID) === placeholder) sessions.delete(externalID); return terminal }
        if (!handle || !leaseToken) throw new Error("VGXNESS lifecycle acquisition failed")
        const state = { externalID, generation: placeholder.generation, handle, leaseToken, summaryCompleted: result?.draft_present === true, contextLoaded: false }
        if (sessions.get(externalID) !== placeholder) { await end(state); return undefined }
        sessions.set(externalID, state)
        return state
      } catch (error) {
        if (sessions.get(externalID) === placeholder) sessions.delete(externalID)
        if (strict) throw error
      }
    })()
    return placeholder.pending
  }
  const forget = async externalID => { let state = sessions.get(externalID); if (!live(state)) { let acquired = await acquire(externalID); if (acquired?.terminal) return; state = sessions.get(externalID); if (!live(state)) { acquired = await acquire(externalID, true); if (acquired?.terminal) return; state = acquired; if (!live(state)) throw new Error("VGXNESS lifecycle reacquisition failed") } }; await end(state); if (sessions.get(externalID) === state) sessions.delete(externalID) }
  return {
    event: async input => {
      const event = input?.event, info = event?.properties?.info, externalID = identifier(info?.id)
      if (!externalID || info?.parentID) return
      if (event?.type === "session.deleted") return await forget(externalID)
      try {
      if (event?.type?.startsWith("session.") && !sessions.has(externalID)) { await acquire(externalID); if (sessions.size > MAX_SESSIONS) { const oldest = sessions.keys().next().value, state = sessions.get(oldest); sessions.delete(oldest); if (state?.handle) void end(state).catch(() => {}) } }
      } catch {}
    },
    "experimental.chat.system.transform": async (input, output) => { try {
      const state = sessions.get(identifier(input?.sessionID)); if (!live(state)) return
      const renewal = await invoke("renew", { session_handle: state.handle, lease_token: state.leaseToken })
      if (sessions.get(identifier(input?.sessionID)) !== state || !activeReceipt(state, renewal)) return
      if (state.contextLoaded) return
      state.contextLoaded = true
      const result = await invoke("context", { session_handle: state.handle })
      const hasHandoff = Object.prototype.hasOwnProperty.call(result, "handoff")
      if (sessions.get(identifier(input?.sessionID)) !== state || result?.session_handle !== state.handle || (hasHandoff && typeof result.handoff !== "string")) return
      const handoff = typeof result?.handoff === "string" ? result.handoff : ""
      const block = "<VGXNESS LIFECYCLE session_handle=\"" + state.handle + "\">\nBefore your terminal response, use the existing MCP memory_session_summary to save a concise summary for this session.\n<UNTRUSTED DATA>\n" + untrusted(handoff) + "\n</UNTRUSTED DATA>\n</VGXNESS LIFECYCLE>"
      if (Array.isArray(output?.system)) output.system.push(block)
    } catch {} },
    "experimental.session.compacting": async input => { try { const state = sessions.get(identifier(input?.sessionID)); if (live(state)) await invoke("checkpoint", { session_handle: state.handle, lease_token: state.leaseToken }) } catch {} },
    "tool.execute.after": async input => { try { const state = sessions.get(identifier(input?.sessionID)); if (live(state) && input?.tool === "vgxness_memory_session_summary" && identifier(input?.callID)) state.summaryCompleted = true } catch {} },
    dispose: async () => { try { disposed = true; const active = [...sessions.values()].filter(live); sessions.clear(); await Promise.allSettled(active.map(end)) } catch {} },
  }
}
`)
}

func inspectDirectory(path string) (exists, drifted bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return true, true, nil
	}
	return true, false, nil
}

func recoveryFailure(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s: %v", integration.ErrRecovery, action, err)
}

func integrationConfigDirectory(options integration.Options) (string, error) {
	if options.ConfigDir != "" {
		if !filepath.IsAbs(options.ConfigDir) {
			return "", fmt.Errorf("%w: OpenCode config directory must be absolute", integration.ErrInvalid)
		}
		return filepath.Clean(options.ConfigDir), nil
	}
	home := options.HomeDir
	var err error
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: home directory must be absolute", integration.ErrInvalid)
	}
	return filepath.Join(filepath.Clean(home), ".config", "opencode"), nil
}

func prepareDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return integration.ErrDrift
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := prepareDirectory(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return integration.ErrDrift
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	if err := syncDirectory(path); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func readRegularFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() > maxArtifactBytes {
		return nil, integration.ErrDrift
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() > maxArtifactBytes {
		return nil, integration.ErrDrift
	}
	data, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtifactBytes {
		return nil, integration.ErrDrift
	}
	return data, nil
}

func isManagedPredecessor(candidate, current []byte, predecessors [][]byte, recognize func([]byte) bool) bool {
	currentIdentity, currentVersion, currentOK := managedArtifactMarker(current)
	candidateIdentity, candidateVersion, candidateOK := managedArtifactMarker(candidate)
	if !currentOK || !candidateOK || candidateIdentity != currentIdentity || candidateVersion > currentVersion {
		return false
	}
	if candidateVersion == currentVersion {
		return recognize != nil && recognize(candidate)
	}
	for _, predecessor := range predecessors {
		if len(predecessor) != 0 && bytes.Equal(candidate, predecessor) {
			return true
		}
	}
	return recognize != nil && recognize(candidate)
}

func managedArtifactMarker(content []byte) (string, int, bool) {
	var identity string
	var version int
	found := false
	for _, rawLine := range bytes.Split(content, []byte{'\n'}) {
		line := string(rawLine)
		body := ""
		switch {
		case strings.HasPrefix(line, "<!-- managed-by: vgxness; artifact: ") && strings.HasSuffix(line, " -->"):
			body = strings.TrimSuffix(strings.TrimPrefix(line, "<!-- managed-by: vgxness; artifact: "), " -->")
		case strings.HasPrefix(line, "// managed-by: vgxness; artifact: "):
			body = strings.TrimPrefix(line, "// managed-by: vgxness; artifact: ")
		default:
			continue
		}
		if found || strings.Count(body, "; version: ") != 1 {
			return "", 0, false
		}
		name, versionText, ok := strings.Cut(body, "; version: ")
		parsed, err := strconv.Atoi(versionText)
		if !ok || name == "" || err != nil || parsed < 0 || strconv.Itoa(parsed) != versionText {
			return "", 0, false
		}
		identity, version, found = name, parsed, true
	}
	return identity, version, found
}

func artifactSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
