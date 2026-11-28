// Command sync is the single entry point for the catalog synchronizations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	"github.com/binderex/catsync/internal/application/ports"
	"github.com/binderex/catsync/internal/application/usecases"
	"github.com/binderex/catsync/internal/domain"
	"github.com/binderex/catsync/internal/infrastructure/catprov/dragonshield"
	"github.com/binderex/catsync/internal/infrastructure/catprov/ygo"
	jsonstore "github.com/binderex/catsync/internal/infrastructure/persistence/json"
	catalogpg "github.com/binderex/catsync/internal/infrastructure/persistence/sql/postgresql/catalog"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		catprov     string
		mode        string
		resume      bool
		mediums     string
		rushDuel    bool
		retrierJSON string
	)

	flag.StringVar(&catprov, "catprov", "", "catalog provider: ygo or dragonshield")
	flag.StringVar(&mode, "mode", "new", "crawl mode: new or all")
	flag.BoolVar(&resume, "resume", true, "resume the latest unfinished run (use -resume=false to start fresh)")
	flag.StringVar(&mediums, "mediums", "TCG,OCG", "comma-separated YGO mediums")
	flag.BoolVar(&rushDuel, "rush-duel", false, "include Rush Duel (disabled until an official English release exists)")
	flag.StringVar(&retrierJSON, "retrier-json", "", "retrier JSON file (default data/retrier.json)")
	flag.Parse()

	provider, err := normalizeProvider(catprov)
	if err != nil {
		return err
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "new" && mode != "all" {
		return fmt.Errorf("invalid -mode %q: use new or all", mode)
	}

	_ = godotenv.Load()

	if strings.TrimSpace(retrierJSON) == "" {
		retrierJSON = os.Getenv("RETRIER_JSON_PATH")
	}
	if strings.TrimSpace(retrierJSON) == "" {
		// Keep the retrier state inside catsync so the project is self-contained.
		retrierJSON = filepath.Join("data", "retrier.json")
	}

	retrierRepo := jsonstore.NewFile(retrierJSON)
	log.Printf("%s: mode=%s resume=%v retrier=%s", provider, mode, resume, retrierRepo.Path())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dsn := resolveDSN()
	db, err := catalogpg.Open(dsn)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	log.Printf("connected to PostgreSQL")
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get PostgreSQL connection: %w", err)
	}
	defer sqlDB.Close()

	persistence := usecases.NewRetrierPersistenceUseCase(
		retrierRepo,
		usecases.WithSaveInterval(saveInterval()),
	)
	catalogUC := usecases.NewCatalogUseCase(catalogpg.NewRepository(db))

	var catalogProvider ports.CatalogProvider
	switch provider {
	case "ygo":
		catalogProvider = ygo.NewYGOCatalogProvider(persistence)
	case "dragonshield":
		catalogProvider = dragonshield.New(persistence)
	}

	providerUC := usecases.NewCatalogProviderUseCase(catalogProvider, catalogUC, persistence)

	opts := domain.ProviderOptions{Resume: resume}
	if provider == "ygo" {
		opts.Mediums = splitMediums(mediums)
		if rushDuel && !containsMedium(opts.Mediums, ygo.MediumRushDuel) {
			opts.Mediums = append(opts.Mediums, ygo.MediumRushDuel)
		}
	}

	var count int
	switch mode {
	case "new":
		count, err = providerUC.GetNewReleases(ctx, opts)
	case "all":
		count, err = providerUC.GetAllReleases(ctx, opts)
	}

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Printf("%s: interrupted; progress kept in retrier persistence", provider)
			return nil
		}
		return fmt.Errorf("%s %s: %w", provider, mode, err)
	}

	log.Printf("%s: mode=%s upserted %d items", provider, mode, count)

	return nil
}

func normalizeProvider(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ygo":
		return "ygo", nil
	case "dragonshield", "dragon", "ds":
		return "dragonshield", nil
	case "":
		return "", fmt.Errorf("-catprov is required: use ygo or dragonshield")
	default:
		return "", fmt.Errorf("invalid -catprov %q: use ygo or dragonshield", value)
	}
}
