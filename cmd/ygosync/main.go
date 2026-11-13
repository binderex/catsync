package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/catprov"
	"github.com/binderex/catsync/internal/database"
	catalogsync "github.com/binderex/catsync/internal/sync"
	"github.com/joho/godotenv"
)

func main() {
	name := flag.String("name", "", "card name to synchronize; empty synchronizes the whole catalog")
	flag.Parse()

	if err := run(*name); err != nil {
		log.Fatal(err)
	}
}

func run(name string) error {
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

	syncer := catalogsync.New(
		catprov.NewYGOCatProv(),
		catalog.NewService(catalog.NewRepository(db)),
	)

	log.Printf("starting catalog synchronization (name=%q)", name)

	if err := syncer.SyncCatalog(context.Background(), name); err != nil {
		return fmt.Errorf("synchronize catalog: %w", err)
	}

	log.Printf("catalog synchronization completed (name=%q)", name)

	return nil
}
