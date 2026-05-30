package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"voice-video-server/db"

	"github.com/jackc/pgx/v5"
	router "github.com/jtclarkjr/router-go"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	openAIRealtimeTranslationModel = "gpt-realtime-translate"
	maxTranslationRequestBytes     = 1 << 20
	translationSessionTTL          = 10 * time.Minute
	translationSessionStoreMessage = "translation session store is unavailable"
)

var (
	errTranslationSessionStoreUnavailable                         = errors.New(translationSessionStoreMessage)
	openAITranslationCallsURL                                     = "https://api.openai.com/v1/realtime/translations/calls"
	openAITranslationHTTPClient                                   = &http.Client{Timeout: 10 * time.Second}
	translationSessionNow                                         = time.Now
	translationSessions                   translationSessionStore = dbTranslationSessionStore{}
	newTranslationOfferClient                                     = func(apiKey string) translationOfferClient {
		return newOpenAITranslationOfferClient(apiKey)
	}
	supportedTranslationLanguages = map[string]struct{}{
		"en": {},
		"es": {},
		"pt": {},
		"fr": {},
		"ja": {},
		"ru": {},
		"zh": {},
		"de": {},
		"ko": {},
		"hi": {},
		"id": {},
		"vi": {},
		"it": {},
	}
)

type createTranslationSessionResponse struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type translationSessionStore interface {
	Create(ctx context.Context, userID, lang string, expiresAt time.Time) (db.TranslationSession, error)
	GetForUser(ctx context.Context, id, userID string) (db.TranslationSession, error)
	MarkConnected(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string) error
	MarkEndedForUser(ctx context.Context, id, userID string) error
}

type dbTranslationSessionStore struct{}

func (dbTranslationSessionStore) Create(ctx context.Context, userID, lang string, expiresAt time.Time) (db.TranslationSession, error) {
	if db.Pool == nil {
		return db.TranslationSession{}, errTranslationSessionStoreUnavailable
	}
	return db.CreateTranslationSession(ctx, userID, lang, expiresAt)
}

func (dbTranslationSessionStore) GetForUser(ctx context.Context, id, userID string) (db.TranslationSession, error) {
	if db.Pool == nil {
		return db.TranslationSession{}, errTranslationSessionStoreUnavailable
	}
	return db.GetTranslationSessionForUser(ctx, id, userID)
}

func (dbTranslationSessionStore) MarkConnected(ctx context.Context, id string) error {
	if db.Pool == nil {
		return errTranslationSessionStoreUnavailable
	}
	return db.MarkTranslationSessionConnected(ctx, id)
}

func (dbTranslationSessionStore) MarkFailed(ctx context.Context, id string) error {
	if db.Pool == nil {
		return errTranslationSessionStoreUnavailable
	}
	return db.MarkTranslationSessionFailed(ctx, id)
}

func (dbTranslationSessionStore) MarkEndedForUser(ctx context.Context, id, userID string) error {
	if db.Pool == nil {
		return errTranslationSessionStoreUnavailable
	}
	return db.MarkTranslationSessionEndedForUser(ctx, id, userID)
}

type translationOfferRequest struct {
	Language         string
	OfferSDP         string
	SafetyIdentifier string
}

type translationOfferClient interface {
	ExchangeTranslationSDP(ctx context.Context, payload translationOfferRequest) (string, error)
}

type openAITranslationOfferClient struct {
	apiKey     string
	callsURL   string
	httpClient *http.Client
	sdkClient  openai.Client
}

type openAITranslationCallSession struct {
	Model string                        `json:"model"`
	Audio openAITranslationSessionAudio `json:"audio"`
}

type openAITranslationSessionAudio struct {
	Output openAITranslationSessionOutput `json:"output"`
}

type openAITranslationSessionOutput struct {
	Language string `json:"language"`
}

func newOpenAITranslationOfferClient(apiKey string) translationOfferClient {
	return &openAITranslationOfferClient{
		apiKey:     apiKey,
		callsURL:   openAITranslationCallsURL,
		httpClient: openAITranslationHTTPClient,
		sdkClient:  openai.NewClient(option.WithAPIKey(apiKey)),
	}
}

func (c *openAITranslationOfferClient) ExchangeTranslationSDP(ctx context.Context, payload translationOfferRequest) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("sdp", payload.OfferSDP); err != nil {
		return "", fmt.Errorf("write sdp part: %w", err)
	}

	sessionConfig, err := json.Marshal(openAITranslationCallSession{
		Model: openAIRealtimeTranslationModel,
		Audio: openAITranslationSessionAudio{
			Output: openAITranslationSessionOutput{
				Language: payload.Language,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal session config: %w", err)
	}

	if err := writer.WriteField("session", string(sessionConfig)); err != nil {
		return "", fmt.Errorf("write session part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.callsURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("OpenAI-Safety-Identifier", payload.SafetyIdentifier)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close OpenAI translation response body: %v", err)
		}
	}()

	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationRequestBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("OpenAI translation call failed with status %d", resp.StatusCode)
	}
	if strings.TrimSpace(string(answer)) == "" {
		return "", errors.New("OpenAI translation call returned an empty SDP answer")
	}

	return string(answer), nil
}

