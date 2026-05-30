package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"voice-video-server/db"

	"github.com/jackc/pgx/v5"
)

func TestHandleCreateTranslationSessionRequiresAuthenticatedUser(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := httptest.NewRequest(http.MethodPost, "/translation/sessions?lang=es", nil)
	rec := httptest.NewRecorder()

	HandleCreateTranslationSession(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHandleCreateTranslationSessionRejectsAnonymousUsers(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions?lang=es", "", true)
	rec := httptest.NewRecorder()

	HandleCreateTranslationSession(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHandleCreateTranslationSessionRequiresOpenAIAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	store := newFakeTranslationSessionStore()
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions?lang=es", "", false)
	rec := httptest.NewRecorder()

	HandleCreateTranslationSession(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHandleCreateTranslationSessionRequiresStore(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	translationSessions = dbTranslationSessionStore{}
	t.Cleanup(func() {
		translationSessions = dbTranslationSessionStore{}
	})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions?lang=es", "", false)
	rec := httptest.NewRecorder()

	HandleCreateTranslationSession(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), translationSessionStoreMessage) {
		t.Fatalf("expected store error, got %q", rec.Body.String())
	}
}

func TestHandleCreateTranslationSessionRejectsUnsupportedLanguage(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions?lang=ar", "", false)
	rec := httptest.NewRecorder()

	HandleCreateTranslationSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleCreateTranslationSessionPersistsSession(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions?lang=ru", "", false)
	rec := httptest.NewRecorder()

	HandleCreateTranslationSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if store.createdUserID != "user-123" {
		t.Fatalf("unexpected user id: %q", store.createdUserID)
	}
	if store.createdLang != "ru" {
		t.Fatalf("unexpected language: %q", store.createdLang)
	}

	var payload createTranslationSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.ID != "session-123" {
		t.Fatalf("unexpected id: %q", payload.ID)
	}
	if !payload.ExpiresAt.Equal(fixedTranslationNow.Add(translationSessionTTL)) {
		t.Fatalf("unexpected expiry: %s", payload.ExpiresAt)
	}
}

func TestHandleTranslationSessionOfferExchangesSDP(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	store.sessions["session-123"] = db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: "user-123",
		Lang:           "es",
		Status:         db.TranslationSessionStatusPending,
		ExpiresAt:      fixedTranslationNow.Add(time.Minute),
	}
	client := &fakeTranslationOfferClient{answer: "answer-sdp"}
	restoreTranslationTestDeps(t, store, client)

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions/session-123/offer", "offer-sdp", false)
	req.Header.Set("Content-Type", "application/sdp")
	rec := httptest.NewRecorder()

	HandleTranslationSessionOffer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "answer-sdp" {
		t.Fatalf("unexpected SDP answer: %q", rec.Body.String())
	}
	if client.apiKey != "openai-key" {
		t.Fatalf("unexpected api key: %q", client.apiKey)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one OpenAI request, got %d", len(client.requests))
	}
	request := client.requests[0]
	if request.Language != "es" {
		t.Fatalf("unexpected language: %q", request.Language)
	}
	if request.OfferSDP != "offer-sdp" {
		t.Fatalf("unexpected offer SDP: %q", request.OfferSDP)
	}
	if request.SafetyIdentifier != hashSafetyIdentifier("user-123") {
		t.Fatalf("unexpected safety identifier: %q", request.SafetyIdentifier)
	}
	if store.sessions["session-123"].Status != db.TranslationSessionStatusConnected {
		t.Fatalf("expected connected session, got %#v", store.sessions["session-123"])
	}
}

func TestHandleTranslationSessionOfferRejectsWrongOwner(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	store.sessions["session-123"] = db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: "other-user",
		Lang:           "es",
		Status:         db.TranslationSessionStatusPending,
		ExpiresAt:      fixedTranslationNow.Add(time.Minute),
	}
	client := &fakeTranslationOfferClient{answer: "answer-sdp"}
	restoreTranslationTestDeps(t, store, client)

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions/session-123/offer", "offer-sdp", false)
	rec := httptest.NewRecorder()

	HandleTranslationSessionOffer(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected no OpenAI requests, got %d", len(client.requests))
	}
}

func TestHandleTranslationSessionOfferRejectsEmptySDP(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions/session-123/offer", "   ", false)
	rec := httptest.NewRecorder()

	HandleTranslationSessionOffer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleTranslationSessionOfferRejectsExpiredSession(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	store.sessions["session-123"] = db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: "user-123",
		Lang:           "es",
		Status:         db.TranslationSessionStatusPending,
		ExpiresAt:      fixedTranslationNow.Add(-time.Second),
	}
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{answer: "answer-sdp"})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions/session-123/offer", "offer-sdp", false)
	rec := httptest.NewRecorder()

	HandleTranslationSessionOffer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if store.sessions["session-123"].Status != db.TranslationSessionStatusEnded {
		t.Fatalf("expected ended session, got %#v", store.sessions["session-123"])
	}
}

func TestHandleTranslationSessionOfferRejectsReusedSession(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	store.sessions["session-123"] = db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: "user-123",
		Lang:           "es",
		Status:         db.TranslationSessionStatusConnected,
		ExpiresAt:      fixedTranslationNow.Add(time.Minute),
	}
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{answer: "answer-sdp"})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions/session-123/offer", "offer-sdp", false)
	rec := httptest.NewRecorder()

	HandleTranslationSessionOffer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleTranslationSessionOfferSanitizesOpenAIErrors(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	store := newFakeTranslationSessionStore()
	store.sessions["session-123"] = db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: "user-123",
		Lang:           "es",
		Status:         db.TranslationSessionStatusPending,
		ExpiresAt:      fixedTranslationNow.Add(time.Minute),
	}
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{err: errors.New("raw upstream error")})

	req := authenticatedTranslationRequest(http.MethodPost, "/translation/sessions/session-123/offer", "offer-sdp", false)
	rec := httptest.NewRecorder()

	HandleTranslationSessionOffer(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
	if strings.Contains(rec.Body.String(), "raw upstream error") {
		t.Fatalf("expected sanitized error, got %q", rec.Body.String())
	}
	if store.sessions["session-123"].Status != db.TranslationSessionStatusFailed {
		t.Fatalf("expected failed session, got %#v", store.sessions["session-123"])
	}
}

