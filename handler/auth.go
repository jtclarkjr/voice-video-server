package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const anonymousDisplayName = "Anon User"

var supabaseAuthHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

type SupabaseAuthUser struct {
	ID          string
	Email       string
	DisplayName string
	IsAnonymous bool
}

type supabaseUserResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	IsAnonymous bool   `json:"is_anonymous"`
	UserMeta    struct {
		Name string `json:"name"`
	} `json:"user_metadata"`
}

func validateSupabaseAccessToken(
	ctx context.Context,
	accessToken string,
) (*SupabaseAuthUser, error) {
	if accessToken == "" {
		return nil, nil
	}

	if !SupabaseAuthConfigured() {
		return nil, errors.New("supabase auth is not configured")
	}

	supabaseURL := strings.TrimRight(os.Getenv("SUPABASE_URL"), "/")
	supabaseSecretKey := os.Getenv("SUPABASE_SECRET_KEY")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		supabaseURL+"/auth/v1/user",
		nil,
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("apikey", supabaseSecretKey)

	resp, err := supabaseAuthHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("failed to close auth response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token validation failed with status %d", resp.StatusCode)
	}

	var payload supabaseUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(payload.UserMeta.Name)
	if payload.IsAnonymous {
		displayName = anonymousDisplayName
	} else if displayName == "" {
		displayName = strings.TrimSpace(payload.Email)
	}

	if displayName == "" {
		displayName = "User"
	}

	return &SupabaseAuthUser{
		ID:          payload.ID,
		Email:       payload.Email,
		DisplayName: displayName,
		IsAnonymous: payload.IsAnonymous,
	}, nil
}