// HandleCreateTranslationSession creates a backend-owned translation session record.
func HandleCreateTranslationSession(w http.ResponseWriter, r *http.Request) {
	authUser, ok := requireTranslationAuth(w, r)
	if !ok {
		return
	}

	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		http.Error(w, "translation service is not configured", http.StatusServiceUnavailable)
		return
	}

	lang, ok := parseTranslationLanguage(w, r)
	if !ok {
		return
	}

	session, err := translationSessions.Create(
		r.Context(),
		authUser.ID,
		lang,
		translationSessionNow().Add(translationSessionTTL),
	)
	if err != nil {
		writeTranslationSessionStoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(createTranslationSessionResponse{
		ID:        session.ID,
		ExpiresAt: session.ExpiresAt,
	}); err != nil {
		log.Printf("failed to write translation session response: %v", err)
	}
}

// HandleTranslationSessionOffer exchanges a browser SDP offer for an OpenAI SDP answer.
func HandleTranslationSessionOffer(w http.ResponseWriter, r *http.Request) {
	authUser, ok := requireTranslationAuth(w, r)
	if !ok {
		return
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		http.Error(w, "translation service is not configured", http.StatusServiceUnavailable)
		return
	}

	sessionID := translationSessionIDFromRequest(r)
	if sessionID == "" {
		http.Error(w, "translation session not found", http.StatusNotFound)
		return
	}

	offerSDP, ok := readTranslationOfferSDP(w, r)
	if !ok {
		return
	}

	session, err := translationSessions.GetForUser(r.Context(), sessionID, authUser.ID)
	if err != nil {
		writeTranslationSessionLookupError(w, err)
		return
	}

	if translationSessionNow().After(session.ExpiresAt) {
		if err := translationSessions.MarkEndedForUser(r.Context(), session.ID, authUser.ID); err != nil {
			log.Printf("failed to mark expired translation session ended: %v", err)
		}
		http.Error(w, "translation session expired", http.StatusBadRequest)
		return
	}
	if session.Status != db.TranslationSessionStatusPending {
		http.Error(w, "translation session is not pending", http.StatusBadRequest)
		return
	}

	answerSDP, err := newTranslationOfferClient(apiKey).ExchangeTranslationSDP(
		r.Context(),
		translationOfferRequest{
			Language:         session.Lang,
			OfferSDP:         offerSDP,
			SafetyIdentifier: hashSafetyIdentifier(authUser.ID),
		},
	)
	if err != nil {
		log.Printf("OpenAI translation SDP exchange failed: %v", err)
		if markErr := translationSessions.MarkFailed(r.Context(), session.ID); markErr != nil {
			log.Printf("failed to mark translation session failed: %v", markErr)
		}
		http.Error(w, "could not create translation session", http.StatusBadGateway)
		return
	}

	if err := translationSessions.MarkConnected(r.Context(), session.ID); err != nil {
		writeTranslationSessionStoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(answerSDP)); err != nil {
		log.Printf("failed to write translation SDP answer: %v", err)
	}
}

// HandleDeleteTranslationSession marks a backend-owned translation session ended.
func HandleDeleteTranslationSession(w http.ResponseWriter, r *http.Request) {
	authUser, ok := requireTranslationAuth(w, r)
	if !ok {
		return
	}

	sessionID := translationSessionIDFromRequest(r)
	if sessionID == "" {
		http.Error(w, "translation session not found", http.StatusNotFound)
		return
	}

	if err := translationSessions.MarkEndedForUser(r.Context(), sessionID, authUser.ID); err != nil {
		writeTranslationSessionLookupError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func requireTranslationAuth(w http.ResponseWriter, r *http.Request) (*SupabaseAuthUser, bool) {
	authUser, ok := SupabaseAuthUserFromContext(r.Context())
	if !ok || authUser == nil || authUser.IsAnonymous {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return nil, false
	}
	return authUser, true
}

func parseTranslationLanguage(w http.ResponseWriter, r *http.Request) (string, bool) {
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	if _, supported := supportedTranslationLanguages[lang]; !supported {
		http.Error(w, "unsupported target language", http.StatusBadRequest)
		return "", false
	}
	return lang, true
}

func translationSessionIDFromRequest(r *http.Request) string {
	if id := strings.TrimSpace(router.URLParam(r, "id")); id != "" {
		return id
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 &&
		parts[0] == "translation" &&
		parts[1] == "sessions" &&
		parts[3] == "offer" {
		return strings.TrimSpace(parts[2])
	}
	if len(parts) == 3 && parts[0] == "translation" && parts[1] == "sessions" {
		return strings.TrimSpace(parts[2])
	}
	return ""
}

func readTranslationOfferSDP(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTranslationRequestBytes))
	if err != nil {
		http.Error(w, "invalid sdp offer", http.StatusBadRequest)
		return "", false
	}

	offerSDP := string(body)
	if strings.TrimSpace(offerSDP) == "" {
		http.Error(w, "invalid sdp offer", http.StatusBadRequest)
		return "", false
	}
	return offerSDP, true
}

func writeTranslationSessionStoreError(w http.ResponseWriter, err error) {
	if isTranslationSessionStoreUnavailable(err) {
		http.Error(w, translationSessionStoreMessage, http.StatusServiceUnavailable)
		return
	}
	log.Printf("translation session store failed: %v", err)
	http.Error(w, translationSessionStoreMessage, http.StatusServiceUnavailable)
}

func writeTranslationSessionLookupError(w http.ResponseWriter, err error) {
	if isTranslationSessionStoreUnavailable(err) {
		http.Error(w, translationSessionStoreMessage, http.StatusServiceUnavailable)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "translation session not found", http.StatusNotFound)
		return
	}
	log.Printf("translation session lookup failed: %v", err)
	http.Error(w, translationSessionStoreMessage, http.StatusServiceUnavailable)
}

func isTranslationSessionStoreUnavailable(err error) bool {
	return errors.Is(err, errTranslationSessionStoreUnavailable)
}

func hashSafetyIdentifier(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:])
}
