package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vgxness/vgxness/internal/integration"
)

type reinstallTransaction struct {
	service         *Integration
	root            *rootTransaction
	state           inspection
	created         []rootInstalledArtifact
	retired         []rootRetiredArtifact
	pendingEvidence reinstallPendingEvidence
	expectedLayout  integration.ManagedLayout
}

func (service *Integration) preflightReinstall(ctx context.Context, options integration.Options, configDirectory string, root *rootTransaction) (*reinstallTransaction, error) {
	if pending, err := service.reinstallPendingAtRoot(ctx, options, root); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, integration.ErrInvalid) {
			return nil, err
		}
		return nil, errors.Join(integration.ErrRecovery, err)
	} else if pending {
		return nil, fmt.Errorf("%w: interrupted OpenCode reinstall evidence is present", integration.ErrRecovery)
	}
	state, err := service.inspect(ctx, options)
	if err != nil {
		return nil, err
	}
	switch state.result.State {
	case integration.StateInstalled, integration.StatePartial:
	case integration.StateAbsent:
		return nil, fmt.Errorf("%w: managed OpenCode artifacts are absent", integration.ErrInvalid)
	default:
		return nil, fmt.Errorf("%w: managed OpenCode artifacts", integration.ErrDrift)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	held, err := root.HeldAtPath()
	if err != nil || !held {
		return nil, fmt.Errorf("%w: OpenCode config root changed after preflight", integration.ErrConflict)
	}
	expectedLayout, err := managedLayout(configDirectory, state.artifacts)
	if err != nil {
		return nil, err
	}
	return &reinstallTransaction{
		service:        service,
		root:           root,
		state:          state,
		created:        make([]rootInstalledArtifact, 0, len(state.artifacts)),
		retired:        make([]rootRetiredArtifact, 0, len(state.retired)),
		expectedLayout: expectedLayout,
	}, nil
}

func (transaction *reinstallTransaction) stage() error {
	for _, item := range transaction.state.artifacts {
		name, relativeErr := transaction.root.Relative(item.path)
		if relativeErr != nil {
			return fmt.Errorf("%w: OpenCode artifact outside config root", integration.ErrInvalid)
		}
		staged, stageErr := transaction.root.StageArtifact(name, item.content, 0o600)
		if stageErr != nil {
			return fmt.Errorf("stage OpenCode reinstall artifact: %w", stageErr)
		}
		transaction.created = append(transaction.created, rootInstalledArtifact{name: name, staged: staged})
	}
	if transaction.service.afterReinstallStaging != nil {
		staged := make([]installedArtifact, 0, len(transaction.created))
		for _, item := range transaction.created {
			staged = append(staged, installedArtifact{path: filepath.Join(transaction.root.path, item.name), temporary: filepath.Join(transaction.root.path, item.staged.temporary), temporaryInfo: item.staged.temporaryInfo, staging: filepath.Join(transaction.root.path, item.staged.staging), stagingInfo: item.staged.stagingInfo, content: item.staged.content})
		}
		transaction.service.afterReinstallStaging(staged)
	}
	return nil
}

