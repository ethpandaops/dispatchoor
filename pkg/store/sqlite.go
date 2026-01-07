package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sirupsen/logrus"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	log  logrus.FieldLogger
	path string
	db   *sql.DB
}

// Ensure SQLiteStore implements Store.
var _ Store = (*SQLiteStore)(nil)

// NewSQLiteStore creates a new SQLite store.
func NewSQLiteStore(log logrus.FieldLogger, path string) Store {
	return &SQLiteStore{
		log:  log.WithField("component", "store"),
		path: path,
	}
}

// Start opens the database connection.
func (s *SQLiteStore) Start(ctx context.Context) error {
	s.log.WithField("path", s.path).Info("Opening SQLite database")

	db, err := sql.Open("sqlite3", s.path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}

	// Test connection.
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	s.db = db

	return nil
}

// Stop closes the database connection.
func (s *SQLiteStore) Stop() error {
	if s.db != nil {
		return s.db.Close()
	}

	return nil
}

// Migrate runs database migrations.
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	s.log.Info("Running database migrations")

	migrations := []string{
		// Groups table.
		`CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			runner_labels TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Job templates table.
		`CREATE TABLE IF NOT EXISTS job_templates (
			id TEXT PRIMARY KEY,
			group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			owner TEXT NOT NULL,
			repo TEXT NOT NULL,
			workflow_id TEXT NOT NULL,
			ref TEXT NOT NULL DEFAULT 'main',
			default_inputs TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Jobs table.
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			template_id TEXT NOT NULL REFERENCES job_templates(id) ON DELETE CASCADE,
			priority INTEGER DEFAULT 0,
			position INTEGER NOT NULL,
			status TEXT NOT NULL,
			inputs TEXT,
			created_by TEXT,
			triggered_at TIMESTAMP,
			run_id INTEGER,
			run_url TEXT,
			runner_name TEXT,
			completed_at TIMESTAMP,
			error_message TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_group_status ON jobs(group_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`,
		// Runners table.
		`CREATE TABLE IF NOT EXISTS runners (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			labels TEXT NOT NULL,
			status TEXT NOT NULL,
			busy INTEGER DEFAULT 0,
			os TEXT,
			last_seen_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		// Users table.
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT,
			role TEXT NOT NULL DEFAULT 'readonly',
			auth_provider TEXT NOT NULL,
			github_id TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_github_id ON users(github_id)`,
		// Sessions table.
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash)`,
		// Audit log table.
		`CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			actor TEXT,
			details TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log(entity_type, entity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at)`,
		// Migration: Add paused column to jobs table.
		`ALTER TABLE jobs ADD COLUMN paused INTEGER DEFAULT 0`,
		// Migration: Add auto-requeue columns to jobs table.
		`ALTER TABLE jobs ADD COLUMN auto_requeue INTEGER DEFAULT 0`,
		`ALTER TABLE jobs ADD COLUMN requeue_limit INTEGER`,
		`ALTER TABLE jobs ADD COLUMN requeue_count INTEGER DEFAULT 0`,
		// Index for efficient history cleanup and pagination.
		`CREATE INDEX IF NOT EXISTS idx_jobs_completed_at ON jobs(completed_at)`,
		// Migration: Add labels column to job_templates table.
		`ALTER TABLE job_templates ADD COLUMN labels TEXT`,
		// Migration: Add in_config column to job_templates table.
		`ALTER TABLE job_templates ADD COLUMN in_config INTEGER DEFAULT 1`,
	}

	for _, migration := range migrations {
		if _, err := s.db.ExecContext(ctx, migration); err != nil {
			// Ignore "duplicate column" errors for ALTER TABLE migrations.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}

			return fmt.Errorf("running migration: %w", err)
		}
	}

	return nil
}

// ============================================================================
// Groups
// ============================================================================

// CreateGroup creates a new group.
func (s *SQLiteStore) CreateGroup(ctx context.Context, group *Group) error {
	labelsJSON, err := json.Marshal(group.RunnerLabels)
	if err != nil {
		return fmt.Errorf("marshaling runner_labels: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO groups (id, name, description, runner_labels, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, group.ID, group.Name, group.Description, string(labelsJSON),
		group.Enabled, group.CreatedAt, group.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting group: %w", err)
	}

	return nil
}

// GetGroup retrieves a group by ID.
func (s *SQLiteStore) GetGroup(ctx context.Context, id string) (*Group, error) {
	var group Group

	var labelsJSON string

	var enabled int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, runner_labels, enabled, created_at, updated_at
		FROM groups WHERE id = ?
	`, id).Scan(&group.ID, &group.Name, &group.Description, &labelsJSON,
		&enabled, &group.CreatedAt, &group.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying group: %w", err)
	}

	if err := json.Unmarshal([]byte(labelsJSON), &group.RunnerLabels); err != nil {
		return nil, fmt.Errorf("unmarshaling runner_labels: %w", err)
	}

	group.Enabled = enabled == 1

	return &group, nil
}

// ListGroups retrieves all groups.
func (s *SQLiteStore) ListGroups(ctx context.Context) ([]*Group, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, runner_labels, enabled, created_at, updated_at
		FROM groups ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying groups: %w", err)
	}

	defer rows.Close()

	var groups []*Group

	for rows.Next() {
		var group Group

		var labelsJSON string

		var enabled int

		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &labelsJSON,
			&enabled, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning group: %w", err)
		}

		if err := json.Unmarshal([]byte(labelsJSON), &group.RunnerLabels); err != nil {
			return nil, fmt.Errorf("unmarshaling runner_labels: %w", err)
		}

		group.Enabled = enabled == 1
		groups = append(groups, &group)
	}

	return groups, rows.Err()
}

// UpdateGroup updates an existing group.
func (s *SQLiteStore) UpdateGroup(ctx context.Context, group *Group) error {
	labelsJSON, err := json.Marshal(group.RunnerLabels)
	if err != nil {
		return fmt.Errorf("marshaling runner_labels: %w", err)
	}

	group.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE groups SET name = ?, description = ?, runner_labels = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, group.Name, group.Description, string(labelsJSON), group.Enabled, group.UpdatedAt, group.ID)

	if err != nil {
		return fmt.Errorf("updating group: %w", err)
	}

	return nil
}

// DeleteGroup deletes a group by ID.
func (s *SQLiteStore) DeleteGroup(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting group: %w", err)
	}

	return nil
}

// ============================================================================
// Job Templates
// ============================================================================

// CreateJobTemplate creates a new job template.
func (s *SQLiteStore) CreateJobTemplate(ctx context.Context, template *JobTemplate) error {
	inputsJSON, err := json.Marshal(template.DefaultInputs)
	if err != nil {
		return fmt.Errorf("marshaling default_inputs: %w", err)
	}

	labelsJSON, err := json.Marshal(template.Labels)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO job_templates (id, group_id, name, owner, repo, workflow_id, ref, default_inputs, labels, in_config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, template.ID, template.GroupID, template.Name, template.Owner, template.Repo,
		template.WorkflowID, template.Ref, string(inputsJSON), string(labelsJSON), template.InConfig, template.CreatedAt, template.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting job_template: %w", err)
	}

	return nil
}

// GetJobTemplate retrieves a job template by ID.
func (s *SQLiteStore) GetJobTemplate(ctx context.Context, id string) (*JobTemplate, error) {
	var template JobTemplate

	var inputsJSON, labelsJSON sql.NullString

	var inConfig int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, group_id, name, owner, repo, workflow_id, ref, default_inputs, labels, in_config, created_at, updated_at
		FROM job_templates WHERE id = ?
	`, id).Scan(&template.ID, &template.GroupID, &template.Name, &template.Owner,
		&template.Repo, &template.WorkflowID, &template.Ref, &inputsJSON, &labelsJSON,
		&inConfig, &template.CreatedAt, &template.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying job_template: %w", err)
	}

	if inputsJSON.Valid && inputsJSON.String != "" {
		if err := json.Unmarshal([]byte(inputsJSON.String), &template.DefaultInputs); err != nil {
			return nil, fmt.Errorf("unmarshaling default_inputs: %w", err)
		}
	}

	if labelsJSON.Valid && labelsJSON.String != "" {
		if err := json.Unmarshal([]byte(labelsJSON.String), &template.Labels); err != nil {
			return nil, fmt.Errorf("unmarshaling labels: %w", err)
		}
	}

	template.InConfig = inConfig == 1

	return &template, nil
}

// ListJobTemplatesByGroup retrieves all job templates for a group.
func (s *SQLiteStore) ListJobTemplatesByGroup(ctx context.Context, groupID string) ([]*JobTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_id, name, owner, repo, workflow_id, ref, default_inputs, labels, in_config, created_at, updated_at
		FROM job_templates WHERE group_id = ? ORDER BY name
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("querying job_templates: %w", err)
	}

	defer rows.Close()

	var templates []*JobTemplate

	for rows.Next() {
		var template JobTemplate

		var inputsJSON, labelsJSON sql.NullString

		var inConfig int

		if err := rows.Scan(&template.ID, &template.GroupID, &template.Name, &template.Owner,
			&template.Repo, &template.WorkflowID, &template.Ref, &inputsJSON, &labelsJSON,
			&inConfig, &template.CreatedAt, &template.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning job_template: %w", err)
		}

		if inputsJSON.Valid && inputsJSON.String != "" {
			if err := json.Unmarshal([]byte(inputsJSON.String), &template.DefaultInputs); err != nil {
				return nil, fmt.Errorf("unmarshaling default_inputs: %w", err)
			}
		}

		if labelsJSON.Valid && labelsJSON.String != "" {
			if err := json.Unmarshal([]byte(labelsJSON.String), &template.Labels); err != nil {
				return nil, fmt.Errorf("unmarshaling labels: %w", err)
			}
		}

		template.InConfig = inConfig == 1
		templates = append(templates, &template)
	}

	return templates, rows.Err()
}

// UpdateJobTemplate updates an existing job template.
func (s *SQLiteStore) UpdateJobTemplate(ctx context.Context, template *JobTemplate) error {
	inputsJSON, err := json.Marshal(template.DefaultInputs)
	if err != nil {
		return fmt.Errorf("marshaling default_inputs: %w", err)
	}

	labelsJSON, err := json.Marshal(template.Labels)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	template.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE job_templates SET name = ?, owner = ?, repo = ?, workflow_id = ?, ref = ?, default_inputs = ?, labels = ?, in_config = ?, updated_at = ?
		WHERE id = ?
	`, template.Name, template.Owner, template.Repo, template.WorkflowID, template.Ref,
		string(inputsJSON), string(labelsJSON), template.InConfig, template.UpdatedAt, template.ID)

	if err != nil {
		return fmt.Errorf("updating job_template: %w", err)
	}

	return nil
}

// DeleteJobTemplate deletes a job template by ID.
func (s *SQLiteStore) DeleteJobTemplate(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting job_template: %w", err)
	}

	return nil
}

// DeleteJobTemplatesByGroup deletes all job templates for a group.
func (s *SQLiteStore) DeleteJobTemplatesByGroup(ctx context.Context, groupID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_templates WHERE group_id = ?`, groupID)
	if err != nil {
		return fmt.Errorf("deleting job_templates: %w", err)
	}

	return nil
}

