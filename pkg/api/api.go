package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/api/docs"
	"github.com/ethpandaops/dispatchoor/pkg/auth"
	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/github"
	"github.com/ethpandaops/dispatchoor/pkg/metrics"
	"github.com/ethpandaops/dispatchoor/pkg/queue"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// Server is the HTTP API server.
type Server interface {
	Start(ctx context.Context) error
	Stop() error
	BroadcastRunnerChange(runner *store.Runner)
}

// server implements Server.
type server struct {
	log            logrus.FieldLogger
	cfg            *config.Config
	store          store.Store
	queue          queue.Service
	auth           auth.Service
	runnersClient  github.Client
	dispatchClient github.Client
	metrics        *metrics.Metrics
	hub            *Hub
	srv            *http.Server
	router         chi.Router

	// Rate limiters for different endpoint tiers.
	authRateLimiter          *IPRateLimiter
	publicRateLimiter        *IPRateLimiter
	authenticatedRateLimiter *IPRateLimiter
}

// Ensure server implements Server.
var _ Server = (*server)(nil)

// NewServer creates a new API server.
func NewServer(log logrus.FieldLogger, cfg *config.Config, st store.Store, q queue.Service, authSvc auth.Service, runnersClient, dispatchClient github.Client, m *metrics.Metrics) Server {
	hub := NewHub(log)

	s := &server{
		log:            log.WithField("component", "api"),
		cfg:            cfg,
		store:          st,
		queue:          q,
		auth:           authSvc,
		runnersClient:  runnersClient,
		dispatchClient: dispatchClient,
		metrics:        m,
		hub:            hub,
	}

	// Initialize rate limiters if enabled.
	if cfg.Server.RateLimit.Enabled {
		s.authRateLimiter = NewIPRateLimiter(cfg.Server.RateLimit.Auth.RequestsPerMinute)
		s.publicRateLimiter = NewIPRateLimiter(cfg.Server.RateLimit.Public.RequestsPerMinute)
		s.authenticatedRateLimiter = NewIPRateLimiter(cfg.Server.RateLimit.Authenticated.RequestsPerMinute)

		log.WithFields(logrus.Fields{
			"auth_rpm":          cfg.Server.RateLimit.Auth.RequestsPerMinute,
			"public_rpm":        cfg.Server.RateLimit.Public.RequestsPerMinute,
			"authenticated_rpm": cfg.Server.RateLimit.Authenticated.RequestsPerMinute,
		}).Info("Rate limiting enabled")
	}

	// Set up callback to broadcast job state changes via WebSocket.
	q.SetJobChangeCallback(func(job *store.Job) {
		hub.BroadcastJobState(job)
	})

	s.setupRouter()

	return s
}

// Start starts the HTTP server.
func (s *server) Start(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:              s.cfg.Server.Listen,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.log.WithField("addr", s.cfg.Server.Listen).Info("Starting API server")

	// Start WebSocket hub.
	go s.hub.Run(ctx)

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.WithError(err).Error("Server error")
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *server) Stop() error {
	if s.srv == nil {
		return nil
	}

	s.log.Info("Stopping API server")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.srv.Shutdown(ctx)
}

// BroadcastRunnerChange broadcasts a runner status change to all matching groups.
func (s *server) BroadcastRunnerChange(runner *store.Runner) {
	// Find all groups whose labels the runner matches.
	for _, groupCfg := range s.cfg.Groups.GitHub {
		if runnerMatchesLabels(runner.Labels, groupCfg.RunnerLabels) {
			s.hub.BroadcastRunnerStatus(runner, groupCfg.ID)
		}
	}
}

// runnerMatchesLabels checks if a runner has all the required labels.
func runnerMatchesLabels(runnerLabels, requiredLabels []string) bool {
	runnerLabelSet := make(map[string]bool, len(runnerLabels))
	for _, label := range runnerLabels {
		runnerLabelSet[label] = true
	}

	for _, required := range requiredLabels {
		if !runnerLabelSet[required] {
			return false
		}
	}

	return true
}

func (s *server) setupRouter() {
	r := chi.NewRouter()

	// Middleware.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS.
	if len(s.cfg.Server.CORSOrigins) > 0 {
		r.Use(corsMiddleware(s.cfg.Server.CORSOrigins))
	}

	// Public endpoints with public rate limit.
	r.Group(func(r chi.Router) {
		if s.publicRateLimiter != nil {
			r.Use(s.publicRateLimiter.Middleware)
		}

		// Health check (public).
		r.Get("/health", s.handleHealth)

		// Metrics endpoint (public).
		r.Handle("/metrics", promhttp.Handler())
	})

	// API v1.
	r.Route("/api/v1", func(r chi.Router) {
		// OpenAPI spec (public rate limit).
		r.Group(func(r chi.Router) {
			if s.publicRateLimiter != nil {
				r.Use(s.publicRateLimiter.Middleware)
			}
			r.Get("/openapi.json", s.handleOpenAPISpec)
		})

		// Auth routes with strict rate limit.
		r.Group(func(r chi.Router) {
			if s.authRateLimiter != nil {
				r.Use(s.authRateLimiter.Middleware)
			}
			r.Post("/auth/login", s.handleLogin)
			r.Get("/auth/github", s.handleGitHubAuth)
			r.Get("/auth/github/callback", s.handleGitHubCallback)
			r.Post("/auth/exchange", s.handleExchangeCode)
		})

		// WebSocket (authentication handled in handler, uses authenticated rate limit).
		r.Group(func(r chi.Router) {
			if s.authenticatedRateLimiter != nil {
				r.Use(s.authenticatedRateLimiter.Middleware)
			}
			r.Get("/ws", s.handleWebSocket)
		})

		// Protected routes with authenticated rate limit.
		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware(s.auth))
			if s.authenticatedRateLimiter != nil {
				r.Use(s.authenticatedRateLimiter.Middleware)
			}

			// Auth (authenticated).
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)

			// Groups (read-only).
			r.Get("/groups", s.handleListGroups)
			r.Get("/groups/{id}", s.handleGetGroup)

			// Job templates (read-only).
			r.Get("/groups/{id}/templates", s.handleListJobTemplates)
			r.Get("/templates/{id}", s.handleGetJobTemplate)

			// Queue (read-only).
			r.Get("/groups/{id}/queue", s.handleGetQueue)
			r.Get("/groups/{id}/history", s.handleGetHistory)
			r.Get("/groups/{id}/history/stats", s.handleGetHistoryStats)

			// Jobs (read-only).
			r.Get("/jobs/{id}", s.handleGetJob)

			// Runners (read-only).
			r.Get("/groups/{id}/runners", s.handleGetRunners)
			r.Get("/runners", s.handleListRunners)

			// System (read-only).
			r.Get("/status", s.handleStatus)

			// Admin-only routes.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin())

				// Group management (admin).
				r.Post("/groups/{id}/pause", s.handlePauseGroup)
				r.Post("/groups/{id}/unpause", s.handleUnpauseGroup)

				// Queue management (admin).
				r.Post("/groups/{id}/queue", s.handleAddJob)
				r.Put("/groups/{id}/queue/reorder", s.handleReorderQueue)

				// Job management (admin).
				r.Put("/jobs/{id}", s.handleUpdateJob)
				r.Delete("/jobs/{id}", s.handleDeleteJob)
				r.Post("/jobs/{id}/pause", s.handlePauseJob)
				r.Post("/jobs/{id}/unpause", s.handleUnpauseJob)
				r.Post("/jobs/{id}/cancel", s.handleCancelJob)
				r.Post("/jobs/{id}/disable-requeue", s.handleDisableAutoRequeue)
				r.Put("/jobs/{id}/auto-requeue", s.handleUpdateAutoRequeue)

				// Runner refresh (admin).
				r.Post("/runners/refresh", s.handleRefreshRunners)
			})
		})
	})

	s.router = r
}

