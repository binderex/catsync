package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/joho/godotenv"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/catprov/dragonshield"
	"github.com/binderex/catsync/internal/database"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Load the .env file when present; real environment variables take precedence.
	_ = godotenv.Load()

	dsn := os.Getenv("DB_URL")
	if dsn == "" {
		return fmt.Errorf("DB_URL environment variable is not set")
	}

	db, err := database.New(dsn)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get underlying sql database: %w", err)
	}
	defer sqlDB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	provider := dragonshield.New()
	items, err := provider.GetSleeves(ctx)
	if err != nil {
		return fmt.Errorf("scrape dragon shield sleeves: %w", err)
	}

	log.Printf("dragonshield: scraped %d sleeves", len(items))

	service := catalog.NewService(catalog.NewRepository(db))
	if err := service.UpsertItems(ctx, items); err != nil {
		return fmt.Errorf("upsert sleeves: %w", err)
	}

	log.Printf("dragonshield: upserted %d sleeves", len(items))

	return nil
}