// UpdateTemplateInConfig updates the in_config status of a job template.
func (s *SQLiteStore) UpdateTemplateInConfig(ctx context.Context, id string, inConfig bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE job_templates SET in_config = ?, updated_at = ? WHERE id = ?
	`, inConfig, time.Now().UTC(), id)

	if err != nil {
		return fmt.Errorf("updating template in_config: %w", err)
	}

	return nil
}

// HasAnyJobs checks if a template has any jobs (regardless of status).
func (s *SQLiteStore) HasAnyJobs(ctx context.Context, templateID string) (bool, error) {
	var count int

	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM jobs WHERE template_id = ?", templateID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("counting jobs for template: %w", err)
	}

	return count > 0, nil
}

// ============================================================================
// Jobs
// ============================================================================

// CreateJob creates a new job.
func (s *SQLiteStore) CreateJob(ctx context.Context, job *Job) error {
	inputsJSON, err := json.Marshal(job.Inputs)
	if err != nil {
		return fmt.Errorf("marshaling inputs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, group_id, template_id, priority, position, status, paused, auto_requeue, requeue_limit, requeue_count, inputs, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.GroupID, job.TemplateID, job.Priority, job.Position, job.Status, job.Paused,
		job.AutoRequeue, job.RequeueLimit, job.RequeueCount, string(inputsJSON), job.CreatedBy, job.CreatedAt, job.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting job: %w", err)
	}

	return nil
}

// GetJob retrieves a job by ID.
func (s *SQLiteStore) GetJob(ctx context.Context, id string) (*Job, error) {
	var job Job

	var inputsJSON sql.NullString

	var triggeredAt, completedAt sql.NullTime

	var runID, requeueLimit sql.NullInt64

	var paused, autoRequeue int

	var runURL, runnerName, errorMessage, createdBy sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, group_id, template_id, priority, position, status, paused, auto_requeue, requeue_limit, requeue_count, inputs, created_by,
			   triggered_at, run_id, run_url, runner_name, completed_at, error_message, created_at, updated_at
		FROM jobs WHERE id = ?
	`, id).Scan(&job.ID, &job.GroupID, &job.TemplateID, &job.Priority, &job.Position, &job.Status,
		&paused, &autoRequeue, &requeueLimit, &job.RequeueCount, &inputsJSON, &createdBy, &triggeredAt, &runID, &runURL, &runnerName, &completedAt,
		&errorMessage, &job.CreatedAt, &job.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying job: %w", err)
	}

	if inputsJSON.Valid && inputsJSON.String != "" {
		if err := json.Unmarshal([]byte(inputsJSON.String), &job.Inputs); err != nil {
			return nil, fmt.Errorf("unmarshaling inputs: %w", err)
		}
	}

	if triggeredAt.Valid {
		job.TriggeredAt = &triggeredAt.Time
	}

	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	if runID.Valid {
		job.RunID = &runID.Int64
	}

	job.Paused = paused == 1
	job.AutoRequeue = autoRequeue == 1

	if requeueLimit.Valid {
		limit := int(requeueLimit.Int64)
		job.RequeueLimit = &limit
	}

	job.RunURL = runURL.String
	job.RunnerName = runnerName.String
	job.ErrorMessage = errorMessage.String
	job.CreatedBy = createdBy.String

	return &job, nil
}