func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	allowAll := len(origins) == 1 && origins[0] == "*"

	originSet := make(map[string]bool, len(origins))
	for _, origin := range origins {
		originSet[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowAll || originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================================
// Response helpers
// ============================================================================

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	Error string `json:"error" example:"Something went wrong"`
}

// GroupWithStats is a group with additional statistics.
type GroupWithStats struct {
	*store.Group
	QueuedJobs    int `json:"queued_jobs" example:"5"`
	RunningJobs   int `json:"running_jobs" example:"2"`
	IdleRunners   int `json:"idle_runners" example:"3"`
	BusyRunners   int `json:"busy_runners" example:"2"`
	TotalRunners  int `json:"total_runners" example:"5"`
	TemplateCount int `json:"template_count" example:"10"`
}

func (s *server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.log.WithError(err).Error("Failed to encode JSON response")
	}
}

func (s *server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

// ============================================================================
// Handlers
// ============================================================================

// HealthResponse is the response for the health check endpoint.
type HealthResponse struct {
	Status string       `json:"status" example:"ok"`
	Config HealthConfig `json:"config"`
}

// HealthConfig contains public configuration information.
type HealthConfig struct {
	Auth HealthAuthConfig `json:"auth"`
}

// HealthAuthConfig indicates which authentication methods are enabled.
type HealthAuthConfig struct {
	Basic  bool `json:"basic" example:"true"`
	GitHub bool `json:"github" example:"false"`
}

// RateLimitErrorResponse is returned when rate limit is exceeded.
type RateLimitErrorResponse struct {
	Error string `json:"error" example:"rate limit exceeded"`
}

// handleOpenAPISpec godoc
//
//	@Summary		OpenAPI specification
//	@Description	Returns the OpenAPI 3.0 specification for the API
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	object	"OpenAPI specification"
//	@Router			/openapi.json [get]
func (s *server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(docs.SwaggerInfo.ReadDoc()))
}

// handleHealth godoc
//
//	@Summary		Health check
//	@Description	Returns the health status of the API server
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Failure		429	{object}	RateLimitErrorResponse	"Rate limit exceeded"
//	@Router			/health [get]
func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, HealthResponse{
		Status: "ok",
		Config: HealthConfig{
			Auth: HealthAuthConfig{
				Basic:  s.cfg.Auth.Basic.Enabled,
				GitHub: s.cfg.Auth.GitHub.Enabled,
			},
		},
	})
}

