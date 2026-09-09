package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vgxness/vgxness/internal/integration"
)

// PreviewMCPRepair verifies an explicitly identified obsolete managed MCP
// entry without changing either the OpenCode configuration or its ownership
// state. It is intentionally narrower than Install: only schema-v1 owned MCP
// entries that did not replace a user entry can use this route.
func (service *Integration) PreviewMCPRepair(ctx context.Context, options integration.Options, proof integration.MCPRepairProof) (integration.Result, error) {
	return service.mcpRepair(ctx, options, proof, false)
}

// RepairMCP replaces one proven obsolete managed MCP entry with the current
// stable launcher. Ordinary install remains fail-closed for this condition.
func (service *Integration) RepairMCP(ctx context.Context, options integration.Options, proof integration.MCPRepairProof) (integration.Result, error) {
	return service.mcpRepair(ctx, options, proof, true)
}

func (service *Integration) mcpRepair(ctx context.Context, options integration.Options, proof integration.MCPRepairProof, apply bool) (result integration.Result, returnErr error) {
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	if apply {
		if err := service.validateMutableLauncher(); err != nil {
			return integration.Result{}, err
		}
	} else if _, err := validateManagedLauncher(service.executable); err != nil {
		return integration.Result{}, fmt.Errorf("%w: managed VGXNESS launcher: %w", integration.ErrInvalid, err)
	}
	if apply && !validMCPRepairProof(proof) {
		return integration.Result{}, fmt.Errorf("%w: MCP repair proof", integration.ErrInvalid)
	}
	if !apply && (proof.OldExecutable != "" || proof.ExpectedEntrySHA256 != "") && !validMCPRepairProof(proof) {
		return integration.Result{}, fmt.Errorf("%w: MCP repair proof", integration.ErrInvalid)
	}
	rootPath, err := integrationConfigDirectory(options)
	if err != nil {
		return integration.Result{}, err
	}
	root, err := openRootTransaction(rootPath, false)
	if err != nil {
		return integration.Result{}, fmt.Errorf("%w: open OpenCode config root: %v", integration.ErrConflict, err)
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	if service.afterMCPRepairRoot != nil {
		service.afterMCPRepairRoot(root)
	}
	config, configInfo, err := root.ReadRegularInfo(defaultAgentConfigName)
	if err != nil {
		return integration.Result{}, fmt.Errorf("%w: OpenCode configuration", integration.ErrDrift)
	}
	stateData, stateInfo, err := root.ReadRegularInfo(filepath.Join("vgxness", defaultAgentStateName))
	if err != nil {
		return integration.Result{}, fmt.Errorf("%w: OpenCode MCP ownership state", integration.ErrDrift)
	}
	oldExecutable, entrySHA, mode, err := inspectMCPRepairEntry(config, stateData)
	if err != nil {
		return integration.Result{}, err
	}
	result = integration.Result{Provider: "opencode", State: integration.StateInstalled, Path: filepath.Join(rootPath, defaultAgentConfigName), MCPRepairOldExecutable: oldExecutable, MCPRepairEntrySHA256: entrySHA, MCPRepairMode: mode}
	if !apply && !validMCPRepairProof(proof) {
		return result, nil
	}
	replacement, err := service.repairedMCPConfig(config, stateData, proof)
	if err != nil {
		return integration.Result{}, err
	}
	result.ArtifactSHA256, result.Changed = artifactSHA256(replacement), !bytes.Equal(config, replacement)
	result.RestartRequired = result.Changed
	if !apply || !result.Changed {
		return result, nil
	}
	held, err := root.HeldAtPath()
	if err != nil || !held {
		return integration.Result{}, fmt.Errorf("%w: OpenCode config root changed before repair", integration.ErrConflict)
	}
	// Recheck both artifacts at the mutation boundary. The state is not changed,
	// but its identity is part of the ownership proof.
	if same, err := root.Same(defaultAgentConfigName, configInfo); err != nil || !same {
		return integration.Result{}, fmt.Errorf("%w: OpenCode configuration changed before repair", integration.ErrConflict)
	}
	if same, err := root.Same(filepath.Join("vgxness", defaultAgentStateName), stateInfo); err != nil || !same {
		return integration.Result{}, fmt.Errorf("%w: OpenCode ownership state changed before repair", integration.ErrConflict)
	}
	if current, _, err := root.ReadRegularInfo(filepath.Join("vgxness", defaultAgentStateName)); err != nil || !bytes.Equal(current, stateData) {
		return integration.Result{}, fmt.Errorf("%w: OpenCode ownership state bytes changed before repair", integration.ErrConflict)
	}
	staged, err := root.StageArtifact(defaultAgentConfigName, replacement, 0o600)
	if err != nil {
		return integration.Result{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.CleanupStaged(staged)) }()
	backup, err := root.Backup(defaultAgentConfigName, config)
	if err != nil {
		return integration.Result{}, err
	}
	defer func() {
		if returnErr != nil {
			if held, _ := root.HeldAtPath(); held {
				if data, info, readErr := root.ReadRegularInfo(backup.name); readErr == nil && os.SameFile(info, backup.info) && bytes.Equal(data, config) {
					result.BackupPath = filepath.Join(rootPath, backup.name)
				}
			}
		}
		returnErr = errors.Join(returnErr, root.releaseBackup(backup))
	}()
	removed := false
	rollback := true
	var published rootPublication
	defer func() {
		if rollback && removed {
			if current, info, readErr := root.ReadRegularInfo(defaultAgentConfigName); readErr == nil && bytes.Equal(current, replacement) && published.info != nil && os.SameFile(info, published.info) {
				if removeErr := root.RemoveExact(defaultAgentConfigName, info, replacement); removeErr != nil {
					returnErr = errors.Join(returnErr, integration.ErrRecovery, removeErr)
					return
				}
			}
			if restoreErr := root.RestoreBackup(backup, defaultAgentConfigName); restoreErr != nil {
				returnErr = errors.Join(returnErr, integration.ErrRecovery, restoreErr)
			}
			return
		}
		returnErr = errors.Join(returnErr, root.cleanupBackup(backup))
	}()
	if err := root.RemoveExact(defaultAgentConfigName, configInfo, config); err != nil {
		return integration.Result{}, err
	}
	removed = true
	published, err = root.PublishStaged(staged, defaultAgentConfigName)
	if err != nil {
		return integration.Result{}, err
	}
	data, info, err := root.ReadRegularInfo(defaultAgentConfigName)
	if err != nil || published.info == nil || !os.SameFile(info, published.info) || !bytes.Equal(data, replacement) {
		return integration.Result{}, fmt.Errorf("%w: OpenCode MCP repair readback", integration.ErrRecovery)
	}
	held, err = root.HeldAtPath()
	if err != nil || !held {
		return integration.Result{}, fmt.Errorf("%w: OpenCode config root changed during repair", integration.ErrConflict)
	}
	currentState, currentStateInfo, err := root.ReadRegularInfo(filepath.Join("vgxness", defaultAgentStateName))
	if err != nil || !os.SameFile(currentStateInfo, stateInfo) || !bytes.Equal(currentState, stateData) {
		return integration.Result{}, fmt.Errorf("%w: OpenCode ownership state changed during repair", integration.ErrConflict)
	}
	rollback = false
	return result, nil
}

func validMCPRepairProof(proof integration.MCPRepairProof) bool {
	return filepath.IsAbs(proof.OldExecutable) && filepath.Clean(proof.OldExecutable) == proof.OldExecutable &&
		len(proof.ExpectedEntrySHA256) == sha256.Size*2 && strings.Trim(proof.ExpectedEntrySHA256, "0123456789abcdef") == ""
}

func canonicalMCPEntrySHA256(entry []byte) (string, error) {
	var value any
	if err := json.Unmarshal(entry, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func (service *Integration) repairedMCPConfig(config, stateData []byte, proof integration.MCPRepairProof) ([]byte, error) {
	values, _, err := readOpenCodeConfigFromBytes(config)
	if err != nil {
		return nil, err
	}
	_, present, err := openCodeMCP(values)
	if err != nil || !present {
		return nil, fmt.Errorf("%w: OpenCode MCP entry", integration.ErrDrift)
	}
	oldExecutable, entrySHA, mode, err := inspectMCPRepairEntry(config, stateData)
	if err != nil {
		return nil, err
	}
	if oldExecutable == service.executable {
		return config, nil
	}
	if proof.OldExecutable != oldExecutable || entrySHA != proof.ExpectedEntrySHA256 {
		return nil, fmt.Errorf("%w: OpenCode MCP entry proof", integration.ErrDrift)
	}
	var desired json.RawMessage
	if mode == "readonly" {
		desired = managedReadOnlyMCPConfig(service.executable)
	} else {
		desired, err = managedMCPConfig(service.executable)
	}
	if err != nil {
		return nil, err
	}
	servers := make(map[string]json.RawMessage)
	if err := json.Unmarshal(values["mcp"], &servers); err != nil || servers == nil {
		return nil, fmt.Errorf("%w: OpenCode MCP configuration", integration.ErrDrift)
	}
	servers["vgxness"] = desired
	raw, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	values["mcp"] = raw
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func inspectMCPRepairEntry(config, stateData []byte) (string, string, string, error) {
	var state defaultAgentState
	if err := json.Unmarshal(stateData, &state); err != nil || !validDefaultAgentState(state) || state.SchemaVersion != 1 || !state.MCPOwned || state.MCPExisted {
		return "", "", "", fmt.Errorf("%w: OpenCode MCP ownership state", integration.ErrDrift)
	}
	values, _, err := readOpenCodeConfigFromBytes(config)
	if err != nil {
		return "", "", "", err
	}
	entry, present, err := openCodeMCP(values)
	if err != nil || !present {
		return "", "", "", fmt.Errorf("%w: OpenCode MCP entry", integration.ErrDrift)
	}
	var parsed struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(entry, &parsed); err != nil || len(parsed.Command) < 2 || !filepath.IsAbs(parsed.Command[0]) || filepath.Clean(parsed.Command[0]) != parsed.Command[0] || parsed.Command[1] != "mcp" {
		return "", "", "", fmt.Errorf("%w: obsolete OpenCode MCP command", integration.ErrDrift)
	}
	mode := "full"
	full, fullErr := managedMCPConfig(parsed.Command[0])
	if fullErr == nil && sameJSONValue(entry, full) {
		mode = "full"
	} else if sameJSONValue(entry, managedReadOnlyMCPConfig(parsed.Command[0])) {
		mode = "readonly"
	} else {
		return "", "", "", fmt.Errorf("%w: obsolete OpenCode MCP command", integration.ErrDrift)
	}
	digest, err := canonicalMCPEntrySHA256(entry)
	return parsed.Command[0], digest, mode, err
}
