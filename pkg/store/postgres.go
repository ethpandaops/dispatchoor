package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// PostgresStore implements Store using PostgreSQL.
type PostgresStore struct {
	log logrus.FieldLogger
	dsn string
	db  *sql.DB
}

// Ensure PostgresStore implements Store.
var _ Store = (*PostgresStore)(nil)

// NewPostgresStore creates a new PostgreSQL store.
func NewPostgresStore(log logrus.FieldLogger, dsn string) Store {
	return &PostgresStore{
		log: log.WithField("component", "store"),
		dsn: dsn,
	}
}

// Start opens the database connection.
func (s *PostgresStore) Start(ctx context.Context) error {
	s.log.Info("Opening PostgreSQL database")

	db, err := sql.Open("postgres", s.dsn)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}

	// Configure connection pool.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection.
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	s.db = db

	return nil
}

// Stop closes the database connection.
func (s *PostgresStore) Stop() error {
	if s.db != nil {
		return s.db.Close()
	}

	return nil
}

// Migrate runs database migrations.
func (s *PostgresStore) Migrate(ctx context.Context) error {
	s.log.Info("Running database migrations")

	migrations := []string{
		// Groups table.
		`CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			runner_labels JSONB NOT NULL,
			enabled BOOLEAN DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
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
			default_inputs JSONB,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		// Jobs table.
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			template_id TEXT NOT NULL REFERENCES job_templates(id) ON DELETE CASCADE,
			priority INTEGER DEFAULT 0,
			position INTEGER NOT NULL,
			status TEXT NOT NULL,
			inputs JSONB,
			created_by TEXT,
			triggered_at TIMESTAMPTZ,
			run_id BIGINT,
			run_url TEXT,
			runner_name TEXT,
			completed_at TIMESTAMPTZ,
			error_message TEXT,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_group_status ON jobs(group_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`,
		// Runners table.
		`CREATE TABLE IF NOT EXISTS runners (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			labels JSONB NOT NULL,
			status TEXT NOT NULL,
			busy BOOLEAN DEFAULT false,
			os TEXT,
			last_seen_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		// Users table.
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT,
			role TEXT NOT NULL DEFAULT 'readonly',
			auth_provider TEXT NOT NULL,
			github_id TEXT,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_github_id ON users(github_id)`,
		// Sessions table.
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
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
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log(entity_type, entity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at)`,
	}

	for _, migration := range migrations {
		if _, err := s.db.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("running migration: %w", err)
		}
	}

	return nil
}

// ============================================================================
// Groups
// ============================================================================

// CreateGroup creates a new group.
func (s *PostgresStore) CreateGroup(ctx context.Context, group *Group) error {
	labelsJSON, err := json.Marshal(group.RunnerLabels)
	if err != nil {
		return fmt.Errorf("marshaling runner_labels: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO groups (id, name, description, runner_labels, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, group.ID, group.Name, group.Description, string(labelsJSON),
		group.Enabled, group.CreatedAt, group.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting group: %w", err)
	}

	return nil
}

// GetGroup retrieves a group by ID.
func (s *PostgresStore) GetGroup(ctx context.Context, id string) (*Group, error) {
	var group Group

	var labelsJSON string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, runner_labels, enabled, created_at, updated_at
		FROM groups WHERE id = $1
	`, id).Scan(&group.ID, &group.Name, &group.Description, &labelsJSON,
		&group.Enabled, &group.CreatedAt, &group.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("querying group: %w", err)
	}

	if err := json.Unmarshal([]byte(labelsJSON), &group.RunnerLabels); err != nil {
		return nil, fmt.Errorf("unmarshaling runner_labels: %w", err)
	}

	return &group, nil
}

// ListGroups retrieves all groups.
func (s *PostgresStore) ListGroups(ctx context.Context) ([]*Group, error) {
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

		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &labelsJSON,
			&group.Enabled, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning group: %w", err)
		}

		if err := json.Unmarshal([]byte(labelsJSON), &group.RunnerLabels); err != nil {
			return nil, fmt.Errorf("unmarshaling runner_labels: %w", err)
		}

		groups = append(groups, &group)
	}

	return groups, rows.Err()
}