// ListJobsByGroup retrieves jobs for a group, optionally filtered by status.
func (s *SQLiteStore) ListJobsByGroup(
	ctx context.Context, groupID string, statuses ...JobStatus,
) ([]*Job, error) {
	query := `
		SELECT id, group_id, template_id, priority, position, status, paused, auto_requeue, requeue_limit, requeue_count, inputs, created_by,
			   triggered_at, run_id, run_url, runner_name, completed_at, error_message, created_at, updated_at
		FROM jobs WHERE group_id = ?
	`

	args := []any{groupID}

	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, status := range statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}

		query += fmt.Sprintf(" AND status IN (%s)", strings.Join(placeholders, ","))
	}

	// Order: running/triggered jobs first by triggered_at, then history jobs by completed_at desc,
	// then pending jobs by position.
	query += ` ORDER BY
		CASE WHEN status IN ('triggered', 'running') THEN 0 ELSE 1 END,
		CASE WHEN status IN ('triggered', 'running') THEN triggered_at END,
		CASE WHEN status IN ('completed', 'failed', 'cancelled') THEN completed_at END DESC,
		position`

	return s.queryJobs(ctx, query, args...)
}

// ListJobsByStatus retrieves all jobs with the given statuses.
func (s *SQLiteStore) ListJobsByStatus(ctx context.Context, statuses ...JobStatus) ([]*Job, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))

	for i, status := range statuses {
		placeholders[i] = "?"
		args[i] = status
	}

	query := fmt.Sprintf(`
		SELECT id, group_id, template_id, priority, position, status, paused, auto_requeue, requeue_limit, requeue_count, inputs, created_by,
			   triggered_at, run_id, run_url, runner_name, completed_at, error_message, created_at, updated_at
		FROM jobs WHERE status IN (%s) ORDER BY position
	`, strings.Join(placeholders, ","))

	return s.queryJobs(ctx, query, args...)
}

