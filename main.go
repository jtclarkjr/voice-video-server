package main

import (
	"log"
	"net/http"
	"os"
	"strings"

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
		db.Connect(dbURL)
		defer db.Close()
	} else {
		log.Println("DATABASE_URL not set, running without database")
	}

	r := router.NewRouter()
	r.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{IncludeTimestamp: false, Output: os.Stdout}))

	allowedCORS := os.Getenv("ALLOWED_CORS")
	if allowedCORS != "" {
		origins := strings.Split(allowedCORS, ",")
		r.Use(middleware.StrictCORS(origins))
	} else {
		r.Use(middleware.SimpleCORS())
	}

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Post("/offer", handler.HandleOffer)
	r.Post("/media", handler.HandleMedia)
	r.Get("/rooms", handler.HandleListRooms)
	r.Get("/rooms/events", handler.HandleRoomEvents)
	r.Get("/ws", handler.HandleSignal)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