// UpdateGroup updates an existing group.
func (s *PostgresStore) UpdateGroup(ctx context.Context, group *Group) error {
	labelsJSON, err := json.Marshal(group.RunnerLabels)
	if err != nil {
		return fmt.Errorf("marshaling runner_labels: %w", err)
	}

	group.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE groups SET name = $1, description = $2, runner_labels = $3, enabled = $4, updated_at = $5
		WHERE id = $6
	`, group.Name, group.Description, string(labelsJSON), group.Enabled, group.UpdatedAt, group.ID)

	if err != nil {
		return fmt.Errorf("updating group: %w", err)
	}

	return nil
}

// DeleteGroup deletes a group by ID.
func (s *PostgresStore) DeleteGroup(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting group: %w", err)
	}

	return nil
}

// ============================================================================
// Job Templates
// ============================================================================

// CreateJobTemplate creates a new job template.
func (s *PostgresStore) CreateJobTemplate(ctx context.Context, template *JobTemplate) error {
	inputsJSON, err := json.Marshal(template.DefaultInputs)
	if err != nil {
		return fmt.Errorf("marshaling default_inputs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO job_templates (id, group_id, name, owner, repo, workflow_id, ref, default_inputs, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, template.ID, template.GroupID, template.Name, template.Owner, template.Repo,
		template.WorkflowID, template.Ref, string(inputsJSON), template.CreatedAt, template.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting job_template: %w", err)
	}

	return nil
}

// GetJobTemplate retrieves a job template by ID.
func (s *PostgresStore) GetJobTemplate(ctx context.Context, id string) (*JobTemplate, error) {
	var template JobTemplate

	var inputsJSON sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, group_id, name, owner, repo, workflow_id, ref, default_inputs, created_at, updated_at
		FROM job_templates WHERE id = $1
	`, id).Scan(&template.ID, &template.GroupID, &template.Name, &template.Owner,
		&template.Repo, &template.WorkflowID, &template.Ref, &inputsJSON,
		&template.CreatedAt, &template.UpdatedAt)

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

	return &template, nil
}

// ListJobTemplatesByGroup retrieves all job templates for a group.
func (s *PostgresStore) ListJobTemplatesByGroup(ctx context.Context, groupID string) ([]*JobTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_id, name, owner, repo, workflow_id, ref, default_inputs, created_at, updated_at
		FROM job_templates WHERE group_id = $1 ORDER BY name
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("querying job_templates: %w", err)
	}

	defer rows.Close()

	var templates []*JobTemplate

	for rows.Next() {
		var template JobTemplate

		var inputsJSON sql.NullString

		if err := rows.Scan(&template.ID, &template.GroupID, &template.Name, &template.Owner,
			&template.Repo, &template.WorkflowID, &template.Ref, &inputsJSON,
			&template.CreatedAt, &template.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning job_template: %w", err)
		}

		if inputsJSON.Valid && inputsJSON.String != "" {
			if err := json.Unmarshal([]byte(inputsJSON.String), &template.DefaultInputs); err != nil {
				return nil, fmt.Errorf("unmarshaling default_inputs: %w", err)
			}
		}

		templates = append(templates, &template)
	}

	return templates, rows.Err()
}

// UpdateJobTemplate updates an existing job template.
func (s *PostgresStore) UpdateJobTemplate(ctx context.Context, template *JobTemplate) error {
	inputsJSON, err := json.Marshal(template.DefaultInputs)
	if err != nil {
		return fmt.Errorf("marshaling default_inputs: %w", err)
	}

	template.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE job_templates SET name = $1, owner = $2, repo = $3, workflow_id = $4, ref = $5, default_inputs = $6, updated_at = $7
		WHERE id = $8
	`, template.Name, template.Owner, template.Repo, template.WorkflowID, template.Ref,
		string(inputsJSON), template.UpdatedAt, template.ID)

	if err != nil {
		return fmt.Errorf("updating job_template: %w", err)
	}

	return nil
}

// DeleteJobTemplate deletes a job template by ID.
func (s *PostgresStore) DeleteJobTemplate(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting job_template: %w", err)
	}

	return nil
}

// DeleteJobTemplatesByGroup deletes all job templates for a group.
func (s *PostgresStore) DeleteJobTemplatesByGroup(ctx context.Context, groupID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_templates WHERE group_id = $1`, groupID)
	if err != nil {
		return fmt.Errorf("deleting job_templates: %w", err)
	}

	return nil
}

// ============================================================================
// Jobs
// ============================================================================