func TestHandleDeleteTranslationSessionMarksEnded(t *testing.T) {
	store := newFakeTranslationSessionStore()
	store.sessions["session-123"] = db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: "user-123",
		Lang:           "es",
		Status:         db.TranslationSessionStatusConnected,
		ExpiresAt:      fixedTranslationNow.Add(time.Minute),
	}
	restoreTranslationTestDeps(t, store, &fakeTranslationOfferClient{})

	req := authenticatedTranslationRequest(http.MethodDelete, "/translation/sessions/session-123", "", false)
	rec := httptest.NewRecorder()

	HandleDeleteTranslationSession(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if store.sessions["session-123"].Status != db.TranslationSessionStatusEnded {
		t.Fatalf("expected ended session, got %#v", store.sessions["session-123"])
	}
}

func TestOpenAITranslationOfferClientSendsMultipartSDPRequest(t *testing.T) {
	var receivedSession openAITranslationCallSession
	var receivedSDP string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer openai-key" {
			t.Fatalf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("OpenAI-Safety-Identifier") != "safety-id" {
			t.Fatalf("unexpected safety identifier: %q", r.Header.Get("OpenAI-Safety-Identifier"))
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(maxTranslationRequestBytes); err != nil {
			t.Fatalf("failed to parse multipart body: %v", err)
		}
		receivedSDP = r.FormValue("sdp")
		if err := json.Unmarshal([]byte(r.FormValue("session")), &receivedSession); err != nil {
			t.Fatalf("failed to decode session config: %v", err)
		}

		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write([]byte("answer-sdp"))
	}))
	defer server.Close()

	restoreOpenAITranslationHTTPClient(t, server)

	answer, err := newOpenAITranslationOfferClient("openai-key").ExchangeTranslationSDP(
		context.Background(),
		translationOfferRequest{
			Language:         "vi",
			OfferSDP:         "offer-sdp",
			SafetyIdentifier: "safety-id",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if answer != "answer-sdp" {
		t.Fatalf("unexpected answer: %q", answer)
	}
	if receivedSDP != "offer-sdp" {
		t.Fatalf("unexpected SDP: %q", receivedSDP)
	}
	if receivedSession.Model != openAIRealtimeTranslationModel {
		t.Fatalf("unexpected model: %q", receivedSession.Model)
	}
	if receivedSession.Audio.Output.Language != "vi" {
		t.Fatalf("unexpected language: %q", receivedSession.Audio.Output.Language)
	}
}

var fixedTranslationNow = time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

type fakeTranslationSessionStore struct {
	sessions      map[string]db.TranslationSession
	createErr     error
	getErr        error
	markFailedErr error
	createdUserID string
	createdLang   string
}

func newFakeTranslationSessionStore() *fakeTranslationSessionStore {
	return &fakeTranslationSessionStore{
		sessions: make(map[string]db.TranslationSession),
	}
}

func (s *fakeTranslationSessionStore) Create(_ context.Context, userID, lang string, expiresAt time.Time) (db.TranslationSession, error) {
	if s.createErr != nil {
		return db.TranslationSession{}, s.createErr
	}
	s.createdUserID = userID
	s.createdLang = lang
	session := db.TranslationSession{
		ID:             "session-123",
		SupabaseUserID: userID,
		Lang:           lang,
		Status:         db.TranslationSessionStatusPending,
		CreatedAt:      fixedTranslationNow,
		ExpiresAt:      expiresAt,
	}
	s.sessions[session.ID] = session
	return session, nil
}

func (s *fakeTranslationSessionStore) GetForUser(_ context.Context, id, userID string) (db.TranslationSession, error) {
	if s.getErr != nil {
		return db.TranslationSession{}, s.getErr
	}
	session, ok := s.sessions[id]
	if !ok || session.SupabaseUserID != userID {
		return db.TranslationSession{}, pgx.ErrNoRows
	}
	return session, nil
}

func (s *fakeTranslationSessionStore) MarkConnected(_ context.Context, id string) error {
	session, ok := s.sessions[id]
	if !ok {
		return pgx.ErrNoRows
	}
	session.Status = db.TranslationSessionStatusConnected
	connectedAt := fixedTranslationNow
	session.ConnectedAt = &connectedAt
	s.sessions[id] = session
	return nil
}

func (s *fakeTranslationSessionStore) MarkFailed(_ context.Context, id string) error {
	if s.markFailedErr != nil {
		return s.markFailedErr
	}
	session, ok := s.sessions[id]
	if !ok {
		return pgx.ErrNoRows
	}
	session.Status = db.TranslationSessionStatusFailed
	endedAt := fixedTranslationNow
	session.EndedAt = &endedAt
	s.sessions[id] = session
	return nil
}

func (s *fakeTranslationSessionStore) MarkEndedForUser(_ context.Context, id, userID string) error {
	session, ok := s.sessions[id]
	if !ok || session.SupabaseUserID != userID {
		return pgx.ErrNoRows
	}
	session.Status = db.TranslationSessionStatusEnded
	endedAt := fixedTranslationNow
	session.EndedAt = &endedAt
	s.sessions[id] = session
	return nil
}

type fakeTranslationOfferClient struct {
	apiKey   string
	answer   string
	err      error
	requests []translationOfferRequest
}

func (c *fakeTranslationOfferClient) ExchangeTranslationSDP(_ context.Context, payload translationOfferRequest) (string, error) {
	c.requests = append(c.requests, payload)
	if c.err != nil {
		return "", c.err
	}
	return c.answer, nil
}

func authenticatedTranslationRequest(method, path, body string, anonymous bool) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	user := &SupabaseAuthUser{
		ID:          "user-123",
		Email:       "jane@example.com",
		DisplayName: "Jane",
		IsAnonymous: anonymous,
	}
	return req.WithContext(context.WithValue(req.Context(), supabaseAuthUserContextKey, user))
}

func restoreTranslationTestDeps(t *testing.T, store translationSessionStore, client *fakeTranslationOfferClient) {
	t.Helper()

	previousStore := translationSessions
	previousClientFactory := newTranslationOfferClient
	previousNow := translationSessionNow

	translationSessions = store
	newTranslationOfferClient = func(apiKey string) translationOfferClient {
		client.apiKey = apiKey
		return client
	}
	translationSessionNow = func() time.Time {
		return fixedTranslationNow
	}

	t.Cleanup(func() {
		translationSessions = previousStore
		newTranslationOfferClient = previousClientFactory
		translationSessionNow = previousNow
	})
}

func restoreOpenAITranslationHTTPClient(t *testing.T, server *httptest.Server) {
	t.Helper()

	previousURL := openAITranslationCallsURL
	previousClient := openAITranslationHTTPClient

	openAITranslationCallsURL = server.URL
	openAITranslationHTTPClient = server.Client()

	t.Cleanup(func() {
		openAITranslationCallsURL = previousURL
		openAITranslationHTTPClient = previousClient
	})
}
