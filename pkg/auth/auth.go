package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

// Service defines the interface for authentication operations.
type Service interface {
	Start(ctx context.Context) error
	Stop() error

	// Authentication.
	AuthenticateBasic(ctx context.Context, username, password string) (*store.User, string, error)
	AuthenticateGitHub(ctx context.Context, code string) (*store.User, string, error)
	ValidateSession(ctx context.Context, token string) (*store.User, error)
	Logout(ctx context.Context, token string) error

	// Authorization.
	HasRole(user *store.User, role store.Role) bool
	IsAdmin(user *store.User) bool

	// GitHub OAuth URL.
	GetGitHubAuthURL(state string) string

	// OAuth State (CSRF protection).
	CreateOAuthState(ctx context.Context) (string, error)
	ValidateOAuthState(ctx context.Context, state string) error

	// Auth Code (one-time exchange).
	CreateAuthCode(ctx context.Context, userID string) (string, error)
	ExchangeAuthCode(ctx context.Context, code string) (*store.User, string, error)
}

// service implements Service.
type service struct {
	log        logrus.FieldLogger
	cfg        *config.Config
	store      store.Store
	sessionTTL time.Duration
}

// Ensure service implements Service.
var _ Service = (*service)(nil)

// NewService creates a new auth service.
func NewService(log logrus.FieldLogger, cfg *config.Config, st store.Store) Service {
	return &service{
		log:        log.WithField("component", "auth"),
		cfg:        cfg,
		store:      st,
		sessionTTL: cfg.Auth.SessionTTL,
	}
}

// Start initializes the auth service.
func (s *service) Start(ctx context.Context) error {
	s.log.Info("Starting auth service")

	// Sync basic auth users from config.
	if s.cfg.Auth.Basic.Enabled {
		if err := s.syncBasicAuthUsers(ctx); err != nil {
			return fmt.Errorf("syncing basic auth users: %w", err)
		}
	}

	// Start session cleanup goroutine.
	go s.cleanupSessions(ctx)

	return nil
}

// Stop shuts down the auth service.
func (s *service) Stop() error {
	s.log.Info("Stopping auth service")

	return nil
}

// syncBasicAuthUsers creates or updates users from the basic auth config.
func (s *service) syncBasicAuthUsers(ctx context.Context) error {
	for _, userCfg := range s.cfg.Auth.Basic.Users {
		existing, err := s.store.GetUserByUsername(ctx, userCfg.Username)
		if err != nil {
			return fmt.Errorf("checking user %s: %w", userCfg.Username, err)
		}

		// Hash the password.
		hash, err := bcrypt.GenerateFromPassword([]byte(userCfg.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hashing password for %s: %w", userCfg.Username, err)
		}

		role := store.Role(userCfg.Role)
		if role == "" {
			role = store.RoleReadOnly
		}

		now := time.Now()

		if existing == nil {
			// Create new user.
			user := &store.User{
				ID:           uuid.New().String(),
				Username:     userCfg.Username,
				PasswordHash: string(hash),
				Role:         role,
				AuthProvider: store.AuthProviderBasic,
				CreatedAt:    now,
				UpdatedAt:    now,
			}

			if err := s.store.CreateUser(ctx, user); err != nil {
				return fmt.Errorf("creating user %s: %w", userCfg.Username, err)
			}

			s.log.WithField("username", userCfg.Username).Info("Created basic auth user")
		} else {
			// Update existing user.
			existing.PasswordHash = string(hash)
			existing.Role = role
			existing.UpdatedAt = now

			if err := s.store.UpdateUser(ctx, existing); err != nil {
				return fmt.Errorf("updating user %s: %w", userCfg.Username, err)
			}

			s.log.WithField("username", userCfg.Username).Debug("Updated basic auth user")
		}
	}

	return nil
}

// AuthenticateBasic authenticates a user with username and password.
func (s *service) AuthenticateBasic(ctx context.Context, username, password string) (*store.User, string, error) {
	if !s.cfg.Auth.Basic.Enabled {
		return nil, "", fmt.Errorf("basic auth is not enabled")
	}

	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, "", fmt.Errorf("getting user: %w", err)
	}

	if user == nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	if user.AuthProvider != store.AuthProviderBasic {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	// Verify password.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", fmt.Errorf("invalid credentials")
	}

	// Create session.
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, "", fmt.Errorf("creating session: %w", err)
	}

	s.log.WithField("username", username).Info("User authenticated via basic auth")

	return user, token, nil
}

