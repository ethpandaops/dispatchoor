package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/auth"
	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/github"
	"github.com/ethpandaops/dispatchoor/pkg/metrics"
	"github.com/ethpandaops/dispatchoor/pkg/queue"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
)

// stubQueue implements queue.Service with no-op methods for testing.
type stubQueue struct{}

func (q *stubQueue) Start(context.Context) error                  { return nil }
func (q *stubQueue) Stop() error                                  { return nil }
func (q *stubQueue) SetJobChangeCallback(queue.JobChangeCallback) {}
func (q *stubQueue) Enqueue(context.Context, string, string, string, map[string]string, *queue.EnqueueOptions) (*store.Job, error) {
	return nil, nil
}
func (q *stubQueue) Dequeue(context.Context, string) (*store.Job, error) { return nil, nil }
func (q *stubQueue) Peek(context.Context, string) (*store.Job, error)    { return nil, nil }
func (q *stubQueue) Remove(context.Context, string) error                { return nil }
func (q *stubQueue) Reorder(context.Context, string, []string) error     { return nil }
func (q *stubQueue) GetJob(context.Context, string) (*store.Job, error)  { return nil, nil }
func (q *stubQueue) ListPending(context.Context, string) ([]*store.Job, error) {
	return nil, nil
}
func (q *stubQueue) ListByStatus(context.Context, string, ...store.JobStatus) ([]*store.Job, error) {
	return nil, nil
}
func (q *stubQueue) ListHistory(context.Context, string, int) ([]*store.Job, error) {
	return nil, nil
}
func (q *stubQueue) ListHistoryPaginated(context.Context, store.HistoryQueryOpts) (*store.HistoryResult, error) {
	return nil, nil
}
func (q *stubQueue) MarkTriggered(context.Context, string, int64, string) error { return nil }
func (q *stubQueue) MarkRunning(context.Context, string, int64, string) error   { return nil }
func (q *stubQueue) MarkCompleted(context.Context, string) error                { return nil }
func (q *stubQueue) MarkFailed(context.Context, string, string) error           { return nil }
func (q *stubQueue) MarkCancelled(context.Context, string) error                { return nil }
func (q *stubQueue) Pause(context.Context, string) (*store.Job, error)          { return nil, nil }
func (q *stubQueue) Unpause(context.Context, string) (*store.Job, error)        { return nil, nil }
func (q *stubQueue) UpdateInputs(context.Context, string, map[string]string) error {
	return nil
}
func (q *stubQueue) UpdateJob(context.Context, string, *queue.UpdateJobOptions) error {
	return nil
}
func (q *stubQueue) DisableAutoRequeue(context.Context, string) (*store.Job, error) {
	return nil, nil
}
func (q *stubQueue) UpdateAutoRequeue(context.Context, string, bool, *int) (*store.Job, error) {
	return nil, nil
}

// stubAuth implements auth.Service for testing.
type stubAuth struct{}

func (a *stubAuth) Start(context.Context) error { return nil }
func (a *stubAuth) Stop() error                 { return nil }
func (a *stubAuth) AuthenticateBasic(context.Context, string, string) (*store.User, string, error) {
	return nil, "", nil
}
func (a *stubAuth) AuthenticateGitHub(context.Context, string) (*store.User, string, error) {
	return nil, "", nil
}
func (a *stubAuth) ValidateSession(_ context.Context, _ string) (*store.User, error) {
	return &store.User{
		ID:       "test-user-id",
		Username: "testadmin",
		Role:     store.RoleAdmin,
	}, nil
}
func (a *stubAuth) Logout(context.Context, string) error             { return nil }
func (a *stubAuth) HasRole(_ *store.User, _ store.Role) bool         { return true }
func (a *stubAuth) IsAdmin(_ *store.User) bool                       { return true }
func (a *stubAuth) GetGitHubAuthURL(string) string                   { return "" }
func (a *stubAuth) CreateOAuthState(context.Context) (string, error) { return "", nil }
func (a *stubAuth) ValidateOAuthState(context.Context, string) error { return nil }
func (a *stubAuth) CreateAuthCode(context.Context, string) (string, error) {
	return "", nil
}
func (a *stubAuth) ExchangeAuthCode(context.Context, string) (*store.User, string, error) {
	return nil, "", nil
}

// stubGitHubClient implements github.Client for testing.
type stubGitHubClient struct{}

