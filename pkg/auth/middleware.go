package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/ethpandaops/dispatchoor/pkg/store"
)

// Context keys for user information.
type contextKey string

const (
	userContextKey contextKey = "user"
)

// UserFromContext retrieves the authenticated user from the context.
func UserFromContext(ctx context.Context) *store.User {
	user, ok := ctx.Value(userContextKey).(*store.User)
	if !ok {
		return nil
	}

	return user
}

// ContextWithUser adds a user to the context.
func ContextWithUser(ctx context.Context, user *store.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// AuthMiddleware creates middleware that validates session tokens.
func AuthMiddleware(authSvc Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)

				return
			}

			user, err := authSvc.ValidateSession(r.Context(), token)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)

				return
			}

			// Add user to context.
			ctx := ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuthMiddleware creates middleware that validates session tokens but allows unauthenticated requests.
func OptionalAuthMiddleware(authSvc Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token != "" {
				user, err := authSvc.ValidateSession(r.Context(), token)
				if err == nil && user != nil {
					ctx := ContextWithUser(r.Context(), user)
					r = r.WithContext(ctx)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole creates middleware that requires a specific role.
func RequireRole(role store.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)

				return
			}

			// Admin has all permissions.
			if user.Role == store.RoleAdmin {
				next.ServeHTTP(w, r)

				return
			}

			if user.Role != role {
				http.Error(w, "Forbidden", http.StatusForbidden)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin creates middleware that requires admin role.
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRole(store.RoleAdmin)
}

// extractToken extracts the bearer token from the request.
func extractToken(r *http.Request) string {
	// Check Authorization header.
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Support both "Bearer <token>" and "<token>" formats.
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}

		return authHeader
	}

	// Check cookie.
	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Check query parameter (for WebSocket connections).
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}