// AuthenticateGitHub authenticates a user with a GitHub OAuth code.
func (s *service) AuthenticateGitHub(ctx context.Context, code string) (*store.User, string, error) {
	if !s.cfg.Auth.GitHub.Enabled {
		return nil, "", fmt.Errorf("github auth is not enabled")
	}

	// Exchange code for access token.
	accessToken, err := s.exchangeGitHubCode(ctx, code)
	if err != nil {
		return nil, "", fmt.Errorf("exchanging github code: %w", err)
	}

	// Get GitHub user info.
	githubUser, err := s.getGitHubUser(ctx, accessToken)
	if err != nil {
		return nil, "", fmt.Errorf("getting github user: %w", err)
	}

	// Determine role based on user or org membership.
	// Role mappings also control access - if not in any mapping, login is rejected.
	var role store.Role

	var authorized bool

	// Check individual user mapping first (takes priority, case-insensitive).
	usernameLower := strings.ToLower(githubUser.Login)

	for user, mappedRole := range s.cfg.Auth.GitHub.UserRoleMapping {
		if strings.ToLower(user) == usernameLower {
			role = store.Role(mappedRole)
			authorized = true

			break
		}
	}

	// If no user mapping found, check org-based mapping.
	if !authorized && len(s.cfg.Auth.GitHub.OrgRoleMapping) > 0 {
		orgs, err := s.getGitHubUserOrgs(ctx, accessToken)
		if err != nil {
			return nil, "", fmt.Errorf("getting github orgs: %w", err)
		}

		for _, org := range orgs {
			if mappedRole, ok := s.cfg.Auth.GitHub.OrgRoleMapping[org]; ok {
				role = store.Role(mappedRole)
				authorized = true

				break
			}
		}
	}

	// Reject if user is not in any role mapping.
	if !authorized {
		return nil, "", fmt.Errorf("user not authorized: not in any role mapping")
	}

	// Get or create user.
	user, err := s.store.GetUserByGitHubID(ctx, githubUser.ID)
	if err != nil {
		return nil, "", fmt.Errorf("getting user by github id: %w", err)
	}

	now := time.Now()

	if user == nil {
		// Create new user.
		user = &store.User{
			ID:           uuid.New().String(),
			Username:     githubUser.Login,
			Role:         role,
			AuthProvider: store.AuthProviderGitHub,
			GitHubID:     githubUser.ID,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := s.store.CreateUser(ctx, user); err != nil {
			return nil, "", fmt.Errorf("creating user: %w", err)
		}

		s.log.WithField("username", user.Username).Info("Created GitHub user")
	} else {
		// Update user role if needed.
		user.Role = role
		user.Username = githubUser.Login
		user.UpdatedAt = now

		if err := s.store.UpdateUser(ctx, user); err != nil {
			return nil, "", fmt.Errorf("updating user: %w", err)
		}
	}

	// Create session.
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, "", fmt.Errorf("creating session: %w", err)
	}

	s.log.WithField("username", user.Username).Info("User authenticated via GitHub")

	return user, token, nil
}

// ValidateSession validates a session token and returns the associated user.
func (s *service) ValidateSession(ctx context.Context, token string) (*store.User, error) {
	tokenHash := hashToken(token)

	session, err := s.store.GetSessionByToken(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}

	if session == nil {
		return nil, fmt.Errorf("session not found")
	}

	if time.Now().After(session.ExpiresAt) {
		// Delete expired session.
		_ = s.store.DeleteSession(ctx, session.ID)

		return nil, fmt.Errorf("session expired")
	}

	user, err := s.store.GetUser(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}

	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	return user, nil
}

// Logout invalidates a session.
func (s *service) Logout(ctx context.Context, token string) error {
	tokenHash := hashToken(token)

	session, err := s.store.GetSessionByToken(ctx, tokenHash)
	if err != nil {
		return fmt.Errorf("getting session: %w", err)
	}

	if session == nil {
		return nil
	}

	if err := s.store.DeleteSession(ctx, session.ID); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return nil
}

// HasRole checks if a user has a specific role.
func (s *service) HasRole(user *store.User, role store.Role) bool {
	if user == nil {
		return false
	}

	// Admin role has all permissions.
	if user.Role == store.RoleAdmin {
		return true
	}

	return user.Role == role
}

// IsAdmin checks if a user is an admin.
func (s *service) IsAdmin(user *store.User) bool {
	return s.HasRole(user, store.RoleAdmin)
}

// GetGitHubAuthURL returns the GitHub OAuth authorization URL.
func (s *service) GetGitHubAuthURL(state string) string {
	return fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&state=%s&scope=read:org",
		s.cfg.Auth.GitHub.ClientID,
		state,
	)
}