func (c *stubGitHubClient) Start(context.Context) error { return nil }
func (c *stubGitHubClient) Stop() error                 { return nil }
func (c *stubGitHubClient) IsConnected() bool           { return false }
func (c *stubGitHubClient) ConnectionError() string     { return "" }
func (c *stubGitHubClient) ListOrgRunners(context.Context, string) ([]*github.Runner, error) {
	return nil, nil
}
func (c *stubGitHubClient) ListRepoRunners(context.Context, string, string) ([]*github.Runner, error) {
	return nil, nil
}
func (c *stubGitHubClient) TriggerWorkflowDispatch(context.Context, string, string, string, string, map[string]string) error {
	return nil
}
func (c *stubGitHubClient) GetWorkflowRun(context.Context, string, string, int64) (*github.WorkflowRun, error) {
	return nil, nil
}
func (c *stubGitHubClient) ListWorkflowRuns(context.Context, string, string, string, github.ListWorkflowRunsOpts) ([]*github.WorkflowRun, error) {
	return nil, nil
}
func (c *stubGitHubClient) ListWorkflowRunJobs(context.Context, string, string, int64) ([]*github.WorkflowJob, error) {
	return nil, nil
}
func (c *stubGitHubClient) CancelWorkflowRun(context.Context, string, string, int64) error {
	return nil
}
func (c *stubGitHubClient) RateLimitRemaining() int   { return 0 }
func (c *stubGitHubClient) RateLimitReset() time.Time { return time.Time{} }

// Verify interface compliance.
var (
	_ queue.Service = (*stubQueue)(nil)
	_ auth.Service  = (*stubAuth)(nil)
	_ github.Client = (*stubGitHubClient)(nil)
)

// testMetrics is a shared metrics instance to avoid duplicate prometheus registration.
var testMetrics = metrics.New()

