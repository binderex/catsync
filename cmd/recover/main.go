// Command recover rebuilds the YGO provider snapshot from PostgreSQL.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	catalogpg "github.com/binderex/catsync/internal/infrastructure/persistence/sql/postgresql/catalog"
	"github.com/binderex/catsync/internal/utility/recovery"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var output string
	flag.StringVar(&output, "output", "", "retrier JSON file (default data/retrier.json)")
	flag.Parse()

	_ = godotenv.Load(filepath.Join("..", ".env"))
	_ = godotenv.Load()

	dsn := strings.TrimSpace(os.Getenv("DB_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("CATSYNC_DB_URL"))
	}
	if dsn == "" {
		dsn = "postgres://binderex:binderex@localhost:5433/binderex?sslmode=disable"
	}

	if strings.TrimSpace(output) == "" {
		output = filepath.Join("data", "retrier.json")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := catalogpg.Open(dsn)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get PostgreSQL connection: %w", err)
	}
	defer sqlDB.Close()

	stats, err := recovery.Run(ctx, catalogpg.NewRepository(db), output)
	if err != nil {
		return err
	}

	log.Printf(
		"recovered %d items (%d sets, %d cards, %d orphan cards) into %s (%d bytes)",
		stats.Items,
		stats.Sets,
		stats.Cards,
		stats.Orphans,
		stats.Path,
		stats.Bytes,
	)

	return nil
}
