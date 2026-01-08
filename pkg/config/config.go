package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for dispatchoor.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	GitHub     GitHubConfig     `yaml:"github"`
	Dispatcher DispatcherConfig `yaml:"dispatcher"`
	Auth       AuthConfig       `yaml:"auth"`
	History    HistoryConfig    `yaml:"history"`
	Groups     GroupsConfig     `yaml:"groups"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Listen      string   `yaml:"listen"`
	CORSOrigins []string `yaml:"cors_origins"`
}

// DatabaseConfig contains database connection settings.
type DatabaseConfig struct {
	Driver   string         `yaml:"driver"`
	SQLite   SQLiteConfig   `yaml:"sqlite"`
	Postgres PostgresConfig `yaml:"postgres"`
}

// SQLiteConfig contains SQLite-specific settings.
type SQLiteConfig struct {
	Path string `yaml:"path"`
}

// PostgresConfig contains PostgreSQL-specific settings.
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
}

// GitHubConfig contains GitHub API settings.
type GitHubConfig struct {
	Token           string        `yaml:"token"`
	PollInterval    time.Duration `yaml:"poll_interval"`
	RateLimitBuffer int           `yaml:"rate_limit_buffer"`
}

// DispatcherConfig contains dispatch loop settings.
type DispatcherConfig struct {
	Enabled          bool          `yaml:"enabled"`
	Interval         time.Duration `yaml:"interval"`
	TrackingInterval time.Duration `yaml:"tracking_interval"`
}

// AuthConfig contains authentication settings.
type AuthConfig struct {
	SessionTTL time.Duration    `yaml:"session_ttl"`
	Basic      BasicAuthConfig  `yaml:"basic"`
	GitHub     GitHubAuthConfig `yaml:"github"`
}

// BasicAuthConfig contains basic auth settings.
type BasicAuthConfig struct {
	Enabled bool       `yaml:"enabled"`
	Users   []UserAuth `yaml:"users"`
}

// UserAuth represents a user configured for basic auth.
type UserAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Role     string `yaml:"role"`
}

// GitHubAuthConfig contains GitHub OAuth settings.
type GitHubAuthConfig struct {
	Enabled         bool              `yaml:"enabled"`
	ClientID        string            `yaml:"client_id"`
	ClientSecret    string            `yaml:"client_secret"`
	RedirectURL     string            `yaml:"redirect_url"`
	OrgRoleMapping  map[string]string `yaml:"org_role_mapping"`
	UserRoleMapping map[string]string `yaml:"user_role_mapping"`
}

// HistoryConfig contains job history retention settings.
type HistoryConfig struct {
	RetentionDays   int           `yaml:"retention_days"`   // default 30, -1 to disable
	CleanupInterval time.Duration `yaml:"cleanup_interval"` // default 1h
}

// GroupsConfig contains all group configurations.
type GroupsConfig struct {
	GitHub []Group `yaml:"github"`
}

// Group represents a runner pool and its associated workflow dispatch jobs.
type Group struct {
	ID                             string                     `yaml:"id"`
	Name                           string                     `yaml:"name"`
	Description                    string                     `yaml:"description"`
	RunnerLabels                   []string                   `yaml:"runner_labels"`
	WorkflowDispatchTemplates      []WorkflowDispatchTemplate `yaml:"workflow_dispatch_templates"`
	WorkflowDispatchTemplatesFiles []string                   `yaml:"workflow_dispatch_templates_files"`
}

// WorkflowDispatchTemplate represents a workflow dispatch template configuration.
type WorkflowDispatchTemplate struct {
	ID         string            `yaml:"id"`
	Name       string            `yaml:"name"`
	Owner      string            `yaml:"owner"`
	Repo       string            `yaml:"repo"`
	WorkflowID string            `yaml:"workflow_id"`
	Ref        string            `yaml:"ref"`
	Inputs     map[string]string `yaml:"inputs"`
	Labels     map[string]string `yaml:"labels"`
}

// Load reads and parses configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables.
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Load templates from external files.
	configDir := filepath.Dir(path)
	if err := loadTemplateFiles(&cfg, configDir); err != nil {
		return nil, fmt.Errorf("loading template files: %w", err)
	}

	// Apply defaults.
	applyDefaults(&cfg)

	// Validate configuration.
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// loadTemplateFiles loads workflow dispatch templates from external files.
func loadTemplateFiles(cfg *Config, configDir string) error {
	for i := range cfg.Groups.GitHub {
		group := &cfg.Groups.GitHub[i]

		for _, templateFile := range group.WorkflowDispatchTemplatesFiles {
			// Resolve path relative to config file directory.
			templatePath := templateFile
			if !filepath.IsAbs(templatePath) {
				templatePath = filepath.Join(configDir, templatePath)
			}

			// Read and parse template file.
			data, err := os.ReadFile(templatePath)
			if err != nil {
				return fmt.Errorf("reading template file %s for group %s: %w",
					templateFile, group.ID, err)
			}

			// Expand environment variables.
			expanded := expandEnvVars(string(data))

			var templates []WorkflowDispatchTemplate
			if err := yaml.Unmarshal([]byte(expanded), &templates); err != nil {
				return fmt.Errorf("parsing template file %s for group %s: %w",
					templateFile, group.ID, err)
			}

			// Append templates from file to any inline templates.
			group.WorkflowDispatchTemplates = append(group.WorkflowDispatchTemplates, templates...)
		}
	}

	return nil
}

// expandEnvVars replaces ${VAR} and $VAR patterns with environment variable values.
func expandEnvVars(s string) string {
	// Match ${VAR} pattern.
	re := regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	s = re.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}

		return match
	})

	// Match $VAR pattern (only at word boundaries).
	re = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
	s = re.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[1:]
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}

		return match
	})

	return s
}

// applyDefaults sets default values for unset configuration fields.
func applyDefaults(cfg *Config) {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":9090"
	}

	if cfg.Database.Driver == "" {
		cfg.Database.Driver = "sqlite"
	}

	if cfg.Database.SQLite.Path == "" {
		cfg.Database.SQLite.Path = "./dispatchoor.db"
	}

	if cfg.Database.Postgres.Port == 0 {
		cfg.Database.Postgres.Port = 5432
	}

	if cfg.Database.Postgres.SSLMode == "" {
		cfg.Database.Postgres.SSLMode = "disable"
	}

	if cfg.GitHub.PollInterval == 0 {
		cfg.GitHub.PollInterval = 60 * time.Second
	}

	if cfg.GitHub.RateLimitBuffer == 0 {
		cfg.GitHub.RateLimitBuffer = 100
	}

	if cfg.Dispatcher.Interval == 0 {
		cfg.Dispatcher.Interval = 30 * time.Second
	}

	if cfg.Dispatcher.TrackingInterval == 0 {
		cfg.Dispatcher.TrackingInterval = 30 * time.Second
	}

	if cfg.Auth.SessionTTL == 0 {
		cfg.Auth.SessionTTL = 24 * time.Hour
	}

	if cfg.History.RetentionDays == 0 {
		cfg.History.RetentionDays = 30
	}

	if cfg.History.CleanupInterval == 0 {
		cfg.History.CleanupInterval = time.Hour
	}

	// Set default refs for workflow dispatch templates.
	for i := range cfg.Groups.GitHub {
		for j := range cfg.Groups.GitHub[i].WorkflowDispatchTemplates {
			if cfg.Groups.GitHub[i].WorkflowDispatchTemplates[j].Ref == "" {
				cfg.Groups.GitHub[i].WorkflowDispatchTemplates[j].Ref = "main"
			}
		}
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	// Validate database config.
	switch c.Database.Driver {
	case "sqlite":
		if c.Database.SQLite.Path == "" {
			return fmt.Errorf("sqlite.path is required when driver is sqlite")
		}
	case "postgres":
		if c.Database.Postgres.Host == "" {
			return fmt.Errorf("postgres.host is required when driver is postgres")
		}

		if c.Database.Postgres.Database == "" {
			return fmt.Errorf("postgres.database is required when driver is postgres")
		}
	default:
		return fmt.Errorf("unsupported database driver: %s", c.Database.Driver)
	}

	// Validate GitHub config.
	if c.GitHub.Token == "" {
		return fmt.Errorf("github.token is required")
	}

	// Validate auth config.
	if !c.Auth.Basic.Enabled && !c.Auth.GitHub.Enabled {
		return fmt.Errorf("at least one auth method (basic or github) must be enabled")
	}

	if c.Auth.GitHub.Enabled {
		if c.Auth.GitHub.ClientID == "" {
			return fmt.Errorf("auth.github.client_id is required when github auth is enabled")
		}

		if c.Auth.GitHub.ClientSecret == "" {
			return fmt.Errorf("auth.github.client_secret is required when github auth is enabled")
		}
	}

	// Validate groups.
	groupIDs := make(map[string]bool)
	jobIDs := make(map[string]bool)

	for _, group := range c.Groups.GitHub {
		if group.ID == "" {
			return fmt.Errorf("group id is required")
		}

		if groupIDs[group.ID] {
			return fmt.Errorf("duplicate group id: %s", group.ID)
		}

		groupIDs[group.ID] = true

		if len(group.RunnerLabels) == 0 {
			return fmt.Errorf("group %s: runner_labels is required", group.ID)
		}

		for _, tmpl := range group.WorkflowDispatchTemplates {
			if tmpl.ID == "" {
				return fmt.Errorf("group %s: workflow_dispatch_template id is required", group.ID)
			}

			if jobIDs[tmpl.ID] {
				return fmt.Errorf("duplicate workflow_dispatch_template id: %s", tmpl.ID)
			}

			jobIDs[tmpl.ID] = true

			if tmpl.Owner == "" {
				return fmt.Errorf("template %s: owner is required", tmpl.ID)
			}

			if tmpl.Repo == "" {
				return fmt.Errorf("template %s: repo is required", tmpl.ID)
			}

			if tmpl.WorkflowID == "" {
				return fmt.Errorf("template %s: workflow_id is required", tmpl.ID)
			}
		}
	}

	return nil
}

// GetDSN returns the database connection string.
func (c *Config) GetDSN() string {
	switch c.Database.Driver {
	case "sqlite":
		return c.Database.SQLite.Path
	case "postgres":
		return fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			c.Database.Postgres.Host,
			c.Database.Postgres.Port,
			c.Database.Postgres.User,
			c.Database.Postgres.Password,
			c.Database.Postgres.Database,
			c.Database.Postgres.SSLMode,
		)
	default:
		return ""
	}
}

// String returns a sanitized string representation of the config (no secrets).
func (c *Config) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Server: listen=%s\n", c.Server.Listen))
	sb.WriteString(fmt.Sprintf("Database: driver=%s\n", c.Database.Driver))
	sb.WriteString(fmt.Sprintf("GitHub: poll_interval=%s\n", c.GitHub.PollInterval))
	sb.WriteString(fmt.Sprintf("Dispatcher: enabled=%t interval=%s tracking_interval=%s\n",
		c.Dispatcher.Enabled, c.Dispatcher.Interval, c.Dispatcher.TrackingInterval))
	sb.WriteString(fmt.Sprintf("Auth: basic=%t github=%t\n",
		c.Auth.Basic.Enabled, c.Auth.GitHub.Enabled))
	sb.WriteString(fmt.Sprintf("Groups: %d\n", len(c.Groups.GitHub)))

	return sb.String()
}