// handleStatus godoc
//
//	@Summary		System status
//	@Description	Returns comprehensive system status including database, GitHub API, and queue statistics
//	@Tags			system
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	SystemStatusResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		429	{object}	RateLimitErrorResponse	"Rate limit exceeded"
//	@Router			/status [get]
func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Initialize response with current timestamp.
	resp := SystemStatusResponse{
		Status:    ComponentStatusHealthy,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Check database health with timeout.
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	dbStart := time.Now()

	if err := s.store.Ping(dbCtx); err != nil {
		resp.Database = DatabaseStatus{
			Status: ComponentStatusUnhealthy,
			Error:  err.Error(),
		}

		resp.Status = ComponentStatusDegraded
	} else {
		resp.Database = DatabaseStatus{
			Status:  ComponentStatusHealthy,
			Latency: fmt.Sprintf("%dms", time.Since(dbStart).Milliseconds()),
		}
	}

	// GitHub connection and rate limit info for both clients.
	resp.GitHub = GitHubClientsStatus{}

	// Helper function to get status for a single client.
	getClientStatus := func(client github.Client, name string) *GitHubClientStatus {
		if client == nil {
			return &GitHubClientStatus{
				Status:    ComponentStatusUnhealthy,
				Connected: false,
				Error:     name + " token not configured",
			}
		}

		if !client.IsConnected() {
			return &GitHubClientStatus{
				Status:    ComponentStatusUnhealthy,
				Connected: false,
				Error:     client.ConnectionError(),
			}
		}

		remaining := client.RateLimitRemaining()
		resetTime := client.RateLimitReset()

		clientStatus := ComponentStatusHealthy
		if remaining < 100 {
			clientStatus = ComponentStatusDegraded
		}

		if remaining < 10 {
			clientStatus = ComponentStatusUnhealthy
		}

		resetIn := time.Until(resetTime)
		if resetIn < 0 {
			resetIn = 0
		}

		return &GitHubClientStatus{
			Status:             clientStatus,
			Connected:          true,
			RateLimitRemaining: remaining,
			RateLimitReset:     resetTime.UTC().Format(time.RFC3339),
			ResetIn:            resetIn.Round(time.Second).String(),
		}
	}

	resp.GitHub.Runners = getClientStatus(s.runnersClient, "Runners")
	resp.GitHub.Dispatch = getClientStatus(s.dispatchClient, "Dispatch")

	// Update overall status based on GitHub clients.
	if (resp.GitHub.Runners != nil && resp.GitHub.Runners.Status == ComponentStatusUnhealthy) ||
		(resp.GitHub.Dispatch != nil && resp.GitHub.Dispatch.Status == ComponentStatusUnhealthy) {
		if resp.Status == ComponentStatusHealthy {
			resp.Status = ComponentStatusDegraded
		}
	}

	// Queue statistics.
	pendingJobs, _ := s.store.ListJobsByStatus(ctx, store.JobStatusPending)
	triggeredJobs, _ := s.store.ListJobsByStatus(ctx, store.JobStatusTriggered)
	runningJobs, _ := s.store.ListJobsByStatus(ctx, store.JobStatusRunning)

	resp.Queue = QueueStats{
		PendingJobs:   len(pendingJobs),
		TriggeredJobs: len(triggeredJobs),
		RunningJobs:   len(runningJobs),
	}

	// Version info.
	resp.Version = VersionInfo{
		Version:   "dev",
		GitCommit: "unknown",
		BuildDate: "unknown",
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleListGroups godoc
//
//	@Summary		List groups
//	@Description	Returns all configured groups with statistics
//	@Tags			groups
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}		GroupWithStats
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups [get]
func (s *server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroups(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list groups")
		s.writeError(w, http.StatusInternalServerError, "Failed to list groups")

		return
	}

	result := make([]GroupWithStats, 0, len(groups))

	for _, group := range groups {
		stats := GroupWithStats{Group: group}

		// Get job counts.
		pendingJobs, err := s.store.ListJobsByGroup(r.Context(), group.ID, store.JobStatusPending)
		if err == nil {
			stats.QueuedJobs = len(pendingJobs)
		}

		runningJobs, err := s.store.ListJobsByGroup(r.Context(), group.ID, store.JobStatusTriggered, store.JobStatusRunning)
		if err == nil {
			stats.RunningJobs = len(runningJobs)
		}

		// Get runner counts.
		runners, err := s.store.ListRunnersByLabels(r.Context(), group.RunnerLabels)
		if err == nil {
			for _, runner := range runners {
				stats.TotalRunners++

				if runner.Busy {
					stats.BusyRunners++
				} else if runner.Status == store.RunnerStatusOnline {
					stats.IdleRunners++
				}
			}
		}

		// Get template count.
		templates, err := s.store.ListJobTemplatesByGroup(r.Context(), group.ID)
		if err == nil {
			stats.TemplateCount = len(templates)
		}

		result = append(result, stats)
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleGetGroup godoc
//
//	@Summary		Get group
//	@Description	Returns a single group by ID
//	@Tags			groups
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{object}	store.Group
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups/{id} [get]
func (s *server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	group, err := s.store.GetGroup(r.Context(), id)
	if err != nil {
		s.log.WithError(err).Error("Failed to get group")
		s.writeError(w, http.StatusInternalServerError, "Failed to get group")

		return
	}

	if group == nil {
		s.writeError(w, http.StatusNotFound, "Group not found")

		return
	}

	s.writeJSON(w, http.StatusOK, group)
}

// handlePauseGroup godoc
//
//	@Summary		Pause group
//	@Description	Pauses job dispatching for a group (requires admin)
//	@Tags			groups
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{object}	store.Group
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups/{id}/pause [post]
func (s *server) handlePauseGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	group, err := s.store.GetGroup(r.Context(), id)
	if err != nil {
		s.log.WithError(err).Error("Failed to get group")
		s.writeError(w, http.StatusInternalServerError, "Failed to get group")

		return
	}

	if group == nil {
		s.writeError(w, http.StatusNotFound, "Group not found")

		return
	}

	group.Paused = true

	if err := s.store.UpdateGroup(r.Context(), group); err != nil {
		s.log.WithError(err).Error("Failed to pause group")
		s.writeError(w, http.StatusInternalServerError, "Failed to pause group")

		return
	}

	s.log.WithField("group", id).Info("Group paused")
	s.writeJSON(w, http.StatusOK, group)
}

// handleUnpauseGroup godoc
//
//	@Summary		Unpause group
//	@Description	Resumes job dispatching for a group (requires admin)
//	@Tags			groups
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{object}	store.Group
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups/{id}/unpause [post]
func (s *server) handleUnpauseGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	group, err := s.store.GetGroup(r.Context(), id)
	if err != nil {
		s.log.WithError(err).Error("Failed to get group")
		s.writeError(w, http.StatusInternalServerError, "Failed to get group")

		return
	}

	if group == nil {
		s.writeError(w, http.StatusNotFound, "Group not found")

		return
	}

	group.Paused = false

	if err := s.store.UpdateGroup(r.Context(), group); err != nil {
		s.log.WithError(err).Error("Failed to unpause group")
		s.writeError(w, http.StatusInternalServerError, "Failed to unpause group")

		return
	}

	s.log.WithField("group", id).Info("Group unpaused")
	s.writeJSON(w, http.StatusOK, group)
}

// handleListJobTemplates godoc
//
//	@Summary		List job templates
//	@Description	Returns all job templates for a group
//	@Tags			templates
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{array}		store.JobTemplate
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups/{id}/templates [get]
func (s *server) handleListJobTemplates(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	templates, err := s.store.ListJobTemplatesByGroup(r.Context(), groupID)
	if err != nil {
		s.log.WithError(err).Error("Failed to list job templates")
		s.writeError(w, http.StatusInternalServerError, "Failed to list job templates")

		return
	}

	if templates == nil {
		templates = []*store.JobTemplate{}
	}

	s.writeJSON(w, http.StatusOK, templates)
}

// handleGetJobTemplate godoc
//
//	@Summary		Get job template
//	@Description	Returns a single job template by ID
//	@Tags			templates
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	store.JobTemplate
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/templates/{id} [get]
func (s *server) handleGetJobTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	template, err := s.store.GetJobTemplate(r.Context(), id)
	if err != nil {
		s.log.WithError(err).Error("Failed to get job template")
		s.writeError(w, http.StatusInternalServerError, "Failed to get job template")

		return
	}

	if template == nil {
		s.writeError(w, http.StatusNotFound, "Job template not found")

		return
	}

	s.writeJSON(w, http.StatusOK, template)
}

// handleGetQueue godoc
//
//	@Summary		Get queue
//	@Description	Returns all pending, triggered, and running jobs in the group's queue
//	@Tags			queue
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{array}		store.Job
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups/{id}/queue [get]
func (s *server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	jobs, err := s.store.ListJobsByGroup(r.Context(), groupID, store.JobStatusPending, store.JobStatusTriggered, store.JobStatusRunning)
	if err != nil {
		s.log.WithError(err).Error("Failed to get queue")
		s.writeError(w, http.StatusInternalServerError, "Failed to get queue")

		return
	}

	if jobs == nil {
		jobs = []*store.Job{}
	}

	s.writeJSON(w, http.StatusOK, jobs)
}

// handleGetRunners godoc
//
//	@Summary		Get group runners
//	@Description	Returns all runners matching the group's runner labels
//	@Tags			runners
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{array}		store.Runner
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/groups/{id}/runners [get]
func (s *server) handleGetRunners(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	group, err := s.store.GetGroup(r.Context(), groupID)
	if err != nil {
		s.log.WithError(err).Error("Failed to get group")
		s.writeError(w, http.StatusInternalServerError, "Failed to get group")

		return
	}

	if group == nil {
		s.writeError(w, http.StatusNotFound, "Group not found")

		return
	}

	runners, err := s.store.ListRunnersByLabels(r.Context(), group.RunnerLabels)
	if err != nil {
		s.log.WithError(err).Error("Failed to list runners")
		s.writeError(w, http.StatusInternalServerError, "Failed to list runners")

		return
	}

	if runners == nil {
		runners = []*store.Runner{}
	}

	s.writeJSON(w, http.StatusOK, runners)
}

// handleListRunners godoc
//
//	@Summary		List all runners
//	@Description	Returns all GitHub Actions runners across all groups
//	@Tags			runners
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}		store.Runner
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/runners [get]
func (s *server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	runners, err := s.store.ListRunners(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to list runners")
		s.writeError(w, http.StatusInternalServerError, "Failed to list runners")

		return
	}

	if runners == nil {
		runners = []*store.Runner{}
	}

	s.writeJSON(w, http.StatusOK, runners)
}

// ============================================================================
// Job Handlers
// ============================================================================