func (s *SQLiteStore) queryJobs(ctx context.Context, query string, args ...any) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying jobs: %w", err)
	}

	defer rows.Close()

	var jobs []*Job

	for rows.Next() {
		var job Job

		var inputsJSON sql.NullString

		var triggeredAt, completedAt sql.NullTime

		var runID, requeueLimit sql.NullInt64

		var paused, autoRequeue int

		var runURL, runnerName, errorMessage, createdBy sql.NullString

		if err := rows.Scan(&job.ID, &job.GroupID, &job.TemplateID, &job.Priority, &job.Position,
			&job.Status, &paused, &autoRequeue, &requeueLimit, &job.RequeueCount, &inputsJSON, &createdBy, &triggeredAt, &runID, &runURL, &runnerName,
			&completedAt, &errorMessage, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning job: %w", err)
		}

		if inputsJSON.Valid && inputsJSON.String != "" {
			if err := json.Unmarshal([]byte(inputsJSON.String), &job.Inputs); err != nil {
				return nil, fmt.Errorf("unmarshaling inputs: %w", err)
			}
		}

		if triggeredAt.Valid {
			job.TriggeredAt = &triggeredAt.Time
		}

		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		if runID.Valid {
			job.RunID = &runID.Int64
		}

		job.Paused = paused == 1
		job.AutoRequeue = autoRequeue == 1

		if requeueLimit.Valid {
			limit := int(requeueLimit.Int64)
			job.RequeueLimit = &limit
		}

		job.RunURL = runURL.String
		job.RunnerName = runnerName.String
		job.ErrorMessage = errorMessage.String
		job.CreatedBy = createdBy.String

		jobs = append(jobs, &job)
	}

	return jobs, rows.Err()
}

