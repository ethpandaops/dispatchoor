package store

import (
	"context"
	"time"
)

// Store defines the interface for database operations.
type Store interface {
	// Lifecycle.
	Start(ctx context.Context) error
	Stop() error

	// Health check.
	Ping(ctx context.Context) error

	// Groups.
	CreateGroup(ctx context.Context, group *Group) error
	GetGroup(ctx context.Context, id string) (*Group, error)
	ListGroups(ctx context.Context) ([]*Group, error)
	UpdateGroup(ctx context.Context, group *Group) error
	DeleteGroup(ctx context.Context, id string) error

	// Job Templates.
	CreateJobTemplate(ctx context.Context, template *JobTemplate) error
	GetJobTemplate(ctx context.Context, id string) (*JobTemplate, error)
	ListJobTemplatesByGroup(ctx context.Context, groupID string) ([]*JobTemplate, error)
	UpdateJobTemplate(ctx context.Context, template *JobTemplate) error
	DeleteJobTemplate(ctx context.Context, id string) error
	DeleteJobTemplatesByGroup(ctx context.Context, groupID string) error
	UpdateTemplateInConfig(ctx context.Context, id string, inConfig bool) error
	HasAnyJobs(ctx context.Context, templateID string) (bool, error)

	// Jobs.
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	ListJobsByGroup(ctx context.Context, groupID string, statuses ...JobStatus) ([]*Job, error)
	ListJobsByStatus(ctx context.Context, statuses ...JobStatus) ([]*Job, error)
	ListJobHistory(ctx context.Context, opts HistoryQueryOpts) (*HistoryResult, error)
	GetHistoryStats(ctx context.Context, opts HistoryStatsOpts) (*HistoryStatsResult, error)
	GetHistoryTimeBounds(ctx context.Context, groupID string) (oldest, newest *time.Time, err error)
	UpdateJob(ctx context.Context, job *Job) error
	DeleteJob(ctx context.Context, id string) error
	DeleteOldJobs(ctx context.Context, olderThan time.Time) (int64, error)
	ReorderJobs(ctx context.Context, groupID string, jobIDs []string) error
	GetNextPendingJob(ctx context.Context, groupID string) (*Job, error)
	GetMaxPosition(ctx context.Context, groupID string) (int, error)

	// Runners.
	UpsertRunner(ctx context.Context, runner *Runner) error
	GetRunner(ctx context.Context, id int64) (*Runner, error)
	GetRunnerByName(ctx context.Context, name string) (*Runner, error)
	ListRunners(ctx context.Context) ([]*Runner, error)
	ListRunnersByLabels(ctx context.Context, labels []string) ([]*Runner, error)
	DeleteRunner(ctx context.Context, id int64) error
	DeleteStaleRunners(ctx context.Context, olderThan time.Time) error

	// Users.
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	GetUserByGitHubID(ctx context.Context, githubID string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, id string) error

	// Sessions.
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	GetSessionByToken(ctx context.Context, tokenHash string) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context) error
	DeleteUserSessions(ctx context.Context, userID string) error

	// Audit.
	CreateAuditEntry(ctx context.Context, entry *AuditEntry) error
	ListAuditEntries(ctx context.Context, opts AuditQueryOpts) ([]*AuditEntry, int, error)

	// Migrations.
	Migrate(ctx context.Context) error
}

// Group represents a runner pool.
type Group struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	RunnerLabels []string  `json:"runner_labels"`
	Enabled      bool      `json:"enabled"`
	Paused       bool      `json:"paused"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// JobTemplate represents a workflow dispatch job configuration.
type JobTemplate struct {
	ID            string            `json:"id"`
	GroupID       string            `json:"group_id"`
	Name          string            `json:"name"`
	Owner         string            `json:"owner"`
	Repo          string            `json:"repo"`
	WorkflowID    string            `json:"workflow_id"`
	Ref           string            `json:"ref"`
	DefaultInputs map[string]string `json:"default_inputs"`
	Labels        map[string]string `json:"labels"`
	InConfig      bool              `json:"in_config"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// JobStatus represents the state of a job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusTriggered JobStatus = "triggered"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// Job represents a queued or executed workflow dispatch.
