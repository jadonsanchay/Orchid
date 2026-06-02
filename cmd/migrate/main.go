package main

import (
	"context"
	"log"
	"os"

	"orchid/pkg/utils"

	"github.com/jackc/pgx/v5"
)

func main() {
	utils.LoadEnv()
	dbURL := os.Getenv("SUPABASE_DB_URL")
	if dbURL == "" {
		log.Fatal("SUPABASE_DB_URL is not set in environment or .env file")
	}

	escapedURL := utils.EscapeConnectionURI(dbURL)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, escapedURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	schemaSQL, err := os.ReadFile("db/schema.sql")
	if err != nil {
		log.Fatalf("Unable to read db/schema.sql: %v", err)
	}

	log.Println("Applying schema migrations...")
	_, err = conn.Exec(ctx, string(schemaSQL))
	if err != nil {
		log.Fatalf("Failed to apply migrations: %v", err)
	}

	log.Println("Schema migrations applied successfully!")
}