// CreateJob creates a new job.
func (s *PostgresStore) CreateJob(ctx context.Context, job *Job) error {
	inputsJSON, err := json.Marshal(job.Inputs)
	if err != nil {
		return fmt.Errorf("marshaling inputs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, group_id, template_id, priority, position, status, inputs, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, job.ID, job.GroupID, job.TemplateID, job.Priority, job.Position, job.Status,
		string(inputsJSON), job.CreatedBy, job.CreatedAt, job.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting job: %w", err)
	}

	return nil
}

// GetJob retrieves a job by ID.
func (s *PostgresStore) GetJob(ctx context.Context, id string) (*Job, error) {
	var job Job

	var inputsJSON sql.NullString

	var triggeredAt, completedAt sql.NullTime

	var runID sql.NullInt64

	var runURL, runnerName, errorMessage, createdBy sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, group_id, template_id, priority, position, status, inputs, created_by,
			   triggered_at, run_id, run_url, runner_name, completed_at, error_message, created_at, updated_at
		FROM jobs WHERE id = $1
	`, id).Scan(&job.ID, &job.GroupID, &job.TemplateID, &job.Priority, &job.Position, &job.Status,
		&inputsJSON, &createdBy, &triggeredAt, &runID, &runURL, &runnerName, &completedAt,
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

	job.RunURL = runURL.String
	job.RunnerName = runnerName.String
	job.ErrorMessage = errorMessage.String
	job.CreatedBy = createdBy.String

	return &job, nil
}

// ListJobsByGroup retrieves jobs for a group, optionally filtered by status.
func (s *PostgresStore) ListJobsByGroup(
	ctx context.Context, groupID string, statuses ...JobStatus,
) ([]*Job, error) {
	query := `
		SELECT id, group_id, template_id, priority, position, status, inputs, created_by,
			   triggered_at, run_id, run_url, runner_name, completed_at, error_message, created_at, updated_at
		FROM jobs WHERE group_id = $1
	`

	args := []any{groupID}
	paramNum := 2

	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, status := range statuses {
			placeholders[i] = fmt.Sprintf("$%d", paramNum)
			args = append(args, status)
			paramNum++
		}

		query += fmt.Sprintf(" AND status IN (%s)", strings.Join(placeholders, ","))
	}

	query += " ORDER BY position"

	return s.queryJobs(ctx, query, args...)
}

// ListJobsByStatus retrieves all jobs with the given statuses.
func (s *PostgresStore) ListJobsByStatus(ctx context.Context, statuses ...JobStatus) ([]*Job, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))

	for i, status := range statuses {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = status
	}

	query := fmt.Sprintf(`
		SELECT id, group_id, template_id, priority, position, status, inputs, created_by,
			   triggered_at, run_id, run_url, runner_name, completed_at, error_message, created_at, updated_at
		FROM jobs WHERE status IN (%s) ORDER BY position
	`, strings.Join(placeholders, ","))

	return s.queryJobs(ctx, query, args...)
}

func (s *PostgresStore) queryJobs(ctx context.Context, query string, args ...any) ([]*Job, error) {
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

		var runID sql.NullInt64

		var runURL, runnerName, errorMessage, createdBy sql.NullString

		if err := rows.Scan(&job.ID, &job.GroupID, &job.TemplateID, &job.Priority, &job.Position,
			&job.Status, &inputsJSON, &createdBy, &triggeredAt, &runID, &runURL, &runnerName,
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

		job.RunURL = runURL.String
		job.RunnerName = runnerName.String
		job.ErrorMessage = errorMessage.String
		job.CreatedBy = createdBy.String

		jobs = append(jobs, &job)
	}

	return jobs, rows.Err()
}

// UpdateJob updates an existing job.
func (s *PostgresStore) UpdateJob(ctx context.Context, job *Job) error {
	inputsJSON, err := json.Marshal(job.Inputs)
	if err != nil {
		return fmt.Errorf("marshaling inputs: %w", err)
	}

	job.UpdatedAt = time.Now()

	_, err = s.db.ExecContext(ctx, `
		UPDATE jobs SET priority = $1, position = $2, status = $3, inputs = $4,
			   triggered_at = $5, run_id = $6, run_url = $7, runner_name = $8,
			   completed_at = $9, error_message = $10, updated_at = $11
		WHERE id = $12
	`, job.Priority, job.Position, job.Status, string(inputsJSON),
		job.TriggeredAt, job.RunID, job.RunURL, job.RunnerName,
		job.CompletedAt, job.ErrorMessage, job.UpdatedAt, job.ID)

	if err != nil {
		return fmt.Errorf("updating job: %w", err)
	}

	return nil
}

// DeleteJob deletes a job by ID.
func (s *PostgresStore) DeleteJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting job: %w", err)
	}

	return nil
}

// ReorderJobs updates job positions based on the provided order.
func (s *PostgresStore) ReorderJobs(ctx context.Context, groupID string, jobIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	for i, jobID := range jobIDs {
		_, err := tx.ExecContext(ctx, `
			UPDATE jobs SET position = $1, updated_at = $2 WHERE id = $3 AND group_id = $4
		`, i, time.Now(), jobID, groupID)
		if err != nil {
			return fmt.Errorf("updating job position: %w", err)
		}
	}

	return tx.Commit()
}

// GetNextPendingJob retrieves the next pending job for a group (lowest position).
func (s *PostgresStore) GetNextPendingJob(ctx context.Context, groupID string) (*Job, error) {
	jobs, err := s.ListJobsByGroup(ctx, groupID, JobStatusPending)
	if err != nil {
		return nil, err
	}

	if len(jobs) == 0 {
		return nil, nil
	}

	return jobs[0], nil
}

// GetMaxPosition returns the maximum position for jobs in a group.
func (s *PostgresStore) GetMaxPosition(ctx context.Context, groupID string) (int, error) {
	var maxPos sql.NullInt64

	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(position) FROM jobs WHERE group_id = $1
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
func (s *PostgresStore) UpsertRunner(ctx context.Context, runner *Runner) error {
	labelsJSON, err := json.Marshal(runner.Labels)
	if err != nil {
		return fmt.Errorf("marshaling labels: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO runners (id, name, labels, status, busy, os, last_seen_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT(id) DO UPDATE SET
			name = EXCLUDED.name,
			labels = EXCLUDED.labels,
			status = EXCLUDED.status,
			busy = EXCLUDED.busy,
			os = EXCLUDED.os,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, runner.ID, runner.Name, string(labelsJSON), runner.Status, runner.Busy,
		runner.OS, runner.LastSeenAt, runner.CreatedAt, runner.UpdatedAt)

	if err != nil {
		return fmt.Errorf("upserting runner: %w", err)
	}

	return nil
}

// GetRunner retrieves a runner by ID.
func (s *PostgresStore) GetRunner(ctx context.Context, id int64) (*Runner, error) {
	var runner Runner

	var labelsJSON string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, labels, status, busy, os, last_seen_at, created_at, updated_at
		FROM runners WHERE id = $1
	`, id).Scan(&runner.ID, &runner.Name, &labelsJSON, &runner.Status, &runner.Busy,
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

	return &runner, nil
}

// ListRunners retrieves all runners.
func (s *PostgresStore) ListRunners(ctx context.Context) ([]*Runner, error) {
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
func (s *PostgresStore) ListRunnersByLabels(ctx context.Context, labels []string) ([]*Runner, error) {
	// Use PostgreSQL's JSONB containment operator for efficient label matching.
	if len(labels) == 0 {
		return s.ListRunners(ctx)
	}

	// Build query using JSONB contains for each label.
	query := `
		SELECT id, name, labels, status, busy, os, last_seen_at, created_at, updated_at
		FROM runners WHERE `

	conditions := make([]string, len(labels))
	args := make([]any, len(labels))

	for i, label := range labels {
		conditions[i] = fmt.Sprintf("labels @> $%d", i+1)
		labelJSON, _ := json.Marshal([]string{label})
		args[i] = string(labelJSON)
	}

	query += strings.Join(conditions, " AND ") + " ORDER BY name"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying runners by labels: %w", err)
	}

	defer rows.Close()

	return s.scanRunners(rows)
}

func (s *PostgresStore) scanRunners(rows *sql.Rows) ([]*Runner, error) {
	var runners []*Runner

	for rows.Next() {
		var runner Runner

		var labelsJSON string

		if err := rows.Scan(&runner.ID, &runner.Name, &labelsJSON, &runner.Status, &runner.Busy,
			&runner.OS, &runner.LastSeenAt, &runner.CreatedAt, &runner.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning runner: %w", err)
		}

		if err := json.Unmarshal([]byte(labelsJSON), &runner.Labels); err != nil {
			return nil, fmt.Errorf("unmarshaling labels: %w", err)
		}

		runners = append(runners, &runner)
	}

	return runners, rows.Err()
}

// DeleteRunner deletes a runner by ID.
func (s *PostgresStore) DeleteRunner(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runners WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting runner: %w", err)
	}

	return nil
}