// createSession creates a new session for a user.
func (s *service) createSession(ctx context.Context, user *store.User) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}

	now := time.Now()

	session := &store.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		TokenHash: hashToken(token),
		ExpiresAt: now.Add(s.sessionTTL),
		CreatedAt: now,
	}

	if err := s.store.CreateSession(ctx, session); err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}

	return token, nil
}

// cleanupSessions periodically removes expired sessions, OAuth states, and auth codes.
func (s *service) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.store.DeleteExpiredSessions(ctx); err != nil {
				s.log.WithError(err).Error("Failed to cleanup expired sessions")
			}

			if err := s.store.DeleteExpiredOAuthStates(ctx); err != nil {
				s.log.WithError(err).Error("Failed to cleanup expired oauth states")
			}

			if err := s.store.DeleteExpiredAuthCodes(ctx); err != nil {
				s.log.WithError(err).Error("Failed to cleanup expired auth codes")
			}
		}
	}
}

// generateToken generates a cryptographically secure random token.
func generateToken() (string, error) {
	bytes := make([]byte, 32)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(bytes), nil
}

// hashToken hashes a token for storage.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))

	return hex.EncodeToString(hash[:])
}

// GitHubUser represents a GitHub user profile.
type GitHubUser struct {
	ID    string
	Login string
}

const (
	oauthStateTTL = 5 * time.Minute
	authCodeTTL   = 30 * time.Second
)

// CreateOAuthState generates a random state token for CSRF protection.
func (s *service) CreateOAuthState(ctx context.Context) (string, error) {
	stateBytes := make([]byte, 32)

	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	state := base64.URLEncoding.EncodeToString(stateBytes)
	now := time.Now()

	oauthState := &store.OAuthState{
		State:     state,
		ExpiresAt: now.Add(oauthStateTTL),
		CreatedAt: now,
	}

	if err := s.store.CreateOAuthState(ctx, oauthState); err != nil {
		return "", fmt.Errorf("storing oauth state: %w", err)
	}

	return state, nil
}

// ValidateOAuthState validates and consumes an OAuth state token.
func (s *service) ValidateOAuthState(ctx context.Context, state string) error {
	oauthState, err := s.store.GetOAuthState(ctx, state)
	if err != nil {
		return fmt.Errorf("getting oauth state: %w", err)
	}

	if oauthState == nil {
		return fmt.Errorf("invalid oauth state")
	}

	// Delete the state (single use).
	if err := s.store.DeleteOAuthState(ctx, state); err != nil {
		s.log.WithError(err).Error("Failed to delete oauth state")
	}

	if time.Now().After(oauthState.ExpiresAt) {
		return fmt.Errorf("oauth state expired")
	}

	return nil
}

// CreateAuthCode generates a one-time authorization code for token exchange.
func (s *service) CreateAuthCode(ctx context.Context, userID string) (string, error) {
	codeBytes := make([]byte, 32)

	if _, err := rand.Read(codeBytes); err != nil {
		return "", fmt.Errorf("generating code: %w", err)
	}

	code := base64.URLEncoding.EncodeToString(codeBytes)
	now := time.Now()

	authCode := &store.AuthCode{
		Code:      code,
		UserID:    userID,
		ExpiresAt: now.Add(authCodeTTL),
		CreatedAt: now,
	}

	if err := s.store.CreateAuthCode(ctx, authCode); err != nil {
		return "", fmt.Errorf("storing auth code: %w", err)
	}

	return code, nil
}

// ExchangeAuthCode exchanges a one-time authorization code for a session token.
func (s *service) ExchangeAuthCode(ctx context.Context, code string) (*store.User, string, error) {
	authCode, err := s.store.GetAuthCode(ctx, code)
	if err != nil {
		return nil, "", fmt.Errorf("getting auth code: %w", err)
	}

	if authCode == nil {
		return nil, "", fmt.Errorf("invalid authorization code")
	}

	// Delete the code (single use).
	if err := s.store.DeleteAuthCode(ctx, code); err != nil {
		s.log.WithError(err).Error("Failed to delete auth code")
	}

	if time.Now().After(authCode.ExpiresAt) {
		return nil, "", fmt.Errorf("authorization code expired")
	}

	user, err := s.store.GetUser(ctx, authCode.UserID)
	if err != nil {
		return nil, "", fmt.Errorf("getting user: %w", err)
	}

	if user == nil {
		return nil, "", fmt.Errorf("user not found")
	}

	// Create session.
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, "", fmt.Errorf("creating session: %w", err)
	}

	s.log.WithField("username", user.Username).Info("Auth code exchanged for session")

	return user, token, nil
}