// UpdateJob updates an existing job.
func (s *SQLiteStore) UpdateJob(ctx context.Context, job *Job) error {
	inputsJSON, err := json.Marshal(job.Inputs)
	if err != nil {
		return fmt.Errorf("marshaling inputs: %w", err)
	}

	job.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE jobs SET priority = ?, position = ?, status = ?, paused = ?, auto_requeue = ?, requeue_limit = ?, requeue_count = ?, inputs = ?,
			   triggered_at = ?, run_id = ?, run_url = ?, runner_name = ?,
			   completed_at = ?, error_message = ?, updated_at = ?
		WHERE id = ?
	`, job.Priority, job.Position, job.Status, job.Paused, job.AutoRequeue, job.RequeueLimit, job.RequeueCount, string(inputsJSON),
		job.TriggeredAt, job.RunID, job.RunURL, job.RunnerName,
		job.CompletedAt, job.ErrorMessage, job.UpdatedAt, job.ID)

	if err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	return nil
}

// DeleteJob deletes a job by ID.
func (s *SQLiteStore) DeleteJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting job: %w", err)
	}

	return nil
}

// DeleteOldJobs deletes completed, failed, or cancelled jobs older than the given time.
func (s *SQLiteStore) DeleteOldJobs(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM jobs
		WHERE status IN ('completed', 'failed', 'cancelled')
		AND completed_at < ?
	`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("deleting old jobs: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting rows affected: %w", err)
	}

	return count, nil
}

// ListJobHistory retrieves paginated job history with cursor-based pagination.
func (s *SQLiteStore) ListJobHistory(ctx context.Context, opts HistoryQueryOpts) (*HistoryResult, error) {
	// Determine which statuses to filter by.
	statuses := opts.Statuses
	if len(statuses) == 0 {
		statuses = []JobStatus{JobStatusCompleted, JobStatusFailed, JobStatusCancelled}
	}

	// Build status placeholders and args.
	statusPlaceholders := make([]string, len(statuses))
	args := []any{opts.GroupID}

	for i, status := range statuses {
		statusPlaceholders[i] = "?"
		args = append(args, status)
	}

	// Check if we need to join with job_templates for label filtering.
	needsJoin := len(opts.Labels) > 0

	query := `
		SELECT j.id, j.group_id, j.template_id, j.priority, j.position, j.status, j.paused, j.auto_requeue, j.requeue_limit, j.requeue_count, j.inputs, j.created_by,
			   j.triggered_at, j.run_id, j.run_url, j.runner_name, j.completed_at, j.error_message, j.created_at, j.updated_at
		FROM jobs j
	`

	if needsJoin {
		query += " JOIN job_templates t ON j.template_id = t.id"
	}

	query += fmt.Sprintf(`
		WHERE j.group_id = ?
		AND j.status IN (%s)
	`, strings.Join(statusPlaceholders, ","))

	// Add label filters using SQLite JSON extraction.
	for key, value := range opts.Labels {
		query += " AND json_extract(t.labels, ?) = ?"
		args = append(args, "$."+key, value)
	}

	if opts.Before != nil {
		query += " AND j.completed_at < ?"
		args = append(args, *opts.Before)
	}

	query += " ORDER BY j.completed_at DESC"

	// Fetch one extra to check if more exist.
	fetchLimit := opts.Limit + 1
	query += fmt.Sprintf(" LIMIT %d", fetchLimit)

	jobs, err := s.queryJobs(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	result := &HistoryResult{
		Jobs:    jobs,
		HasMore: false,
	}

	// Check if we got more than requested (indicates more data exists).
	if len(jobs) > opts.Limit {
		result.HasMore = true
		result.Jobs = jobs[:opts.Limit]
	}

	// Set next cursor to the completed_at of the last job.
	if len(result.Jobs) > 0 {
		lastJob := result.Jobs[len(result.Jobs)-1]
		result.NextCursor = lastJob.CompletedAt
	}

	// Get total count with same filters.
	countQuery := "SELECT COUNT(*) FROM jobs j"
	if needsJoin {
		countQuery += " JOIN job_templates t ON j.template_id = t.id"
	}

	countQuery += fmt.Sprintf(`
		WHERE j.group_id = ?
		AND j.status IN (%s)
	`, strings.Join(statusPlaceholders, ","))

	countArgs := []any{opts.GroupID}
	for _, status := range statuses {
		countArgs = append(countArgs, status)
	}

	for key, value := range opts.Labels {
		countQuery += " AND json_extract(t.labels, ?) = ?"
		countArgs = append(countArgs, "$."+key, value)
	}

	var totalCount int

	err = s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("counting history jobs: %w", err)
	}

	result.TotalCount = totalCount

	return result, nil
}