// DeleteStaleRunners deletes runners not seen since the given time.
func (s *PostgresStore) DeleteStaleRunners(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runners WHERE last_seen_at < $1`, olderThan)
	if err != nil {
		return fmt.Errorf("deleting stale runners: %w", err)
	}

	return nil
}

// ============================================================================
// Users
// ============================================================================

// CreateUser creates a new user.
func (s *PostgresStore) CreateUser(ctx context.Context, user *User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, auth_provider, github_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, user.ID, user.Username, user.PasswordHash, user.Role, user.AuthProvider,
		user.GitHubID, user.CreatedAt, user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("inserting user: %w", err)
	}

	return nil
}

// GetUser retrieves a user by ID.
func (s *PostgresStore) GetUser(ctx context.Context, id string) (*User, error) {
	var user User

	var passwordHash, githubID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, auth_provider, github_id, created_at, updated_at
		FROM users WHERE id = $1
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
func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User

	var passwordHash, githubID sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, auth_provider, github_id, created_at, updated_at
		FROM users WHERE username = $1
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
func (s *PostgresStore) GetUserByGitHubID(ctx context.Context, githubID string) (*User, error) {
	var user User

	var passwordHash, gid sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, role, auth_provider, github_id, created_at, updated_at
		FROM users WHERE github_id = $1
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
func (s *PostgresStore) UpdateUser(ctx context.Context, user *User) error {
	user.UpdatedAt = time.Now()

	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET username = $1, password_hash = $2, role = $3, github_id = $4, updated_at = $5
		WHERE id = $6
	`, user.Username, user.PasswordHash, user.Role, user.GitHubID, user.UpdatedAt, user.ID)

	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