// AddJobRequest is the request body for adding a job to the queue.
type AddJobRequest struct {
	TemplateID   string            `json:"template_id,omitempty" example:"my-template"`
	Inputs       map[string]string `json:"inputs"`
	AutoRequeue  bool              `json:"auto_requeue" example:"false"`
	RequeueLimit *int              `json:"requeue_limit" example:"3"`
	// Manual job fields (used when template_id is empty).
	Name       string            `json:"name,omitempty" example:"Manual Job"`
	Owner      string            `json:"owner,omitempty" example:"ethpandaops"`
	Repo       string            `json:"repo,omitempty" example:"dispatchoor"`
	WorkflowID string            `json:"workflow_id,omitempty" example:"deploy.yml"`
	Ref        string            `json:"ref,omitempty" example:"main"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// handleAddJob godoc
//
//	@Summary		Add job to queue
//	@Description	Adds a new job to the group's queue, either from a template or with manual configuration
//	@Tags			jobs
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Group ID"
//	@Param			body	body		AddJobRequest	true	"Job configuration"
//	@Success		201		{object}	store.Job
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Router			/groups/{id}/queue [post]
func (s *server) handleAddJob(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	var req AddJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	// Validate: either template_id is provided, or all manual fields are required.
	if req.TemplateID == "" {
		// Manual job - validate required fields.
		if req.Owner == "" || req.Repo == "" || req.WorkflowID == "" || req.Ref == "" {
			s.writeError(w, http.StatusBadRequest, "Manual jobs require owner, repo, workflow_id, and ref")

			return
		}
	}

	createdBy := "anonymous"
	if user := auth.UserFromContext(r.Context()); user != nil {
		createdBy = user.Username
	}

	opts := &queue.EnqueueOptions{
		AutoRequeue:  req.AutoRequeue,
		RequeueLimit: req.RequeueLimit,
		// Manual job fields.
		Name:       req.Name,
		Owner:      req.Owner,
		Repo:       req.Repo,
		WorkflowID: req.WorkflowID,
		Ref:        req.Ref,
		Labels:     req.Labels,
	}

	job, err := s.queue.Enqueue(r.Context(), groupID, req.TemplateID, createdBy, req.Inputs, opts)
	if err != nil {
		s.log.WithError(err).Error("Failed to add job")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	s.writeJSON(w, http.StatusCreated, job)
}

// handleGetJob godoc
//
//	@Summary		Get job
//	@Description	Returns a single job by ID
//	@Tags			jobs
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Job ID"
//	@Success		200	{object}	store.Job
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/jobs/{id} [get]
func (s *server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := s.queue.GetJob(r.Context(), jobID)
	if err != nil {
		s.log.WithError(err).Error("Failed to get job")
		s.writeError(w, http.StatusInternalServerError, "Failed to get job")

		return
	}

	if job == nil {
		s.writeError(w, http.StatusNotFound, "Job not found")

		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// UpdateJobRequest is the request body for updating a job.
type UpdateJobRequest struct {
	Inputs     map[string]string `json:"inputs"`
	Name       *string           `json:"name,omitempty" example:"Updated Job"`
	Owner      *string           `json:"owner,omitempty" example:"ethpandaops"`
	Repo       *string           `json:"repo,omitempty" example:"dispatchoor"`
	WorkflowID *string           `json:"workflow_id,omitempty" example:"deploy.yml"`
	Ref        *string           `json:"ref,omitempty" example:"main"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// handleUpdateJob godoc
//
//	@Summary		Update job
//	@Description	Updates job configuration (inputs, name, owner, repo, workflow_id, ref, labels)
//	@Tags			jobs
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Job ID"
//	@Param			body	body		UpdateJobRequest	true	"Job updates"
//	@Success		200		{object}	store.Job
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/jobs/{id} [put]
func (s *server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	var req UpdateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	opts := &queue.UpdateJobOptions{
		Inputs:     req.Inputs,
		Name:       req.Name,
		Owner:      req.Owner,
		Repo:       req.Repo,
		WorkflowID: req.WorkflowID,
		Ref:        req.Ref,
		Labels:     req.Labels,
	}

	if err := s.queue.UpdateJob(r.Context(), jobID, opts); err != nil {
		s.log.WithError(err).Error("Failed to update job")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	job, _ := s.queue.GetJob(r.Context(), jobID)
	s.writeJSON(w, http.StatusOK, job)
}

// handleDeleteJob godoc
//
//	@Summary		Delete job
//	@Description	Removes a job from the queue (requires admin)
//	@Tags			jobs
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Job ID"
//	@Success		204	"Job deleted successfully"
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/jobs/{id} [delete]
func (s *server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	if err := s.queue.Remove(r.Context(), jobID); err != nil {
		s.log.WithError(err).Error("Failed to delete job")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handlePauseJob godoc
//
//	@Summary		Pause job
//	@Description	Pauses a job in the queue (requires admin)
//	@Tags			jobs
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Job ID"
//	@Success		200	{object}	store.Job
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/jobs/{id}/pause [post]
func (s *server) handlePauseJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := s.queue.Pause(r.Context(), jobID)
	if err != nil {
		s.log.WithError(err).Error("Failed to pause job")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// handleUnpauseJob godoc
//
//	@Summary		Unpause job
//	@Description	Resumes a paused job (requires admin)
//	@Tags			jobs
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Job ID"
//	@Success		200	{object}	store.Job
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/jobs/{id}/unpause [post]
func (s *server) handleUnpauseJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := s.queue.Unpause(r.Context(), jobID)
	if err != nil {
		s.log.WithError(err).Error("Failed to unpause job")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// handleCancelJob godoc
//
//	@Summary		Cancel job
//	@Description	Cancels a triggered or running job (requires admin). If running on GitHub, also cancels the workflow run.
//	@Tags			jobs
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Job ID"
//	@Success		200	{object}	store.Job
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/jobs/{id}/cancel [post]
func (s *server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	// Get the job.
	job, err := s.queue.GetJob(r.Context(), jobID)
	if err != nil {
		s.log.WithError(err).Error("Failed to get job")
		s.writeError(w, http.StatusInternalServerError, "Failed to get job")

		return
	}

	if job == nil {
		s.writeError(w, http.StatusNotFound, "Job not found")

		return
	}

	// Verify job is triggered or running.
	if job.Status != store.JobStatusTriggered && job.Status != store.JobStatusRunning {
		s.writeError(w, http.StatusBadRequest, "Job can only be cancelled when triggered or running")

		return
	}

	// If we have a run ID, cancel the workflow run on GitHub.
	if job.RunID != nil && *job.RunID != 0 {
		// Get owner/repo - prefer job overrides, fall back to template.
		var owner, repo string

		if job.Owner != nil && *job.Owner != "" {
			owner = *job.Owner
		}

		if job.Repo != nil && *job.Repo != "" {
			repo = *job.Repo
		}

		// If not set on job, get from template.
		if (owner == "" || repo == "") && job.TemplateID != "" {
			template, err := s.store.GetJobTemplate(r.Context(), job.TemplateID)
			if err != nil {
				s.log.WithError(err).Error("Failed to get job template")
				s.writeError(w, http.StatusInternalServerError, "Failed to get job template")

				return
			}

			if template == nil {
				s.writeError(w, http.StatusInternalServerError, "Job template not found")

				return
			}

			if owner == "" {
				owner = template.Owner
			}

			if repo == "" {
				repo = template.Repo
			}
		}

		if owner == "" || repo == "" {
			s.writeError(w, http.StatusInternalServerError, "Cannot determine owner/repo for job")

			return
		}

		// Check if dispatch client is available.
		if s.dispatchClient == nil || !s.dispatchClient.IsConnected() {
			s.writeError(w, http.StatusServiceUnavailable, "GitHub integration is not available")

			return
		}

		// Cancel the workflow run on GitHub.
		if err := s.dispatchClient.CancelWorkflowRun(r.Context(), owner, repo, *job.RunID); err != nil {
			s.log.WithError(err).Warn("Cancel request returned error, checking actual run status")

			// Check if the run was actually cancelled despite the error.
			// GitHub can return transient errors like "job scheduled on GitHub side"
			// even when the cancellation succeeds.
			run, getErr := s.dispatchClient.GetWorkflowRun(r.Context(), owner, repo, *job.RunID)
			if getErr != nil {
				s.log.WithError(getErr).Error("Failed to verify workflow run status after cancel error")
				s.writeError(w, http.StatusInternalServerError, "Failed to cancel workflow run on GitHub")

				return
			}

			// If the run is already completed with a non-cancel conclusion, we can't cancel it.
			if run.Status == "completed" && run.Conclusion != "cancelled" {
				s.log.WithFields(logrus.Fields{
					"status":     run.Status,
					"conclusion": run.Conclusion,
				}).Warn("Workflow run already completed, cannot cancel")
				// Still proceed to mark job as cancelled locally since the run is done.
			} else if run.Conclusion == "cancelled" {
				s.log.Info("Workflow run confirmed cancelled")
			} else {
				// Run is still in_progress - GitHub is processing the cancellation.
				// This is expected; proceed with marking job cancelled locally.
				s.log.WithFields(logrus.Fields{
					"status":     run.Status,
					"conclusion": run.Conclusion,
				}).Info("Workflow run cancellation in progress")
			}
		}
	}

	// Mark the job as cancelled.
	if err := s.queue.MarkCancelled(r.Context(), job.ID); err != nil {
		s.log.WithError(err).Error("Failed to mark job as cancelled")
		s.writeError(w, http.StatusInternalServerError, "Failed to mark job as cancelled")

		return
	}

	// Get the updated job.
	job, _ = s.queue.GetJob(r.Context(), jobID)

	s.writeJSON(w, http.StatusOK, job)
}

// handleDisableAutoRequeue godoc
//
//	@Summary		Disable auto-requeue
//	@Description	Disables auto-requeue for a job (requires admin)
//	@Tags			jobs
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"Job ID"
//	@Success		200	{object}	store.Job
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Router			/jobs/{id}/disable-requeue [post]
func (s *server) handleDisableAutoRequeue(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	job, err := s.queue.DisableAutoRequeue(r.Context(), jobID)
	if err != nil {
		s.log.WithError(err).Error("Failed to disable auto-requeue")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// UpdateAutoRequeueRequest is the request body for updating auto-requeue settings.
type UpdateAutoRequeueRequest struct {
	AutoRequeue  bool `json:"auto_requeue" example:"true"`
	RequeueLimit *int `json:"requeue_limit" example:"5"`
}

// handleUpdateAutoRequeue godoc
//
//	@Summary		Update auto-requeue settings
//	@Description	Enables or disables auto-requeue for a job and optionally sets a requeue limit
//	@Tags			jobs
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"Job ID"
//	@Param			body	body		UpdateAutoRequeueRequest	true	"Auto-requeue settings"
//	@Success		200		{object}	store.Job
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Router			/jobs/{id}/auto-requeue [put]
func (s *server) handleUpdateAutoRequeue(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")

	var req UpdateAutoRequeueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	job, err := s.queue.UpdateAutoRequeue(r.Context(), jobID, req.AutoRequeue, req.RequeueLimit)
	if err != nil {
		s.log.WithError(err).Error("Failed to update auto-requeue")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// ReorderQueueRequest is the request body for reordering the job queue.
type ReorderQueueRequest struct {
	JobIDs []string `json:"job_ids" example:"job-1,job-2,job-3"`
}

// handleReorderQueue godoc
//
//	@Summary		Reorder queue
//	@Description	Reorders jobs in the queue by specifying the desired order of job IDs
//	@Tags			queue
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string					true	"Group ID"
//	@Param			body	body	ReorderQueueRequest		true	"New job order"
//	@Success		204		"Queue reordered successfully"
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Router			/groups/{id}/queue/reorder [put]
func (s *server) handleReorderQueue(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	var req ReorderQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	if len(req.JobIDs) == 0 {
		s.writeError(w, http.StatusBadRequest, "job_ids is required")

		return
	}

	if err := s.queue.Reorder(r.Context(), groupID, req.JobIDs); err != nil {
		s.log.WithError(err).Error("Failed to reorder queue")
		s.writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// Status Types
// ============================================================================

// ComponentStatus represents health status of a component.
type ComponentStatus string

const (
	ComponentStatusHealthy   ComponentStatus = "healthy"
	ComponentStatusDegraded  ComponentStatus = "degraded"
	ComponentStatusUnhealthy ComponentStatus = "unhealthy"
)

// DatabaseStatus contains database health information.
type DatabaseStatus struct {
	Status  ComponentStatus `json:"status"`
	Latency string          `json:"latency,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// GitHubClientStatus contains status and rate limit information for a single GitHub client.
type GitHubClientStatus struct {
	Status             ComponentStatus `json:"status"`
	Connected          bool            `json:"connected"`
	Error              string          `json:"error,omitempty"`
	RateLimitRemaining int             `json:"rate_limit_remaining"`
	RateLimitReset     string          `json:"rate_limit_reset,omitempty"`
	ResetIn            string          `json:"reset_in,omitempty"`
}

// GitHubClientsStatus contains status for both GitHub clients.
type GitHubClientsStatus struct {
	Runners  *GitHubClientStatus `json:"runners,omitempty"`
	Dispatch *GitHubClientStatus `json:"dispatch,omitempty"`
}

// QueueStats contains queue statistics.
type QueueStats struct {
	PendingJobs   int `json:"pending_jobs"`
	TriggeredJobs int `json:"triggered_jobs"`
	RunningJobs   int `json:"running_jobs"`
}

// VersionInfo contains build version information.
type VersionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildDate string `json:"build_date"`
}

// SystemStatusResponse is the comprehensive status response.
type SystemStatusResponse struct {
	Status    ComponentStatus     `json:"status"`
	Timestamp string              `json:"timestamp"`
	Database  DatabaseStatus      `json:"database"`
	GitHub    GitHubClientsStatus `json:"github"`
	Queue     QueueStats          `json:"queue"`
	Version   VersionInfo         `json:"version"`
}

// HistoryResponse wraps the paginated history response.
type HistoryResponse struct {
	Jobs       []*store.Job `json:"jobs"`
	HasMore    bool         `json:"has_more" example:"true"`
	NextCursor string       `json:"next_cursor,omitempty" example:"2024-01-15T10:30:00Z"`
	TotalCount int          `json:"total_count" example:"150"`
}

// handleGetHistory godoc
//
//	@Summary		Get job history
//	@Description	Returns paginated history of completed, failed, and cancelled jobs
//	@Tags			history
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		string	true	"Group ID"
//	@Param			limit	query		int		false	"Number of jobs to return (max 100)"	default(50)
//	@Param			before	query		string	false	"Cursor for pagination (RFC3339 timestamp)"
//	@Param			status	query		string	false	"Filter by status (comma-separated: completed,failed,cancelled)"
//	@Success		200		{object}	HistoryResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/groups/{id}/history [get]
func (s *server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	var before *time.Time

	if beforeStr := r.URL.Query().Get("before"); beforeStr != "" {
		t, err := time.Parse(time.RFC3339Nano, beforeStr)
		if err == nil {
			before = &t
		}
	}

	// Parse status filter (comma-separated).
	var statuses []store.JobStatus

	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		statusParts := strings.Split(statusStr, ",")
		for _, st := range statusParts {
			st = strings.TrimSpace(st)
			switch st {
			case "completed":
				statuses = append(statuses, store.JobStatusCompleted)
			case "failed":
				statuses = append(statuses, store.JobStatusFailed)
			case "cancelled":
				statuses = append(statuses, store.JobStatusCancelled)
			}
		}
	}

	// Parse label filters (label.KEY=VALUE).
	labels := make(map[string]string)

	for key, values := range r.URL.Query() {
		if strings.HasPrefix(key, "label.") && len(values) > 0 {
			labelKey := strings.TrimPrefix(key, "label.")
			labels[labelKey] = values[0]
		}
	}

	opts := store.HistoryQueryOpts{
		GroupID:  groupID,
		Limit:    limit,
		Before:   before,
		Statuses: statuses,
		Labels:   labels,
	}

	result, err := s.queue.ListHistoryPaginated(r.Context(), opts)
	if err != nil {
		s.log.WithError(err).Error("Failed to get history")
		s.writeError(w, http.StatusInternalServerError, "Failed to get history")

		return
	}

	resp := HistoryResponse{
		Jobs:       result.Jobs,
		HasMore:    result.HasMore,
		TotalCount: result.TotalCount,
	}

	if result.NextCursor != nil {
		resp.NextCursor = result.NextCursor.Format(time.RFC3339Nano)
	}

	if resp.Jobs == nil {
		resp.Jobs = []*store.Job{}
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// HistoryStatsResponse wraps the aggregated history statistics.
type HistoryStatsResponse struct {
	Buckets []HistoryStatsBucket `json:"buckets"`
	Range   HistoryStatsRange    `json:"range"`
	Totals  HistoryStatsTotals   `json:"totals"`
}

// HistoryStatsBucket represents job counts in a time bucket.
type HistoryStatsBucket struct {
	Timestamp string `json:"timestamp" example:"2024-01-15T10:00:00Z"`
	Completed int    `json:"completed" example:"5"`
	Failed    int    `json:"failed" example:"1"`
	Cancelled int    `json:"cancelled" example:"0"`
}

// HistoryStatsRange describes the time range of the statistics.
type HistoryStatsRange struct {
	Start          string `json:"start" example:"2024-01-15T00:00:00Z"`
	End            string `json:"end" example:"2024-01-16T00:00:00Z"`
	BucketDuration string `json:"bucket_duration" example:"1h0m0s"`
}

// HistoryStatsTotals contains total counts across all buckets.
type HistoryStatsTotals struct {
	Completed int `json:"completed" example:"120"`
	Failed    int `json:"failed" example:"15"`
	Cancelled int `json:"cancelled" example:"5"`
}

// handleGetHistoryStats godoc
//
//	@Summary		Get history statistics
//	@Description	Returns aggregated job statistics over a time range
//	@Tags			history
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		string	true	"Group ID"
//	@Param			range	query		string	false	"Time range (1h, 6h, 24h, 7d, 30d, auto)"	default(auto)
//	@Success		200		{object}	HistoryStatsResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/groups/{id}/history/stats [get]
func (s *server) handleGetHistoryStats(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "id")

	// Parse time range parameter.
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "auto"
	}

	now := time.Now()
	var start, end time.Time
	var buckets int

	end = now

	switch rangeStr {
	case "1h":
		start = now.Add(-1 * time.Hour)
		buckets = 12 // 5 minute intervals
	case "6h":
		start = now.Add(-6 * time.Hour)
		buckets = 24 // 15 minute intervals
	case "24h":
		start = now.Add(-24 * time.Hour)
		buckets = 24 // 1 hour intervals
	case "7d":
		start = now.Add(-7 * 24 * time.Hour)
		buckets = 28 // 6 hour intervals
	case "30d":
		start = now.Add(-30 * 24 * time.Hour)
		buckets = 30 // 1 day intervals
	case "auto":
		// For auto mode, show all jobs from oldest to now.
		oldestTime, _, err := s.store.GetHistoryTimeBounds(r.Context(), groupID)
		if err != nil {
			s.log.WithError(err).Error("Failed to get history time bounds")
			s.writeError(w, http.StatusInternalServerError, "Failed to get history stats")

			return
		}

		if oldestTime == nil {
			// No history data, return empty buckets for 24h.
			start = now.Add(-24 * time.Hour)
			buckets = 24
		} else {
			// Set start to oldest job time (with small buffer).
			start = oldestTime.Add(-1 * time.Minute)

			// Calculate appropriate number of buckets based on span.
			span := now.Sub(start)

			if span > 30*24*time.Hour {
				buckets = 30 // ~1 day per bucket
			} else if span > 7*24*time.Hour {
				buckets = 28 // ~6 hours per bucket
			} else if span > 24*time.Hour {
				buckets = 24 // ~1 hour per bucket
			} else if span > 6*time.Hour {
				buckets = 24 // ~15 min per bucket
			} else if span > 1*time.Hour {
				buckets = 12 // ~5 min per bucket
			} else {
				buckets = 12 // Small intervals
			}
		}
	default:
		s.writeError(w, http.StatusBadRequest, "Invalid range parameter")

		return
	}

	opts := store.HistoryStatsOpts{
		GroupID: groupID,
		Start:   start,
		End:     end,
		Buckets: buckets,
	}

	result, err := s.store.GetHistoryStats(r.Context(), opts)
	if err != nil {
		s.log.WithError(err).Error("Failed to get history stats")
		s.writeError(w, http.StatusInternalServerError, "Failed to get history stats")

		return
	}

	// Convert to response format with string timestamps.
	respBuckets := make([]HistoryStatsBucket, len(result.Buckets))
	for i, bucket := range result.Buckets {
		respBuckets[i] = HistoryStatsBucket{
			Timestamp: bucket.Timestamp.Format(time.RFC3339),
			Completed: bucket.Completed,
			Failed:    bucket.Failed,
			Cancelled: bucket.Cancelled,
		}
	}

	resp := HistoryStatsResponse{
		Buckets: respBuckets,
		Range: HistoryStatsRange{
			Start:          result.Range.Start.Format(time.RFC3339),
			End:            result.Range.End.Format(time.RFC3339),
			BucketDuration: result.Range.BucketDuration.String(),
		},
		Totals: HistoryStatsTotals{
			Completed: result.Totals.Completed,
			Failed:    result.Totals.Failed,
			Cancelled: result.Totals.Cancelled,
		},
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleRefreshRunners godoc
//
//	@Summary		Refresh runners
//	@Description	Triggers a refresh of runner information from GitHub (requires admin)
//	@Tags			runners
//	@Security		BearerAuth
//	@Success		204	"Runners refresh initiated"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Router			/runners/refresh [post]
func (s *server) handleRefreshRunners(w http.ResponseWriter, _ *http.Request) {
	// TODO: Implement runner refresh by calling poller.ForceRefresh()
	// For now, just return success.
	w.WriteHeader(http.StatusNoContent)
}

// handleWebSocket godoc
//
//	@Summary		WebSocket connection
//	@Description	Establishes a WebSocket connection for real-time job and runner updates
//	@Tags			websocket
//	@Param			token	query	string	false	"Authentication token"
//	@Success		101		"WebSocket connection established"
//	@Failure		401		{object}	ErrorResponse
//	@Router			/ws [get]
func (s *server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ServeWs(s.hub, s.auth, s.cfg.Server.CORSOrigins, w, r)
}

// ============================================================================
// Auth Handlers
// ============================================================================

// LoginRequest is the request body for username/password login.
type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"password123"`
}

// LoginResponse is the response for successful authentication.
type LoginResponse struct {
	Token string      `json:"token" example:"eyJhbGciOiJIUzI1NiIs..."`
	User  *store.User `json:"user"`
}

// handleLogin godoc
//
//	@Summary		Login with username and password
//	@Description	Authenticates a user with username and password, returns JWT token
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		LoginRequest	true	"Login credentials"
//	@Success		200		{object}	LoginResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		429		{object}	RateLimitErrorResponse	"Rate limit exceeded"
//	@Router			/auth/login [post]
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	if req.Username == "" || req.Password == "" {
		s.writeError(w, http.StatusBadRequest, "Username and password are required")

		return
	}

	user, token, err := s.auth.AuthenticateBasic(r.Context(), req.Username, req.Password)
	if err != nil {
		s.log.WithError(err).WithField("username", req.Username).Warn("Login failed")
		s.writeError(w, http.StatusUnauthorized, "Invalid credentials")

		return
	}

	// Set session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.isSecureRequest(r),
		MaxAge:   int(s.cfg.Auth.SessionTTL.Seconds()),
	})

	s.writeJSON(w, http.StatusOK, LoginResponse{
		Token: token,
		User:  user,
	})
}

// handleLogout godoc
//
//	@Summary		Logout
//	@Description	Logs out the current user and invalidates the session
//	@Tags			auth
//	@Security		BearerAuth
//	@Success		204	"Logged out successfully"
//	@Failure		401	{object}	ErrorResponse
//	@Router			/auth/logout [post]
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Get token from cookie or header.
	token := ""

	if cookie, err := r.Cookie("session"); err == nil {
		token = cookie.Value
	}

	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		}
	}

	if token != "" {
		if err := s.auth.Logout(r.Context(), token); err != nil {
			s.log.WithError(err).Warn("Logout error")
		}
	}

	// Clear session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.isSecureRequest(r),
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleMe godoc
//
//	@Summary		Get current user
//	@Description	Returns the currently authenticated user's information
//	@Tags			auth
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	store.User
//	@Failure		401	{object}	ErrorResponse
//	@Router			/auth/me [get]
func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		s.writeError(w, http.StatusUnauthorized, "Not authenticated")

		return
	}

	s.writeJSON(w, http.StatusOK, user)
}

// handleGitHubAuth godoc
//
//	@Summary		GitHub OAuth initiation
//	@Description	Initiates GitHub OAuth flow by redirecting to GitHub authorization page
//	@Tags			auth
//	@Param			state	query	string	false	"OAuth state for CSRF protection"
//	@Success		307		"Redirect to GitHub"
//	@Failure		404		{object}	ErrorResponse	"GitHub auth not enabled"
//	@Failure		429		{object}	RateLimitErrorResponse	"Rate limit exceeded"
//	@Router			/auth/github [get]
func (s *server) handleGitHubAuth(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Auth.GitHub.Enabled {
		s.writeError(w, http.StatusNotFound, "GitHub auth is not enabled")

		return
	}

	// Generate cryptographically secure state for CSRF protection.
	state, err := s.auth.CreateOAuthState(r.Context())
	if err != nil {
		s.log.WithError(err).Error("Failed to create OAuth state")
		s.writeError(w, http.StatusInternalServerError, "Failed to initiate OAuth flow")

		return
	}

	authURL := s.auth.GetGitHubAuthURL(state)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// handleGitHubCallback godoc
//
//	@Summary		GitHub OAuth callback
//	@Description	Handles GitHub OAuth callback and completes authentication
//	@Tags			auth
//	@Produce		json
//	@Param			code		query		string	true	"OAuth authorization code"
//	@Param			state		query		string	false	"OAuth state for CSRF validation"
//	@Param			redirect	query		string	false	"URL to redirect after successful auth"
//	@Success		200			{object}	LoginResponse	"JSON response for API clients"
//	@Success		307			"Redirect for browser clients"
//	@Failure		400			{object}	ErrorResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse	"GitHub auth not enabled"
//	@Failure		429			{object}	RateLimitErrorResponse	"Rate limit exceeded"
//	@Router			/auth/github/callback [get]
func (s *server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Auth.GitHub.Enabled {
		s.writeError(w, http.StatusNotFound, "GitHub auth is not enabled")

		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.writeError(w, http.StatusBadRequest, "Missing code parameter")

		return
	}

	// Validate state parameter for CSRF protection.
	state := r.URL.Query().Get("state")
	if state == "" {
		s.writeError(w, http.StatusBadRequest, "Missing state parameter")

		return
	}

	if err := s.auth.ValidateOAuthState(r.Context(), state); err != nil {
		s.log.WithError(err).Warn("Invalid OAuth state")
		s.writeError(w, http.StatusBadRequest, "Invalid or expired state parameter")

		return
	}

	user, token, err := s.auth.AuthenticateGitHub(r.Context(), code)
	if err != nil {
		s.log.WithError(err).Warn("GitHub auth failed")
		s.writeError(w, http.StatusUnauthorized, "Authentication failed")

		return
	}

	// Set session cookie (works for same-origin requests).
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.isSecureRequest(r),
		MaxAge:   int(s.cfg.Auth.SessionTTL.Seconds()),
	})

	// Check if client wants JSON response (API clients) or redirect (browsers).
	if r.Header.Get("Accept") == "application/json" {
		s.writeJSON(w, http.StatusOK, LoginResponse{
			Token: token,
			User:  user,
		})

		return
	}

	// Redirect to frontend for browser-based flow.
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" {
		redirectURL = s.cfg.Auth.GitHub.RedirectURL
	}

	if redirectURL == "" {
		redirectURL = "/"
	}

	// Generate one-time authorization code for cross-origin token exchange.
	// This is more secure than putting the session token in the URL.
	authCode, err := s.auth.CreateAuthCode(r.Context(), user.ID)
	if err != nil {
		s.log.WithError(err).Error("Failed to create auth code")
		s.writeError(w, http.StatusInternalServerError, "Failed to complete authentication")

		return
	}

	// Append auth code to redirect URL for the UI to exchange for a token.
	if strings.Contains(redirectURL, "?") {
		redirectURL += "&code=" + authCode
	} else {
		redirectURL += "?code=" + authCode
	}

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

type exchangeCodeRequest struct {
	Code string `json:"code"`
}

// handleExchangeCode godoc
//
//	@Summary		Exchange auth code for token
//	@Description	Exchanges a one-time authorization code for a session token
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		exchangeCodeRequest	true	"Auth code"
//	@Success		200		{object}	LoginResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		429		{object}	RateLimitErrorResponse	"Rate limit exceeded"
//	@Router			/auth/exchange [post]
func (s *server) handleExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req exchangeCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")

		return
	}

	if req.Code == "" {
		s.writeError(w, http.StatusBadRequest, "Code is required")

		return
	}

	user, token, err := s.auth.ExchangeAuthCode(r.Context(), req.Code)
	if err != nil {
		s.log.WithError(err).Warn("Code exchange failed")
		s.writeError(w, http.StatusUnauthorized, "Invalid or expired code")

		return
	}

	// Set session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.isSecureRequest(r),
		MaxAge:   int(s.cfg.Auth.SessionTTL.Seconds()),
	})

	s.writeJSON(w, http.StatusOK, LoginResponse{
		Token: token,
		User:  user,
	})
}

