package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"voice-video-server/db"
	"voice-video-server/handler"

	"github.com/joho/godotenv"
	"github.com/jtclarkjr/router-go"
	"github.com/jtclarkjr/router-go/middleware"
)

func main() {
	_ = godotenv.Load()
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		log.Println("DATABASE_URL set, connecting to database")
		if err := db.Connect(dbCtx, dbURL); err != nil {
			log.Printf("Database unavailable, continuing without database: %v", err)
		} else {
			defer db.Close()
		}
		cancel()
	} else {
		log.Println("DATABASE_URL not set, running without database")
	}

	r := router.NewRouter()
	r.Use(middleware.LoggerWithConfig(
		middleware.LoggerConfig{
			IncludeTimestamp: false, Output: os.Stdout,
		},
	))

	allowedCORS := os.Getenv("ALLOWED_CORS")
	if allowedCORS != "" {
		origins := parseAllowedOrigins(allowedCORS)
		if len(origins) > 0 {
			r.Use(middleware.StrictCORS(origins))
		} else {
			log.Println("ALLOWED_CORS was set but no valid origins were found, falling back to permissive CORS")
			r.Use(middleware.SimpleCORS())
		}
	} else {
		r.Use(middleware.SimpleCORS())
	}

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Options("/health", handleOptions)

	r.Get("/rooms", handler.HandleListRooms)
	r.Get("/rooms/events", handler.HandleRoomEvents)
	r.Get("/ws", handler.HandleSignal)
	r.Options("/rooms", handleOptions)
	r.Options("/rooms/events", handleOptions)
	r.Options("/ws", handleOptions)

	if handler.SupabaseAuthConfigured() {
		r.Use(handler.RequireSupabaseAuth())
	} else {
		log.Println("SUPABASE auth not configured, protected route middleware disabled")
	}

	r.Post("/offer", handler.HandleOffer)
	r.Post("/media", handler.HandleMedia)
	r.Options("/offer", handleOptions)
	r.Options("/media", handleOptions)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func parseAllowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))

	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		origins = append(origins, origin)
	}

	return origins
}

func handleOptions(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