func (transaction *reinstallTransaction) publish(ctx context.Context) error {
	held, err := transaction.root.HeldAtPath()
	if err != nil || !held {
		return fmt.Errorf("%w: OpenCode config root changed before mutation", integration.ErrConflict)
	}
	transaction.pendingEvidence, err = transaction.service.writeReinstallPendingAtRoot(ctx, transaction.root, transaction.expectedLayout)
	if err != nil {
		return errors.Join(integration.ErrRecovery, fmt.Errorf("write reinstall pending marker: %w", err))
	}
	for index, item := range transaction.state.artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		installed := &transaction.created[index]
		if item.present {
			expected := item.content
			if item.upgrade || item.defaultAgent != nil && item.prior != nil {
				expected = item.prior
			}
			backup, backupErr := transaction.root.Anchor(installed.name, expected)
			if backupErr != nil {
				return fmt.Errorf("%w: protect OpenCode reinstall predecessor", integration.ErrConflict)
			}
			installed.backup = &backup
			if transaction.service.afterReinstallAnchorPath != nil {
				transaction.service.afterReinstallAnchorPath(filepath.Join(transaction.root.path, backup.name))
			}
			if err := transaction.root.RemoveExact(installed.name, backup.info, expected); err != nil {
				return fmt.Errorf("%w: remove OpenCode reinstall predecessor: %v", integration.ErrConflict, err)
			}
		}
		if transaction.service.reinstallCheckpoint != nil {
			if err := transaction.service.reinstallCheckpoint(reinstallCheckpointMoved, item.path); err != nil {
				return err
			}
		}
		publication, publishErr := transaction.root.PublishStaged(installed.staged, installed.name)
		if publishErr != nil {
			if publication.state == rootPublicationPending {
				installed.publication = publication
			}
			return fmt.Errorf("publish OpenCode reinstall artifact: %w", publishErr)
		}
		installed.published = publication.info
		if transaction.service.reinstallCheckpoint != nil {
			if err := transaction.service.reinstallCheckpoint(reinstallCheckpointPublished, item.path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (transaction *reinstallTransaction) retireAndVerify() error {
	for _, item := range transaction.state.retired {
		name, relativeErr := transaction.root.Relative(item.path)
		if relativeErr != nil {
			return fmt.Errorf("%w: retired OpenCode artifact outside config root", integration.ErrInvalid)
		}
		retiredItem, retireErr := retireRootArtifact(transaction.root, name, item)
		if retireErr != nil {
			return retireErr
		}
		transaction.retired = append(transaction.retired, retiredItem)
		if transaction.service.afterRetirement != nil {
			if err := transaction.service.afterRetirement(); err != nil {
				return err
			}
		}
	}
	if err := verifyRootInstall(transaction.root, transaction.state); err != nil {
		return fmt.Errorf("read back OpenCode reinstall artifacts: %w", integration.ErrDrift)
	}
	for _, item := range transaction.state.artifacts {
		if transaction.service.reinstallCheckpoint != nil {
			if err := transaction.service.reinstallCheckpoint(reinstallCheckpointVerified, item.path); err != nil {
				return err
			}
		}
	}
	for _, item := range transaction.created {
		if item.backup == nil {
			continue
		}
		data, info, err := transaction.root.ReadRegularInfo(item.backup.name)
		if err != nil || item.backup.info == nil || !os.SameFile(info, item.backup.info) || !bytes.Equal(data, item.backup.content) {
			return fmt.Errorf("%w: reinstall predecessor anchor changed before cleanup", integration.ErrDrift)
		}
	}
	return nil
}

func (transaction *reinstallTransaction) finish(returnErr error, rollback bool) error {
	if rollback {
		var recoveryErr error
		for index := len(transaction.retired) - 1; index >= 0; index-- {
			if err := transaction.root.RestoreBackup(transaction.retired[index].backup, transaction.retired[index].name); err != nil {
				recoveryErr = errors.Join(recoveryErr, recoveryFailure("restore retired OpenCode artifact", err))
			}
		}
		for index := len(transaction.created) - 1; index >= 0; index-- {
			if err := rollbackRootReinstalledArtifact(transaction.root, transaction.created[index]); err != nil {
				recoveryErr = errors.Join(recoveryErr, recoveryFailure("restore OpenCode reinstall predecessor", err))
			}
		}
		if recoveryErr == nil && transaction.pendingEvidence.info != nil {
			recoveryErr = clearReinstallPendingAtRoot(transaction.root, transaction.pendingEvidence)
		} else if recoveryErr != nil && transaction.pendingEvidence.info != nil {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("%w: reinstall pending marker retained at %q", integration.ErrRecovery, filepath.Join(transaction.root.path, reinstallPendingName)))
		}
		return errors.Join(returnErr, recoveryErr)
	}
	for _, item := range transaction.retired {
		returnErr = errors.Join(returnErr, transaction.root.cleanupBackup(item.backup))
	}
	for _, item := range transaction.created {
		if item.backup != nil {
			returnErr = errors.Join(returnErr, transaction.root.cleanupBackup(*item.backup))
		}
		returnErr = errors.Join(returnErr, transaction.root.CleanupStaged(item.staged))
	}
	if transaction.pendingEvidence.info != nil {
		returnErr = errors.Join(returnErr, clearReinstallPendingAtRoot(transaction.root, transaction.pendingEvidence))
	}
	return returnErr
}
