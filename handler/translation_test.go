package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleTranslationClientSecretRequiresAuthenticatedUser(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	req := httptest.NewRequest(http.MethodPost, "/translation/client-secret", strings.NewReader(`{"targetLanguage":"es"}`))
	rec := httptest.NewRecorder()

	HandleTranslationClientSecret(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHandleTranslationClientSecretRequiresOpenAIAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	req := authenticatedTranslationRequest(`{"targetLanguage":"es"}`, false)
	rec := httptest.NewRecorder()

	HandleTranslationClientSecret(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHandleTranslationClientSecretRejectsUnsupportedLanguage(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	req := authenticatedTranslationRequest(`{"targetLanguage":"xx"}`, false)
	rec := httptest.NewRecorder()

	HandleTranslationClientSecret(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleTranslationClientSecretRejectsAnonymousUsers(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	req := authenticatedTranslationRequest(`{"targetLanguage":"es"}`, true)
	rec := httptest.NewRecorder()

	HandleTranslationClientSecret(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHandleTranslationClientSecretProxiesOpenAIResponse(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer openai-key" {
			t.Fatalf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("OpenAI-Safety-Identifier") != hashSafetyIdentifier("user-123") {
			t.Fatalf("unexpected safety identifier: %q", r.Header.Get("OpenAI-Safety-Identifier"))
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"client-secret","expires_at":123}`))
	}))
	defer server.Close()

	restoreOpenAITestClient(server)
	defer restoreOpenAITestClient(nil)

	req := authenticatedTranslationRequest(`{"targetLanguage":"es"}`, false)
	rec := httptest.NewRecorder()

	HandleTranslationClientSecret(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"value":"client-secret"`) {
		t.Fatalf("expected proxied client secret response, got %s", rec.Body.String())
	}

	session, ok := receivedBody["session"].(map[string]any)
	if !ok {
		t.Fatalf("expected session object, got %#v", receivedBody["session"])
	}
	if session["model"] != openAIRealtimeTranslationModel {
		t.Fatalf("unexpected model: %#v", session["model"])
	}
	audio := session["audio"].(map[string]any)
	output := audio["output"].(map[string]any)
	if output["language"] != "es" {
		t.Fatalf("unexpected language: %#v", output["language"])
	}
}

func TestHandleTranslationClientSecretSanitizesUpstreamErrors(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "raw upstream error", http.StatusInternalServerError)
	}))
	defer server.Close()

	restoreOpenAITestClient(server)
	defer restoreOpenAITestClient(nil)

	req := authenticatedTranslationRequest(`{"targetLanguage":"es"}`, false)
	rec := httptest.NewRecorder()

	HandleTranslationClientSecret(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
	if strings.Contains(rec.Body.String(), "raw upstream error") {
		t.Fatalf("expected sanitized error, got %q", rec.Body.String())
	}
}

func authenticatedTranslationRequest(body string, anonymous bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/translation/client-secret", strings.NewReader(body))
	user := &SupabaseAuthUser{
		ID:          "user-123",
		Email:       "jane@example.com",
		DisplayName: "Jane",
		IsAnonymous: anonymous,
	}
	return req.WithContext(context.WithValue(req.Context(), supabaseAuthUserContextKey, user))
}

func restoreOpenAITestClient(server *httptest.Server) {
	if server == nil {
		openAITranslationClientSecretsURL = "https://api.openai.com/v1/realtime/translations/client_secrets"
		openAITranslationHTTPClient = &http.Client{Timeout: 10_000_000_000}
		return
	}
	openAITranslationClientSecretsURL = server.URL
	openAITranslationHTTPClient = server.Client()
}
