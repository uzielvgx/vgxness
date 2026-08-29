package memory

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) StartProviderSession(ctx context.Context, request ProviderSessionStart) (ProviderSession, error) {
	project, provider, external, ok := providerSessionInput(request)
	if s == nil || s.readOnly || !ok {
		return ProviderSession{}, fmt.Errorf("%w: provider session", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO projects(id) VALUES(?)`, project); err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	hash := sha256.Sum256([]byte(external))
	now := s.now().UTC()
	if err := reconcileProviderSessions(ctx, tx, project, provider, hash[:], now); err != nil {
		return ProviderSession{}, err
	}
	value, found, err := loadProviderSession(tx.QueryRowContext(ctx, providerSessionSelect+` WHERE project_id=? AND provider=? AND external_id_hash=?`, project, provider, hash[:]))
	if err != nil {
		return ProviderSession{}, err
	}
	if found {
		if value.State == ProviderSessionActive {
			token, tokenErr := newProviderSessionToken()
			if tokenErr != nil {
				return ProviderSession{}, tokenErr
			}
			updated := monotonicProviderSessionTime(now, value.UpdatedAt)
			until := updated.Add(providerSessionLeaseTTL)
			query := `UPDATE local_provider_sessions SET lease_token=?,lease_until=?,updated_at=? WHERE handle=? AND project_id=? AND state='active' AND lease_token=?`
			args := []any{token, until.UnixNano(), updated.UnixNano(), value.Handle, project, value.LeaseToken}
			if value.LeaseToken == "" && value.LeaseUntil == nil {
				query = `UPDATE local_provider_sessions SET lease_token=?,lease_until=?,updated_at=? WHERE handle=? AND project_id=? AND state='active' AND lease_token IS NULL AND lease_until IS NULL`
				args = args[:5]
			}
			result, updateErr := tx.ExecContext(ctx, query, args...)
			if updateErr != nil {
				return ProviderSession{}, writeError(ctx, updateErr)
			}
			changed, _ := result.RowsAffected()
			if changed != 1 {
				return ProviderSession{}, fmt.Errorf("%w: provider session takeover", ErrConflict)
			}
			var draft int
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM local_provider_session_drafts WHERE handle=? AND project_id=?)`, value.Handle, project).Scan(&draft); err != nil {
				return ProviderSession{}, writeError(ctx, err)
			}
			value.LeaseToken, value.LeaseUntil, value.UpdatedAt, value.DraftPresent = token, &until, updated, draft == 1
		}
		return value, commit(ctx, tx)
	}
	handle, err := newProviderSessionHandle()
	if err != nil {
		return ProviderSession{}, err
	}
	token, err := newProviderSessionToken()
	if err != nil {
		return ProviderSession{}, err
	}
	until := now.Add(providerSessionLeaseTTL)
	if _, err = tx.ExecContext(ctx, `INSERT INTO local_provider_sessions(handle,project_id,provider,external_id_hash,state,checkpointed,lease_token,lease_until,created_at,updated_at) VALUES(?,?,?,?,?,0,?,?,?,?)`, handle, project, provider, hash[:], ProviderSessionActive, token, until.UnixNano(), now.UnixNano(), now.UnixNano()); err != nil {
		return ProviderSession{}, conflictOrWrite(ctx, err)
	}
	return ProviderSession{Handle: handle, Project: project, Provider: provider, State: ProviderSessionActive, LeaseToken: token, LeaseUntil: &until, CreatedAt: now, UpdatedAt: now}, commit(ctx, tx)
}