// writeTestConfig writes a minimal valid config YAML file and returns the path.
func writeTestConfig(t *testing.T, dir, dbPath string, templates []map[string]any) string {
	t.Helper()

	cfg := map[string]any{
		"server": map[string]any{
			"listen": ":0",
		},
		"database": map[string]any{
			"driver": "sqlite",
			"sqlite": map[string]any{
				"path": dbPath,
			},
		},
		"auth": map[string]any{
			"basic": map[string]any{
				"enabled": true,
				"users": []map[string]any{
					{"username": "admin", "password": "pass", "role": "admin"},
				},
			},
		},
		"groups": map[string]any{
			"github": []map[string]any{
				{
					"id":                          "test-group",
					"name":                        "Test Group",
					"runner_labels":               []string{"self-hosted"},
					"workflow_dispatch_templates": templates,
				},
			},
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Write as YAML-compatible JSON (JSON is valid YAML).
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return cfgPath
}

func TestHandleReloadTemplates(t *testing.T) {
	ctx := context.Background()
	log := logrus.New()
	log.SetOutput(os.Stderr)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create initial config with one template.
	initialTemplates := []map[string]any{
		{
			"id":          "tmpl-1",
			"name":        "Template 1",
			"owner":       "org",
			"repo":        "repo",
			"workflow_id": "build.yml",
			"ref":         "main",
		},
	}
	cfgPath := writeTestConfig(t, tmpDir, dbPath, initialTemplates)

	// Load config and set up store.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	st := store.NewSQLiteStore(log, dbPath)
	if err := st.Start(ctx); err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer func() { _ = st.Stop() }()

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	if err := SyncGroupsFromConfig(ctx, log, st, cfg); err != nil {
		t.Fatalf("Failed to sync groups: %v", err)
	}

	srv := NewServer(log, cfg, cfgPath, st, &stubQueue{}, &stubAuth{},
		&stubGitHubClient{}, &stubGitHubClient{}, testMetrics)

	s := srv.(*server)

	// Verify initial state: one template.
	templates, err := st.ListJobTemplatesByGroup(ctx, "test-group")
	if err != nil {
		t.Fatalf("Failed to list templates: %v", err)
	}

	if len(templates) != 1 {
		t.Fatalf("Expected 1 template, got %d", len(templates))
	}

	// Update config file: add a second template.
	updatedTemplates := []map[string]any{
		{
			"id":          "tmpl-1",
			"name":        "Template 1 Updated",
			"owner":       "org",
			"repo":        "repo",
			"workflow_id": "build.yml",
			"ref":         "main",
		},
		{
			"id":          "tmpl-2",
			"name":        "Template 2",
			"owner":       "org",
			"repo":        "repo",
			"workflow_id": "deploy.yml",
			"ref":         "main",
		},
	}
	writeTestConfig(t, tmpDir, dbPath, updatedTemplates)

	// Call the reload endpoint.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/reload", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	// Inject admin user into context (simulating auth middleware).
	adminUser := &store.User{
		ID:       "test-user-id",
		Username: "testadmin",
		Role:     store.RoleAdmin,
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), adminUser))

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ReloadTemplatesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Message != "Templates reloaded successfully" {
		t.Errorf("Unexpected message: %s", resp.Message)
	}

	if len(resp.Groups) != 1 {
		t.Fatalf("Expected 1 group in response, got %d", len(resp.Groups))
	}

	if resp.Groups[0].GroupID != "test-group" {
		t.Errorf("Expected group_id 'test-group', got '%s'", resp.Groups[0].GroupID)
	}

	if resp.Groups[0].Templates != 2 {
		t.Errorf("Expected 2 templates, got %d", resp.Groups[0].Templates)
	}

	// Verify database was updated.
	templates, err = st.ListJobTemplatesByGroup(ctx, "test-group")
	if err != nil {
		t.Fatalf("Failed to list templates after reload: %v", err)
	}

	if len(templates) != 2 {
		t.Fatalf("Expected 2 templates in DB after reload, got %d", len(templates))
	}

	// Verify template name was updated.
	tmpl1, err := st.GetJobTemplate(ctx, "tmpl-1")
	if err != nil {
		t.Fatalf("Failed to get template: %v", err)
	}

	if tmpl1.Name != "Template 1 Updated" {
		t.Errorf("Expected template name 'Template 1 Updated', got '%s'", tmpl1.Name)
	}

	// Verify in-memory config was updated.
	s.cfgMu.RLock()
	groupsCfg := s.cfg.Groups
	s.cfgMu.RUnlock()

	if len(groupsCfg.GitHub) != 1 {
		t.Fatalf("Expected 1 group in config, got %d", len(groupsCfg.GitHub))
	}

	if len(groupsCfg.GitHub[0].WorkflowDispatchTemplates) != 2 {
		t.Errorf("Expected 2 templates in config, got %d",
			len(groupsCfg.GitHub[0].WorkflowDispatchTemplates))
	}

	// Verify audit entry was created.
	entries, _, err := st.ListAuditEntries(ctx, store.AuditQueryOpts{
		Action: ptr(store.AuditActionConfigReload),
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("Failed to list audit entries: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 audit entry, got %d", len(entries))
	}

	if entries[0].Actor != "testadmin" {
		t.Errorf("Expected audit actor 'testadmin', got '%s'", entries[0].Actor)
	}
}

func TestHandleReloadTemplates_Unauthorized(t *testing.T) {
	log := logrus.New()
	log.SetOutput(os.Stderr)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	templates := []map[string]any{
		{
			"id":          "tmpl-1",
			"name":        "Template 1",
			"owner":       "org",
			"repo":        "repo",
			"workflow_id": "build.yml",
			"ref":         "main",
		},
	}
	cfgPath := writeTestConfig(t, tmpDir, dbPath, templates)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	st := store.NewSQLiteStore(log, dbPath)
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer func() { _ = st.Stop() }()

	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	srv := NewServer(log, cfg, cfgPath, st, &stubQueue{}, &stubAuth{},
		&stubGitHubClient{}, &stubGitHubClient{}, testMetrics)

	s := srv.(*server)

	// Request without auth token should fail.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/reload", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleReloadTemplates_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	log := logrus.New()
	log.SetOutput(os.Stderr)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a valid initial config.
	templates := []map[string]any{
		{
			"id":          "tmpl-1",
			"name":        "Template 1",
			"owner":       "org",
			"repo":        "repo",
			"workflow_id": "build.yml",
			"ref":         "main",
		},
	}
	cfgPath := writeTestConfig(t, tmpDir, dbPath, templates)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	st := store.NewSQLiteStore(log, dbPath)
	if err := st.Start(ctx); err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer func() { _ = st.Stop() }()

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	if err := SyncGroupsFromConfig(ctx, log, st, cfg); err != nil {
		t.Fatalf("Failed to sync groups: %v", err)
	}

	// Point to non-existent config path so reload fails.
	srv := NewServer(log, cfg, filepath.Join(tmpDir, "nonexistent.yaml"),
		st, &stubQueue{}, &stubAuth{},
		&stubGitHubClient{}, &stubGitHubClient{}, testMetrics)

	s := srv.(*server)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/reload", nil)
	req.Header.Set("Authorization", "Bearer test-token")

	adminUser := &store.User{
		ID:       "test-user-id",
		Username: "testadmin",
		Role:     store.RoleAdmin,
	}
	req = req.WithContext(auth.ContextWithUser(req.Context(), adminUser))

	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}

	// Verify original templates are still intact.
	dbTemplates, err := st.ListJobTemplatesByGroup(ctx, "test-group")
	if err != nil {
		t.Fatalf("Failed to list templates: %v", err)
	}

	if len(dbTemplates) != 1 {
		t.Errorf("Expected 1 template still in DB, got %d", len(dbTemplates))
	}
}

func ptr[T any](v T) *T {
	return &v
}
