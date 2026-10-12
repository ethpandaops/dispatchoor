package auth

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethpandaops/dispatchoor/pkg/config"
	"github.com/ethpandaops/dispatchoor/pkg/store"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) (*service, store.Store, *store.User) {
	t.Helper()

	ctx := context.Background()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	st := store.NewSQLiteStore(log, filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, st.Start(ctx))
	require.NoError(t, st.Migrate(ctx))
	t.Cleanup(func() { _ = st.Stop() })

	user := &store.User{
		ID:           "user-1",
		Username:     "alice",
		Role:         store.RoleReadOnly,
		AuthProvider: store.AuthProviderGitHub,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, st.CreateUser(ctx, user))

	svc := &service{log: log, cfg: &config.Config{}, store: st, sessionTTL: time.Hour}

	return svc, st, user
}

func TestAPITokenLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, st, user := newTestService(t)

	token, plaintext, err := svc.CreateAPIToken(ctx, user, "claude", 24*time.Hour)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(plaintext, apiTokenPrefix))
	require.Equal(t, plaintext[:apiTokenDisplayLen], token.TokenPrefix)
	require.NotEqual(t, plaintext, token.TokenHash)

	got, err := svc.ValidateSession(ctx, plaintext)
	require.NoError(t, err)
	require.Equal(t, user.ID, got.ID)
	require.Equal(t, store.RoleReadOnly, got.Role)

	tokens, err := svc.ListAPITokens(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.NotNil(t, tokens[0].LastUsedAt)

	_, err = svc.ValidateSession(ctx, plaintext+"x")
	require.Error(t, err)

	deleted, err := svc.DeleteAPIToken(ctx, token.ID, "other-user")
	require.NoError(t, err)
	require.False(t, deleted)

	deleted, err = svc.DeleteAPIToken(ctx, token.ID, user.ID)
	require.NoError(t, err)
	require.True(t, deleted)

	_, err = svc.ValidateSession(ctx, plaintext)
	require.Error(t, err)

	tokens, err = st.ListAPITokensByUser(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, tokens)
}

func TestAPITokenExpired(t *testing.T) {
	ctx := context.Background()
	svc, _, user := newTestService(t)

	_, plaintext, err := svc.CreateAPIToken(ctx, user, "short", -time.Minute)
	require.NoError(t, err)

	_, err = svc.ValidateSession(ctx, plaintext)
	require.ErrorContains(t, err, "expired")
}

func TestAPITokenFollowsUserRole(t *testing.T) {
	ctx := context.Background()
	svc, st, user := newTestService(t)

	_, plaintext, err := svc.CreateAPIToken(ctx, user, "claude", time.Hour)
	require.NoError(t, err)

	user.Role = store.RoleAdmin
	require.NoError(t, st.UpdateUser(ctx, user))

	got, err := svc.ValidateSession(ctx, plaintext)
	require.NoError(t, err)
	require.Equal(t, store.RoleAdmin, got.Role)

	require.NoError(t, st.DeleteUser(ctx, user.ID))

	_, err = svc.ValidateSession(ctx, plaintext)
	require.Error(t, err)
}