func (s *Store) MarkProviderSessionCheckpoint(ctx context.Context, project, handle, token string) (ProviderSession, error) {
	if s == nil || s.readOnly || !validProviderSessionIdentity(project, handle) || !validProviderSessionToken(token) {
		return ProviderSession{}, fmt.Errorf("%w: provider session", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	value, found, err := loadProviderSession(tx.QueryRowContext(ctx, providerSessionSelect+` WHERE project_id=? AND handle=?`, project, handle))
	if err != nil {
		return ProviderSession{}, err
	}
	if !found {
		return ProviderSession{}, fmt.Errorf("%w: provider session", ErrNotFound)
	}
	if value.State != ProviderSessionActive || value.LeaseToken != token {
		return ProviderSession{}, fmt.Errorf("%w: provider session is closed", ErrConflict)
	}
	now := s.now().UTC()
	now = monotonicProviderSessionTime(now, value.UpdatedAt)
	until := now.Add(providerSessionLeaseTTL)
	result, err := tx.ExecContext(ctx, `UPDATE local_provider_sessions SET checkpointed=1,lease_until=?,updated_at=? WHERE handle=? AND project_id=? AND state='active' AND lease_token=?`, until.UnixNano(), now.UnixNano(), handle, project, token)
	if err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ProviderSession{}, fmt.Errorf("%w: provider session checkpoint", ErrConflict)
	}
	value.Checkpointed, value.UpdatedAt, value.LeaseUntil = true, now, &until
	return value, commit(ctx, tx)
}

func (s *Store) RenewProviderSession(ctx context.Context, project, handle, token string) (ProviderSession, error) {
	if s == nil || s.readOnly || !validProviderSessionIdentity(project, handle) || !validProviderSessionToken(token) {
		return ProviderSession{}, fmt.Errorf("%w: provider session", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	value, found, err := loadProviderSession(tx.QueryRowContext(ctx, providerSessionSelect+` WHERE project_id=? AND handle=?`, project, handle))
	if err != nil {
		return ProviderSession{}, err
	}
	if !found {
		return ProviderSession{}, fmt.Errorf("%w: provider session", ErrNotFound)
	}
	if value.State != ProviderSessionActive || value.LeaseToken != token {
		return ProviderSession{}, fmt.Errorf("%w: provider session lease", ErrConflict)
	}
	now := monotonicProviderSessionTime(s.now().UTC(), value.UpdatedAt)
	until := now.Add(providerSessionLeaseTTL)
	result, updateErr := tx.ExecContext(ctx, `UPDATE local_provider_sessions SET lease_until=?,updated_at=? WHERE handle=? AND project_id=? AND state='active' AND lease_token=?`, until.UnixNano(), now.UnixNano(), handle, project, token)
	if updateErr != nil {
		return ProviderSession{}, writeError(ctx, updateErr)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ProviderSession{}, fmt.Errorf("%w: provider session lease", ErrConflict)
	}
	value.UpdatedAt, value.LeaseUntil = now, &until
	return value, commit(ctx, tx)
}

// SaveProviderSessionDraft stores one local-only optimistic draft. Its content
// never enters the sync outbox and is returned only as metadata.
func (s *Store) SaveProviderSessionDraft(ctx context.Context, request ProviderSessionDraftSave) (ProviderSessionDraft, error) {
	if s == nil || s.readOnly || !validProviderSessionIdentity(request.Project, request.Handle) || !validText(request.Summary, 4096, false) || strings.Contains(request.Summary, request.Handle) {
		return ProviderSessionDraft{}, fmt.Errorf("%w: provider session draft", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderSessionDraft{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	value, found, err := loadProviderSession(tx.QueryRowContext(ctx, providerSessionSelect+` WHERE project_id=? AND handle=?`, request.Project, request.Handle))
	if err != nil {
		return ProviderSessionDraft{}, err
	}
	if !found {
		return ProviderSessionDraft{}, fmt.Errorf("%w: provider session", ErrNotFound)
	}
	if value.State != ProviderSessionActive {
		return ProviderSessionDraft{}, fmt.Errorf("%w: provider session is closed", ErrConflict)
	}
	var old sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT updated_at FROM local_provider_session_drafts WHERE handle=? AND project_id=?`, request.Handle, request.Project).Scan(&old); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ProviderSessionDraft{}, writeError(ctx, err)
	}
	if old.Valid != !request.ExpectedUpdatedAt.IsZero() || old.Valid && old.Int64 != request.ExpectedUpdatedAt.UnixNano() {
		return ProviderSessionDraft{}, fmt.Errorf("%w: stale provider session draft", ErrConflict)
	}
	now := s.now().UTC()
	if old.Valid && now.UnixNano() <= old.Int64 {
		now = time.Unix(0, old.Int64).UTC().Add(time.Nanosecond)
	}
	if old.Valid {
		result, updateErr := tx.ExecContext(ctx, `UPDATE local_provider_session_drafts SET summary=?,updated_at=? WHERE handle=? AND project_id=? AND updated_at=?`, request.Summary, now.UnixNano(), request.Handle, request.Project, old.Int64)
		if updateErr != nil {
			return ProviderSessionDraft{}, conflictOrWrite(ctx, updateErr)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return ProviderSessionDraft{}, fmt.Errorf("%w: stale provider session draft", ErrConflict)
		}
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO local_provider_session_drafts(handle,project_id,summary,updated_at) VALUES(?,?,?,?)`, request.Handle, request.Project, request.Summary, now.UnixNano())
		if err != nil {
			return ProviderSessionDraft{}, conflictOrWrite(ctx, err)
		}
	}
	return ProviderSessionDraft{Handle: request.Handle, Project: request.Project, UpdatedAt: now}, commit(ctx, tx)
}

func (s *Store) EndProviderSession(ctx context.Context, request ProviderSessionEnd) (ProviderSession, error) {
	if err := cancelled(ctx); err != nil {
		return ProviderSession{}, err
	}
	if s == nil || s.readOnly || !validProviderSessionIdentity(request.Project, request.Handle) || !validText(request.ExternalID, 4096, false) || !validProviderSessionEnd(request) || !validProviderSessionToken(request.LeaseToken) {
		return ProviderSession{}, fmt.Errorf("%w: provider session end", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	value, found, err := loadProviderSession(tx.QueryRowContext(ctx, providerSessionSelect+` WHERE project_id=? AND handle=?`, request.Project, request.Handle))
	if err != nil {
		return ProviderSession{}, err
	}
	if !found {
		return ProviderSession{}, fmt.Errorf("%w: provider session", ErrNotFound)
	}
	hash := sha256.Sum256([]byte(request.ExternalID))
	var stored []byte
	if err = tx.QueryRowContext(ctx, `SELECT external_id_hash FROM local_provider_sessions WHERE handle=? AND project_id=?`, request.Handle, request.Project).Scan(&stored); err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	if len(stored) != sha256.Size || subtle.ConstantTimeCompare(stored, hash[:]) != 1 {
		return ProviderSession{}, fmt.Errorf("%w: provider session identity", ErrInvalid)
	}
	if value.State != ProviderSessionActive {
		if value.State == request.State {
			return value, commit(ctx, tx)
		}
		return ProviderSession{}, fmt.Errorf("%w: incompatible provider session close", ErrConflict)
	}
	if value.LeaseToken != request.LeaseToken {
		return ProviderSession{}, fmt.Errorf("%w: provider session lease", ErrConflict)
	}
	now := s.now().UTC()
	if request.State == ProviderSessionCompleted {
		summary := request.Summary
		if summary == "" {
			if err = tx.QueryRowContext(ctx, `SELECT summary FROM local_provider_session_drafts WHERE handle=? AND project_id=?`, request.Handle, request.Project).Scan(&summary); errors.Is(err, sql.ErrNoRows) {
				return ProviderSession{}, fmt.Errorf("%w: provider session draft", ErrNotFound)
			} else if err != nil {
				return ProviderSession{}, writeError(ctx, err)
			}
		}
		if !validText(summary, 4096, false) || strings.Contains(summary, request.ExternalID) || strings.Contains(summary, request.Handle) {
			return ProviderSession{}, fmt.Errorf("%w: provider session summary", ErrInvalid)
		}
		id, idErr := newID()
		if idErr != nil {
			return ProviderSession{}, idErr
		}
		item := Observation{ID: id, Project: request.Project, Scope: ScopeProject, Type: "summary", Content: summary, TopicKey: providerSessionSummaryTopic(id), Provenance: Provenance{Producer: "provider-session"}, State: StateActive, CreatedAt: now, UpdatedAt: now}
		if err = insertObservation(ctx, tx, item); err != nil {
			return ProviderSession{}, err
		}
		if err = s.enqueueLocalWrite(ctx, tx, item); err != nil {
			return ProviderSession{}, err
		}
		value.FinalObservationID = id
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM local_provider_session_drafts WHERE handle=? AND project_id=?`, request.Handle, request.Project); err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE local_provider_sessions SET state=?,final_observation_id=?,lease_token=NULL,lease_until=NULL,updated_at=?,completed_at=? WHERE handle=? AND project_id=? AND state='active' AND lease_token=?`, request.State, nullable(value.FinalObservationID), now.UnixNano(), now.UnixNano(), request.Handle, request.Project, request.LeaseToken)
	if err != nil {
		return ProviderSession{}, writeError(ctx, err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ProviderSession{}, fmt.Errorf("%w: provider session close", ErrConflict)
	}
	value.State, value.UpdatedAt, value.CompletedAt = request.State, now, &now
	return value, commit(ctx, tx)
}

// ProviderSessionContext exposes only the newest completed, same-project summary.
func (s *Store) ProviderSessionContext(ctx context.Context, project, handle string) (ProviderSessionContext, error) {
	if err := cancelled(ctx); err != nil {
		return ProviderSessionContext{}, err
	}
	if s == nil || !validProviderSessionIdentity(project, handle) {
		return ProviderSessionContext{}, fmt.Errorf("%w: provider session", ErrInvalid)
	}
	current, found, err := loadProviderSession(s.db.QueryRowContext(ctx, providerSessionSelect+` WHERE project_id=? AND handle=?`, project, handle))
	if err != nil {
		return ProviderSessionContext{}, err
	}
	if !found || current.State != ProviderSessionActive {
		return ProviderSessionContext{}, fmt.Errorf("%w: active provider session", ErrConflict)
	}
	var id, content string
	err = s.db.QueryRowContext(ctx, `SELECT o.id,o.content FROM local_provider_sessions p JOIN observations o ON o.id=p.final_observation_id WHERE p.project_id=? AND p.state='completed' AND p.handle<>? ORDER BY p.completed_at DESC,p.handle ASC LIMIT 1`, project, handle).Scan(&id, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderSessionContext{Session: current}, nil
	}
	if err != nil {
		return ProviderSessionContext{}, writeError(ctx, err)
	}
	return ProviderSessionContext{Session: current, Handoff: boundedHandoff(id, content)}, nil
}

func (s *Store) UpdateObservation(ctx context.Context, request ObservationUpdate) (Observation, error) {
	if s == nil || s.readOnly || request.ID == "" || request.Project == "" || request.ExpectedUpdatedAt.IsZero() || !validText(request.Content, 4096, false) {
		return Observation{}, fmt.Errorf("%w: observation update", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	defer tx.Rollback()
	item, found, err := loadOne(tx.QueryRowContext(ctx, observationSelect+` WHERE o.id=? AND o.project_id=?`, request.ID, request.Project))
	if err != nil {
		return Observation{}, err
	}
	if !found {
		return Observation{}, fmt.Errorf("%w: observation", ErrNotFound)
	}
	if isProviderSessionSummary(item) {
		return Observation{}, fmt.Errorf("%w: provider session summary is immutable", ErrConflict)
	}
	now := s.now().UTC()
	if !now.After(item.UpdatedAt) {
		now = item.UpdatedAt.Add(time.Nanosecond)
	}
	result, err := tx.ExecContext(ctx, `UPDATE observations SET content=?,updated_at=? WHERE id=? AND updated_at=?`, request.Content, now.UnixNano(), item.ID, request.ExpectedUpdatedAt.UnixNano())
	if err != nil {
		return Observation{}, writeError(ctx, err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return Observation{}, fmt.Errorf("%w: stale observation", ErrConflict)
	}
	var indexed int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM observations_fts WHERE id=?)`, item.ID).Scan(&indexed); err != nil {
		return Observation{}, writeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM observations_fts WHERE id=?`, item.ID); err != nil {
		return Observation{}, writeError(ctx, err)
	}
	if indexed == 1 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO observations_fts(id,title,topic_key,type,content) VALUES(?,?,?,?,?)`, item.ID, item.Title, item.TopicKey, item.Type, request.Content); err != nil {
			return Observation{}, writeError(ctx, err)
		}
	}
	item.Content, item.UpdatedAt = request.Content, now
	if err = s.enqueueLocalWrite(ctx, tx, item); err != nil {
		return Observation{}, err
	}
	return item, commit(ctx, tx)
}

const providerSessionLeaseTTL = 24 * time.Hour
const providerSessionSelect = `SELECT handle,project_id,provider,state,checkpointed,COALESCE(final_observation_id,''),lease_token,lease_until,created_at,updated_at,completed_at FROM local_provider_sessions`

func loadProviderSession(row scanner) (ProviderSession, bool, error) {
	var value ProviderSession
	var checkpointed int
	var created, updated int64
	var completed, until sql.NullInt64
	var token sql.NullString
	err := row.Scan(&value.Handle, &value.Project, &value.Provider, &value.State, &checkpointed, &value.FinalObservationID, &token, &until, &created, &updated, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderSession{}, false, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ProviderSession{}, false, err
	}
	if err != nil || checkpointed < 0 || checkpointed > 1 {
		return ProviderSession{}, false, fmt.Errorf("%w: provider session", ErrCorrupt)
	}
	value.Checkpointed, value.CreatedAt, value.UpdatedAt = checkpointed == 1, time.Unix(0, created).UTC(), time.Unix(0, updated).UTC()
	if token.Valid != until.Valid || token.Valid && !validProviderSessionToken(token.String) || until.Valid && until.Int64 <= 0 {
		return ProviderSession{}, false, fmt.Errorf("%w: provider session lease", ErrCorrupt)
	}
	if token.Valid {
		value.LeaseToken = token.String
		at := time.Unix(0, until.Int64).UTC()
		value.LeaseUntil = &at
	}
	if completed.Valid {
		at := time.Unix(0, completed.Int64).UTC()
		value.CompletedAt = &at
	}
	return value, true, nil
}
func providerSessionInput(request ProviderSessionStart) (string, string, string, bool) {
	project, provider, external := strings.TrimSpace(request.Project), strings.ToLower(strings.TrimSpace(request.Provider)), request.ExternalID
	return project, provider, external, project != "" && provider != "" && validText(external, 4096, false) && project == request.Project && len([]rune(project)) <= 256 && len([]rune(provider)) <= 128
}
func validProviderSessionIdentity(project, handle string) bool {
	return validText(project, 256, false) && validText(handle, 128, false)
}
func validProviderSessionEnd(request ProviderSessionEnd) bool {
	if request.State != ProviderSessionCompleted && request.State != ProviderSessionInterrupted && request.State != ProviderSessionCancelled {
		return false
	}
	if request.State != ProviderSessionCompleted {
		return request.Summary == ""
	}
	return request.Summary == "" || validText(request.Summary, 4096, false)
}
func newProviderSessionHandle() (string, error) {
	id, err := newID()
	return strings.Replace(id, "obs-", "ps-", 1), err
}
func newProviderSessionToken() (string, error)    { return newID() }
func validProviderSessionToken(token string) bool { return validText(token, 128, false) }
func monotonicProviderSessionTime(now, previous time.Time) time.Time {
	if !now.After(previous) {
		return previous.Add(time.Nanosecond)
	}
	return now
}
func reconcileProviderSessions(ctx context.Context, tx *sql.Tx, project, provider string, excludedHash []byte, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT handle,updated_at,created_at FROM local_provider_sessions WHERE project_id=? AND state='active' AND NOT (provider=? AND external_id_hash=?) AND (lease_until IS NULL OR lease_until<=?) ORDER BY COALESCE(lease_until,0),handle LIMIT 128`, project, provider, excludedHash, now.UnixNano())
	if err != nil {
		return writeError(ctx, err)
	}
	type candidate struct {
		handle           string
		updated, created int64
	}
	candidates := make([]candidate, 0, 128)
	for rows.Next() {
		var handle string
		var updated, created int64
		if err := rows.Scan(&handle, &updated, &created); err != nil {
			return writeError(ctx, err)
		}
		candidates = append(candidates, candidate{handle, updated, created})
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return writeError(ctx, err)
	}
	for _, candidate := range candidates {
		at := now.UnixNano()
		if at <= candidate.updated {
			at = candidate.updated + 1
		}
		if at < candidate.created {
			at = candidate.created
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE local_provider_sessions SET state='interrupted',lease_token=NULL,lease_until=NULL,updated_at=?,completed_at=? WHERE handle=? AND project_id=? AND state='active' AND (lease_until IS NULL OR lease_until<=?)`, at, at, candidate.handle, project, now.UnixNano())
		if updateErr != nil {
			return writeError(ctx, updateErr)
		}
		changed, _ := result.RowsAffected()
		if changed == 0 {
			continue
		}
		if changed != 1 {
			return fmt.Errorf("%w: provider session reconciliation", ErrConflict)
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM local_provider_session_drafts WHERE handle=? AND project_id=?`, candidate.handle, project); err != nil {
			return writeError(ctx, err)
		}
	}
	return nil
}
func providerSessionSummaryTopic(id string) string { return "provider-session-summary-" + id }
func boundedHandoff(id, content string) string {
	value := "UNTRUSTED DATA\nprior_completed_summary=" + id + "\n" + content
	runes := []rune(value)
	if len(runes) > 4096 {
		runes = runes[:4096]
	}
	return string(runes)
}