// isSecureRequest checks if the request was made over HTTPS.
func (s *server) isSecureRequest(r *http.Request) bool {
	// Check TLS directly.
	if r.TLS != nil {
		return true
	}

	// Check X-Forwarded-Proto header (common with reverse proxies).
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}

	return false
}

// SyncGroupsFromConfig synchronizes groups and job templates from configuration.
func SyncGroupsFromConfig(ctx context.Context, log logrus.FieldLogger, st store.Store, cfg *config.Config) error {
	log.Info("Syncing groups from configuration")

	now := time.Now()

	for _, groupCfg := range cfg.Groups.GitHub {
		// Check if group exists.
		existing, err := st.GetGroup(ctx, groupCfg.ID)
		if err != nil {
			return fmt.Errorf("checking group %s: %w", groupCfg.ID, err)
		}

		group := &store.Group{
			ID:           groupCfg.ID,
			Name:         groupCfg.Name,
			Description:  groupCfg.Description,
			RunnerLabels: groupCfg.RunnerLabels,
			Enabled:      true,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if existing == nil {
			log.WithField("group", groupCfg.ID).Info("Creating group")

			if err := st.CreateGroup(ctx, group); err != nil {
				return fmt.Errorf("creating group %s: %w", groupCfg.ID, err)
			}
		} else {
			log.WithField("group", groupCfg.ID).Info("Updating group")

			group.CreatedAt = existing.CreatedAt

			if err := st.UpdateGroup(ctx, group); err != nil {
				return fmt.Errorf("updating group %s: %w", groupCfg.ID, err)
			}
		}

		// Build set of template IDs in config for this group.
		configTemplateIDs := make(map[string]bool, len(groupCfg.WorkflowDispatchTemplates))
		for _, tmplCfg := range groupCfg.WorkflowDispatchTemplates {
			configTemplateIDs[tmplCfg.ID] = true
		}

		// Sync job templates (upsert instead of delete/recreate to preserve jobs).
		for _, tmplCfg := range groupCfg.WorkflowDispatchTemplates {
			template := &store.JobTemplate{
				ID:            tmplCfg.ID,
				GroupID:       groupCfg.ID,
				Name:          tmplCfg.Name,
				Owner:         tmplCfg.Owner,
				Repo:          tmplCfg.Repo,
				WorkflowID:    tmplCfg.WorkflowID,
				Ref:           tmplCfg.Ref,
				DefaultInputs: tmplCfg.Inputs,
				Labels:        tmplCfg.Labels,
				InConfig:      true,
				SourceType:    tmplCfg.SourceType,
				SourcePath:    tmplCfg.SourcePath,
				CreatedAt:     now,
				UpdatedAt:     now,
			}

			// Check if template exists.
			existingTemplate, err := st.GetJobTemplate(ctx, tmplCfg.ID)
			if err != nil {
				return fmt.Errorf("checking job template %s: %w", tmplCfg.ID, err)
			}

			if existingTemplate == nil {
				log.WithFields(logrus.Fields{
					"group":    groupCfg.ID,
					"template": tmplCfg.ID,
				}).Info("Creating job template")

				if err := st.CreateJobTemplate(ctx, template); err != nil {
					return fmt.Errorf("creating job template %s: %w", tmplCfg.ID, err)
				}
			} else {
				log.WithFields(logrus.Fields{
					"group":    groupCfg.ID,
					"template": tmplCfg.ID,
				}).Debug("Updating job template")

				template.CreatedAt = existingTemplate.CreatedAt

				if err := st.UpdateJobTemplate(ctx, template); err != nil {
					return fmt.Errorf("updating job template %s: %w", tmplCfg.ID, err)
				}
			}
		}

		// Handle orphaned templates: templates in DB but not in config.
		dbTemplates, err := st.ListJobTemplatesByGroup(ctx, groupCfg.ID)
		if err != nil {
			return fmt.Errorf("listing templates for group %s: %w", groupCfg.ID, err)
		}

		for _, dbTmpl := range dbTemplates {
			if configTemplateIDs[dbTmpl.ID] {
				continue // Template is in config, skip.
			}

			// Template not in config - check if it has any jobs.
			hasJobs, err := st.HasAnyJobs(ctx, dbTmpl.ID)
			if err != nil {
				log.WithError(err).WithField("template", dbTmpl.ID).Warn("Failed to check jobs for orphaned template")

				continue
			}

			if !hasJobs {
				// No jobs, safe to delete.
				if err := st.DeleteJobTemplate(ctx, dbTmpl.ID); err != nil {
					log.WithError(err).WithField("template", dbTmpl.ID).Warn("Failed to delete orphaned template")
				} else {
					log.WithFields(logrus.Fields{
						"group":    groupCfg.ID,
						"template": dbTmpl.ID,
						"name":     dbTmpl.Name,
					}).Info("Deleted orphaned template with no jobs")
				}

				continue
			}

			// Has jobs - mark as not in config if not already.
			if dbTmpl.InConfig {
				if err := st.UpdateTemplateInConfig(ctx, dbTmpl.ID, false); err != nil {
					log.WithError(err).WithField("template", dbTmpl.ID).Warn("Failed to mark template as not in config")
				} else {
					log.WithFields(logrus.Fields{
						"group":    groupCfg.ID,
						"template": dbTmpl.ID,
						"name":     dbTmpl.Name,
					}).Info("Marked template as not in config (has job history)")
				}
			}
		}
	}

	return nil
}
