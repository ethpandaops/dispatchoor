package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
)

func TestValidateSessionGitHubToken(t *testing.T) {
	ctx := context.Background()
	log := logrus.New()
	log.SetOutput(io.Discard)

	userCalls := 0
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("Authorization") == "Bearer gho_outsider" && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"id": 2, "login": "outsider"}`))
		case r.Header.Get("Authorization") == "Bearer gho_outsider" && r.URL.Path == "/user/orgs":
			_, _ = w.Write([]byte(`[{"login": "other"}]`))
		case r.Header.Get("Authorization") != "Bearer gho_member":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/user":
			userCalls++
			_, _ = w.Write([]byte(`{"id": 1, "login": "member"}`))
		case r.URL.Path == "/user/orgs":
			_, _ = w.Write([]byte(`[{"login": "ethpandaops"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gh.Close()

	githubUserURL = gh.URL + "/user"
	githubOrgsURL = gh.URL + "/user/orgs"

	st := store.NewSQLiteStore(log, filepath.Join(t.TempDir(), "test.db"))
	if err := st.Start(ctx); err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer func() { _ = st.Stop() }()

	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	cfg := &config.Config{}
	cfg.Auth.GitHub.Enabled = true
	cfg.Auth.GitHub.OrgRoleMapping = map[string]string{"ethpandaops": "admin"}

	svc := NewService(log, cfg, st)

	for range 2 {
		user, err := svc.ValidateSession(ctx, "gho_member")
		if err != nil {
			t.Fatalf("Expected GitHub token to authenticate: %v", err)
		}

		if user.Username != "member" || user.Role != store.RoleAdmin || user.AuthProvider != store.AuthProviderGitHub {
			t.Fatalf("Unexpected user: %+v", user)
		}
	}

	if userCalls != 1 {
		t.Errorf("Expected GitHub to be queried once, got %d", userCalls)
	}

	if _, err := svc.ValidateSession(ctx, "gho_outsider"); err == nil {
		t.Error("Expected token of user outside role mappings to be rejected")
	}

	if _, err := svc.ValidateSession(ctx, "gho_invalid"); err == nil {
		t.Error("Expected invalid GitHub token to be rejected")
	}

	if _, err := svc.ValidateSession(ctx, "not-a-github-token"); err == nil {
		t.Error("Expected unknown session token to be rejected")
	}

	cfg.Auth.GitHub.Enabled = false

	svc = NewService(log, cfg, st)
	if _, err := svc.ValidateSession(ctx, "gho_member"); err == nil {
		t.Error("Expected GitHub token to be rejected when GitHub auth is disabled")
	}

	cfg.Auth.GitHub.Enabled = true

	svc = NewService(log, cfg, st)
	userCalls = 0

	for i := range githubTokenLookupBurst + 5 {
		_, _ = svc.ValidateSession(ctx, fmt.Sprintf("gho_random%d", i))
	}

	if _, err := svc.ValidateSession(ctx, "gho_member"); err == nil {
		t.Error("Expected uncached GitHub token to be rejected once lookups are rate limited")
	}

	if userCalls != 0 {
		t.Errorf("Expected no member lookups while rate limited, got %d", userCalls)
	}
}