// ReorderJobs updates job positions based on the provided order.
func (s *SQLiteStore) ReorderJobs(ctx context.Context, groupID string, jobIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	for i, jobID := range jobIDs {
		_, err := tx.ExecContext(ctx, `
			UPDATE jobs SET position = ?, updated_at = ? WHERE id = ? AND group_id = ?
		`, i, time.Now(), jobID, groupID)
		if err != nil {
			return fmt.Errorf("updating job position: %w", err)
		}
	}

	return tx.Commit()
}

// GetNextPendingJob retrieves the next pending job for a group (lowest position).
// Paused jobs are excluded from selection.
func (s *SQLiteStore) GetNextPendingJob(ctx context.Context, groupID string) (*Job, error) {
	jobs, err := s.ListJobsByGroup(ctx, groupID, JobStatusPending)
	if err != nil {
		return nil, err
	}

	// Find first non-paused job.
	for _, job := range jobs {
		if !job.Paused {
			return job, nil
		}
	}

	return nil, nil
}

// GetMaxPosition returns the maximum position for jobs in a group.
func (s *SQLiteStore) GetMaxPosition(ctx context.Context, groupID string) (int, error) {
	var maxPos sql.NullInt64

	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(position) FROM jobs WHERE group_id = ?
	`, groupID).Scan(&maxPos)

	if err != nil {
		return -1, fmt.Errorf("querying max position: %w", err)
	}

	if !maxPos.Valid {
		return -1, nil
	}

	return int(maxPos.Int64), nil
}

// ============================================================================
// Runners
// ============================================================================

// UpsertRunner creates or updates a runner.
func (s *SQLiteStore) UpsertRunner(ctx context.Context, runner *Runner) error {
	labelsJSON, err := json.Marshal(runner.Labels)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO runners (id, name, labels, status, busy, os, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			labels = excluded.labels,
			status = excluded.status,
			busy = excluded.busy,
			os = excluded.os,
			last_seen_at = excluded.last_seen_at,
			updated_at = excluded.updated_at
	`, runner.ID, runner.Name, string(labelsJSON), runner.Status, runner.Busy,
		runner.OS, runner.LastSeenAt, runner.CreatedAt, runner.UpdatedAt)

	if err != nil {
		return fmt.Errorf("upserting runner: %w", err)
	}

	return nil
}

// GetRunner retrieves a runner by ID.
func (s *SQLiteStore) GetRunner(ctx context.Context, id int64) (*Runner, error) {
	var runner Runner

	var labelsJSON string

	var busy int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, labels, status, busy, os, last_seen_at, created_at, updated_at
		FROM runners WHERE id = ?
	`, id).Scan(&runner.ID, &runner.Name, &labelsJSON, &runner.Status, &busy,
		&runner.OS, &runner.LastSeenAt, &runner.CreatedAt, &runner.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying runner: %w", err)
	}

	if err := json.Unmarshal([]byte(labelsJSON), &runner.Labels); err != nil {
		return nil, fmt.Errorf("unmarshaling labels: %w", err)
	}

	runner.Busy = busy == 1

	return &runner, nil
}

// GetRunnerByName retrieves a runner by name.
func (s *SQLiteStore) GetRunnerByName(ctx context.Context, name string) (*Runner, error) {
	var runner Runner

	var labelsJSON string

	var busy int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, labels, status, busy, os, last_seen_at, created_at, updated_at
		FROM runners WHERE name = ?
	`, name).Scan(&runner.ID, &runner.Name, &labelsJSON, &runner.Status, &busy,
		&runner.OS, &runner.LastSeenAt, &runner.CreatedAt, &runner.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying runner by name: %w", err)
	}

	if err := json.Unmarshal([]byte(labelsJSON), &runner.Labels); err != nil {
		return nil, fmt.Errorf("unmarshaling labels: %w", err)
	}

	runner.Busy = busy == 1

	return &runner, nil
}

