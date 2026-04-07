package db

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

// Connect creates a connection pool to the database.
func Connect(databaseURL string) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Unable to ping database: %v", err)
	}

	Pool = pool
	log.Println("Connected to database")
}

// Close closes the database connection pool.
func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