// DeleteUser deletes a user by ID.
func (s *PostgresStore) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}

	return nil
}

// ============================================================================
// Sessions
// ============================================================================

// CreateSession creates a new session.
func (s *PostgresStore) CreateSession(ctx context.Context, session *Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt)

	if err != nil {
		return fmt.Errorf("inserting session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID.
func (s *PostgresStore) GetSession(ctx context.Context, id string) (*Session, error) {
	var session Session

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE id = $1
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
func (s *PostgresStore) GetSessionByToken(ctx context.Context, tokenHash string) (*Session, error) {
	var session Session

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions WHERE token_hash = $1
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
func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return nil
}

// DeleteExpiredSessions deletes all expired sessions.
func (s *PostgresStore) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < $1`, time.Now())
	if err != nil {
		return fmt.Errorf("deleting expired sessions: %w", err)
	}

	return nil
}

// DeleteUserSessions deletes all sessions for a user.
func (s *PostgresStore) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("deleting user sessions: %w", err)
	}

	return nil
}

// ============================================================================
// Audit
// ============================================================================

// CreateAuditEntry creates a new audit log entry.
func (s *PostgresStore) CreateAuditEntry(ctx context.Context, entry *AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (id, action, entity_type, entity_id, actor, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, entry.ID, entry.Action, entry.EntityType, entry.EntityID, entry.Actor, entry.Details, entry.CreatedAt)

	if err != nil {
		return fmt.Errorf("inserting audit_entry: %w", err)
	}

	return nil
}

// ListAuditEntries retrieves audit entries with filtering and pagination.
func (s *PostgresStore) ListAuditEntries(
	ctx context.Context, opts AuditQueryOpts,
) ([]*AuditEntry, int, error) {
	query := `SELECT id, action, entity_type, entity_id, actor, details, created_at FROM audit_log WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM audit_log WHERE 1=1`

	var args []any
	paramNum := 1

	if opts.EntityType != nil {
		query += fmt.Sprintf(" AND entity_type = $%d", paramNum)
		countQuery += fmt.Sprintf(" AND entity_type = $%d", paramNum)

		args = append(args, *opts.EntityType)
		paramNum++
	}

	if opts.EntityID != nil {
		query += fmt.Sprintf(" AND entity_id = $%d", paramNum)
		countQuery += fmt.Sprintf(" AND entity_id = $%d", paramNum)

		args = append(args, *opts.EntityID)
		paramNum++
	}

	if opts.Action != nil {
		query += fmt.Sprintf(" AND action = $%d", paramNum)
		countQuery += fmt.Sprintf(" AND action = $%d", paramNum)

		args = append(args, *opts.Action)
		paramNum++
	}

	if opts.Actor != nil {
		query += fmt.Sprintf(" AND actor = $%d", paramNum)
		countQuery += fmt.Sprintf(" AND actor = $%d", paramNum)

		args = append(args, *opts.Actor)
		paramNum++
	}

	if opts.Since != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", paramNum)
		countQuery += fmt.Sprintf(" AND created_at >= $%d", paramNum)

		args = append(args, *opts.Since)
		paramNum++
	}

	if opts.Until != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", paramNum)
		countQuery += fmt.Sprintf(" AND created_at <= $%d", paramNum)

		args = append(args, *opts.Until)
		paramNum++
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