// ListRunners retrieves all runners.
func (s *SQLiteStore) ListRunners(ctx context.Context) ([]*Runner, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, labels, status, busy, os, last_seen_at, created_at, updated_at
		FROM runners ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying runners: %w", err)
	}

	defer rows.Close()

	return s.scanRunners(rows)
}

// ListRunnersByLabels retrieves runners that have all the specified labels.
func (s *SQLiteStore) ListRunnersByLabels(ctx context.Context, labels []string) ([]*Runner, error) {
	// Get all runners and filter in memory (SQLite JSON support is limited).
	runners, err := s.ListRunners(ctx)
	if err != nil {
		return nil, err
	}

	var matched []*Runner

	for _, runner := range runners {
		if hasAllLabels(runner.Labels, labels) {
			matched = append(matched, runner)
		}
	}

	return matched, nil
}

func hasAllLabels(runnerLabels, requiredLabels []string) bool {
	labelSet := make(map[string]bool, len(runnerLabels))

	for _, label := range runnerLabels {
		labelSet[label] = true
	}

	for _, required := range requiredLabels {
		if !labelSet[required] {
			return false
		}
	}

	return true
}

func (s *SQLiteStore) scanRunners(rows *sql.Rows) ([]*Runner, error) {
	var runners []*Runner

	for rows.Next() {
		var runner Runner

		var labelsJSON string

		var busy int

		if err := rows.Scan(&runner.ID, &runner.Name, &labelsJSON, &runner.Status, &busy,
			&runner.OS, &runner.LastSeenAt, &runner.CreatedAt, &runner.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning runner: %w", err)
		}

		if err := json.Unmarshal([]byte(labelsJSON), &runner.Labels); err != nil {
			return nil, fmt.Errorf("unmarshaling labels: %w", err)
		}

		runner.Busy = busy == 1
		runners = append(runners, &runner)
	}

	return runners, rows.Err()
}

// DeleteRunner deletes a runner by ID.
func (s *SQLiteStore) DeleteRunner(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runners WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting runner: %w", err)
	}

	return nil
}

// DeleteStaleRunners deletes runners not seen since the given time.
func (s *SQLiteStore) DeleteStaleRunners(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runners WHERE last_seen_at < ?`, olderThan)
	if err != nil {
		return fmt.Errorf("deleting stale runners: %w", err)
	}

	return nil
}

// ============================================================================
// Users
// ============================================================================

// CreateUser creates a new user.
func (s *SQLiteStore) CreateUser(ctx context.Context, user *User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, auth_provider, github_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.AuthProvider,
		user.GitHubID, user.CreatedAt, user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting user: %w", err)
	}

	return nil
}

// GetUser retrieves a user by ID.
func (s *SQLiteStore) GetUser(ctx context.Context, id string) (*User, error) {
	var user User

	var passwordHash, githubID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, auth_provider, github_id, created_at, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&user.ID, &user.Username, &passwordHash, &user.Role, &user.AuthProvider,
		&githubID, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying user: %w", err)
	}

	user.PasswordHash = passwordHash.String
	user.GitHubID = githubID.String

	return &user, nil
}

// GetUserByUsername retrieves a user by username.
func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User

	var passwordHash, githubID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, auth_provider, github_id, created_at, updated_at
		FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &passwordHash, &user.Role, &user.AuthProvider,
		&githubID, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying user by username: %w", err)
	}

	user.PasswordHash = passwordHash.String
	user.GitHubID = githubID.String

	return &user, nil
}

// GetUserByGitHubID retrieves a user by GitHub ID.
func (s *SQLiteStore) GetUserByGitHubID(ctx context.Context, githubID string) (*User, error) {
	var user User

	var passwordHash, gid sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, auth_provider, github_id, created_at, updated_at
		FROM users WHERE github_id = ?
	`, githubID).Scan(&user.ID, &user.Username, &passwordHash, &user.Role, &user.AuthProvider,
		&gid, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying user by github_id: %w", err)
	}

	user.PasswordHash = passwordHash.String
	user.GitHubID = gid.String

	return &user, nil
}

