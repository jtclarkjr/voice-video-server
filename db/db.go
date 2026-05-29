package db

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

// Connect creates a connection pool to the database.
func Connect(ctx context.Context, databaseURL string) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("ping database: %w", err)
	}

	Pool = pool
	log.Println("Connected to database")
	return nil
}

// Close closes the database connection pool.
func Close() {
	if Pool != nil {
		Pool.Close()
	}
}