type Job struct {
	ID           string            `json:"id"`
	GroupID      string            `json:"group_id"`
	TemplateID   string            `json:"template_id"`
	Priority     int               `json:"priority"`
	Position     int               `json:"position"`
	Status       JobStatus         `json:"status"`
	Paused       bool              `json:"paused"`
	AutoRequeue  bool              `json:"auto_requeue"`
	RequeueLimit *int              `json:"requeue_limit"`
	RequeueCount int               `json:"requeue_count"`
	Inputs       map[string]string `json:"inputs"`
	CreatedBy    string            `json:"created_by"`
	TriggeredAt  *time.Time        `json:"triggered_at"`
	RunID        *int64            `json:"run_id"`
	RunURL       string            `json:"run_url"`
	RunnerID     *int64            `json:"runner_id"`
	RunnerName   string            `json:"runner_name"`
	CompletedAt  *time.Time        `json:"completed_at"`
	ErrorMessage string            `json:"error_message"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`

	// Override fields (nil/empty means use template value).
	Name       *string           `json:"name,omitempty"`
	Owner      *string           `json:"owner,omitempty"`
	Repo       *string           `json:"repo,omitempty"`
	WorkflowID *string           `json:"workflow_id,omitempty"`
	Ref        *string           `json:"ref,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// RunnerStatus represents the status of a GitHub Actions runner.
type RunnerStatus string

const (
	RunnerStatusOnline  RunnerStatus = "online"
	RunnerStatusOffline RunnerStatus = "offline"
)

// Runner represents a GitHub Actions runner.
type Runner struct {
	ID         int64        `json:"id"`
	Name       string       `json:"name"`
	Labels     []string     `json:"labels"`
	Status     RunnerStatus `json:"status"`
	Busy       bool         `json:"busy"`
	OS         string       `json:"os"`
	LastSeenAt time.Time    `json:"last_seen_at"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// AuthProvider represents the authentication provider for a user.
type AuthProvider string

const (
	AuthProviderBasic  AuthProvider = "basic"
	AuthProviderGitHub AuthProvider = "github"
)

// Role represents a user's access level.
type Role string

const (
	RoleReadOnly Role = "readonly"
	RoleAdmin    Role = "admin"
)

// User represents a user account.
type User struct {
	ID           string       `json:"id"`
	Username     string       `json:"username"`
	PasswordHash string       `json:"-"`
	Role         Role         `json:"role"`
	AuthProvider AuthProvider `json:"auth_provider"`
	GitHubID     string       `json:"github_id,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Session represents an active user session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditAction represents the type of action being audited.
type AuditAction string

const (
	AuditActionJobCreated   AuditAction = "job_created"
	AuditActionJobTriggered AuditAction = "job_triggered"
	AuditActionJobCompleted AuditAction = "job_completed"
	AuditActionJobFailed    AuditAction = "job_failed"
	AuditActionJobCancelled AuditAction = "job_cancelled"
	AuditActionJobReordered AuditAction = "job_reordered"
	AuditActionUserLogin    AuditAction = "user_login"
	AuditActionUserLogout   AuditAction = "user_logout"
	AuditActionConfigReload AuditAction = "config_reload"
)

// AuditEntityType represents the type of entity being audited.
type AuditEntityType string

const (
	AuditEntityJob     AuditEntityType = "job"
	AuditEntityGroup   AuditEntityType = "group"
	AuditEntityRunner  AuditEntityType = "runner"
	AuditEntityUser    AuditEntityType = "user"
	AuditEntitySession AuditEntityType = "session"
	AuditEntitySystem  AuditEntityType = "system"
)

// AuditEntry represents an audit log entry.
type AuditEntry struct {
	ID         string          `json:"id"`
	Action     AuditAction     `json:"action"`
	EntityType AuditEntityType `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Actor      string          `json:"actor"`
	Details    string          `json:"details"`
	CreatedAt  time.Time       `json:"created_at"`
}

// AuditQueryOpts contains options for querying audit entries.
type AuditQueryOpts struct {
	EntityType *AuditEntityType
	EntityID   *string
	Action     *AuditAction
	Actor      *string
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// HistoryQueryOpts contains options for querying job history.
type HistoryQueryOpts struct {
	GroupID  string
	Limit    int
	Before   *time.Time        // cursor: fetch jobs completed before this time
	Statuses []JobStatus       // filter by status (multi-select, empty = all history statuses)
	Labels   map[string]string // filter by template labels (AND logic)
}

// HistoryResult contains paginated history results.
type HistoryResult struct {
	Jobs       []*Job
	HasMore    bool
	NextCursor *time.Time // completed_at of the last job
	TotalCount int
}

// HistoryStatsOpts contains options for querying history statistics.
type HistoryStatsOpts struct {
	GroupID string
	Start   time.Time
	End     time.Time
	Buckets int // number of time buckets to return
}

// HistoryStatsBucket contains aggregated job counts for a time bucket.
type HistoryStatsBucket struct {
	Timestamp time.Time `json:"timestamp"`
	Completed int       `json:"completed"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
}

// HistoryStatsRange contains metadata about the time range.
type HistoryStatsRange struct {
	Start          time.Time     `json:"start"`
	End            time.Time     `json:"end"`
	BucketDuration time.Duration `json:"bucket_duration"`
}

// HistoryStatsTotals contains total counts across all buckets.
type HistoryStatsTotals struct {
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}

// HistoryStatsResult contains aggregated history statistics.
type HistoryStatsResult struct {
	Buckets []*HistoryStatsBucket `json:"buckets"`
	Range   HistoryStatsRange     `json:"range"`
	Totals  HistoryStatsTotals    `json:"totals"`
}