// UpdateUser updates an existing user.
func (s *SQLiteStore) UpdateUser(ctx context.Context, user *User) error {
	user.UpdatedAt = time.Now()

	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET username = ?, password_hash = ?, role = ?, github_id = ?, updated_at = ?
		WHERE id = ?
	`, user.Username, user.PasswordHash, user.Role, user.GitHubID, user.UpdatedAt, user.ID)

	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

// DeleteUser deletes a user by ID.
func (s *SQLiteStore) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}

	return nil
}

// ============================================================================
// Sessions
// ============================================================================

// CreateSession creates a new session.
func (s *SQLiteStore) CreateSession(ctx context.Context, session *Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt)

	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID.
func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*Session, error) {
	var session Session

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE id = ?
	`, id).Scan(&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying session: %w", err)
	}

	return &session, nil
}

// GetSessionByToken retrieves a session by token hash.
func (s *SQLiteStore) GetSessionByToken(ctx context.Context, tokenHash string) (*Session, error) {
	var session Session

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE token_hash = ?
	`, tokenHash).Scan(&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying session by token: %w", err)
	}

	return &session, nil
}

// DeleteSession deletes a session by ID.
func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return nil
}

// DeleteExpiredSessions deletes all expired sessions.
func (s *SQLiteStore) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now())
	if err != nil {
		return fmt.Errorf("deleting expired sessions: %w", err)
	}

	return nil
}

// DeleteUserSessions deletes all sessions for a user.
func (s *SQLiteStore) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("deleting user sessions: %w", err)
	}

	return nil
}

// ============================================================================
// Audit
// ============================================================================

// CreateAuditEntry creates a new audit log entry.
func (s *SQLiteStore) CreateAuditEntry(ctx context.Context, entry *AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (id, action, entity_type, entity_id, actor, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.Action, entry.EntityType, entry.EntityID, entry.Actor, entry.Details, entry.CreatedAt)

	if err != nil {
		return fmt.Errorf("inserting audit_entry: %w", err)
	}

	return nil
}

// ListAuditEntries retrieves audit entries with filtering and pagination.
func (s *SQLiteStore) ListAuditEntries(
	ctx context.Context, opts AuditQueryOpts,
) ([]*AuditEntry, int, error) {
	query := `SELECT id, action, entity_type, entity_id, actor, details, created_at FROM audit_log WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM audit_log WHERE 1=1`

	var args []any

	if opts.EntityType != nil {
		query += " AND entity_type = ?"
		countQuery += " AND entity_type = ?"

		args = append(args, *opts.EntityType)
	}

	if opts.EntityID != nil {
		query += " AND entity_id = ?"
		countQuery += " AND entity_id = ?"

		args = append(args, *opts.EntityID)
	}

	if opts.Action != nil {
		query += " AND action = ?"
		countQuery += " AND action = ?"

		args = append(args, *opts.Action)
	}

	if opts.Actor != nil {
		query += " AND actor = ?"
		countQuery += " AND actor = ?"

		args = append(args, *opts.Actor)
	}

	if opts.Since != nil {
		query += " AND created_at >= ?"
		countQuery += " AND created_at >= ?"

		args = append(args, *opts.Since)
	}

	if opts.Until != nil {
		query += " AND created_at <= ?"
		countQuery += " AND created_at <= ?"

		args = append(args, *opts.Until)
	}

	// Get total count.
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting audit entries: %w", err)
	}

	// Apply ordering and pagination.
	query += " ORDER BY created_at DESC"

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying audit entries: %w", err)
	}

	defer rows.Close()

	var entries []*AuditEntry

	for rows.Next() {
		var entry AuditEntry

		var actor, details sql.NullString

		if err := rows.Scan(&entry.ID, &entry.Action, &entry.EntityType, &entry.EntityID,
			&actor, &details, &entry.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scanning audit_entry: %w", err)
		}

		entry.Actor = actor.String
		entry.Details = details.String
		entries = append(entries, &entry)
	}

	return entries, total, rows.Err()
}
