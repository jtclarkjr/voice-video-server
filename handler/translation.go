package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	openAIRealtimeTranslationModel = "gpt-realtime-translate"
	maxTranslationRequestBytes     = 1 << 20
)

var (
	openAITranslationClientSecretsURL = "https://api.openai.com/v1/realtime/translations/client_secrets"
	openAITranslationHTTPClient       = &http.Client{Timeout: 10 * time.Second}
	supportedTranslationLanguages     = map[string]struct{}{
		"en": {},
		"es": {},
		"fr": {},
		"de": {},
		"it": {},
		"pt": {},
		"ja": {},
		"ko": {},
		"zh": {},
		"ar": {},
		"hi": {},
	}
)

type translationClientSecretRequest struct {
	TargetLanguage string `json:"targetLanguage"`
}

type openAITranslationClientSecretRequest struct {
	Session openAITranslationSession `json:"session"`
}

type openAITranslationSession struct {
	Model string                    `json:"model"`
	Audio openAITranslationAudioCfg `json:"audio"`
}

type openAITranslationAudioCfg struct {
	Output openAITranslationOutputCfg `json:"output"`
}

type openAITranslationOutputCfg struct {
	Language string `json:"language"`
}

// HandleTranslationClientSecret creates a short-lived OpenAI Realtime translation
// client secret for authenticated, non-anonymous users.
func HandleTranslationClientSecret(w http.ResponseWriter, r *http.Request) {
	authUser, ok := SupabaseAuthUserFromContext(r.Context())
	if !ok || authUser == nil || authUser.IsAnonymous {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		http.Error(w, "translation service is not configured", http.StatusServiceUnavailable)
		return
	}

	var payload translationClientSecretRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTranslationRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	targetLanguage := strings.ToLower(strings.TrimSpace(payload.TargetLanguage))
	if _, supported := supportedTranslationLanguages[targetLanguage]; !supported {
		http.Error(w, "unsupported target language", http.StatusBadRequest)
		return
	}

	body, err := json.Marshal(openAITranslationClientSecretRequest{
		Session: openAITranslationSession{
			Model: openAIRealtimeTranslationModel,
			Audio: openAITranslationAudioCfg{
				Output: openAITranslationOutputCfg{
					Language: targetLanguage,
				},
			},
		},
	})
	if err != nil {
		http.Error(w, "could not create translation session", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPost,
		openAITranslationClientSecretsURL,
		bytes.NewReader(body),
	)
	if err != nil {
		http.Error(w, "could not create translation session", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Safety-Identifier", hashSafetyIdentifier(authUser.ID))

	resp, err := openAITranslationHTTPClient.Do(req)
	if err != nil {
		log.Printf("OpenAI translation client secret request failed: %v", err)
		http.Error(w, "could not create translation session", http.StatusBadGateway)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close OpenAI translation response body: %v", err)
		}
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.Printf("OpenAI translation client secret request failed with status %d", resp.StatusCode)
		http.Error(w, "could not create translation session", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("failed to write OpenAI translation response: %v", err)
	}
}

func hashSafetyIdentifier(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:])
}
