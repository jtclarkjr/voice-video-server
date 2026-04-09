package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type supabaseAuthContextKey string

const supabaseAuthUserContextKey supabaseAuthContextKey = "supabaseAuthUser"

// SupabaseAuthConfigured reports whether the server can validate Supabase access tokens.
func SupabaseAuthConfigured() bool {
	return strings.TrimSpace(os.Getenv("SUPABASE_URL")) != "" &&
		strings.TrimSpace(os.Getenv("SUPABASE_SECRET_KEY")) != ""
}

// RequireSupabaseAuth blocks requests that do not include a valid Supabase access token.
func RequireSupabaseAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			accessToken := extractSupabaseAccessToken(r)
			if accessToken == "" {
				http.Error(w, "missing access token", http.StatusUnauthorized)
				return
			}

			authUser, err := validateSupabaseAccessToken(r.Context(), accessToken)
			if err != nil || authUser == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), supabaseAuthUserContextKey, authUser)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func SupabaseAuthUserFromContext(ctx context.Context) (*SupabaseAuthUser, bool) {
	authUser, ok := ctx.Value(supabaseAuthUserContextKey).(*SupabaseAuthUser)
	return authUser, ok
}

func extractSupabaseAccessToken(r *http.Request) string {
	if token := extractBearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}

	if token := strings.TrimSpace(r.URL.Query().Get("access_token")); token != "" {
		return token
	}

	if token := strings.TrimSpace(r.URL.Query().Get("auth_token")); token != "" {
		return token
	}

	for _, cookie := range r.Cookies() {
		if !strings.HasSuffix(cookie.Name, "-auth-token") {
			continue
		}

		if token := extractAccessTokenFromCookie(cookie.Value); token != "" {
			return token
		}
	}

	return ""
}

func extractBearerToken(headerValue string) string {
	if headerValue == "" {
		return ""
	}

	parts := strings.Fields(headerValue)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

func extractAccessTokenFromCookie(cookieValue string) string {
	decodedValue, err := url.QueryUnescape(cookieValue)
	if err != nil {
		return ""
	}

	var tokens []string
	if err := json.Unmarshal([]byte(decodedValue), &tokens); err != nil {
		return ""
	}

	if len(tokens) == 0 {
		return ""
	}

	return strings.TrimSpace(tokens[0])
}
