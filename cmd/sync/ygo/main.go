package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/catprov/ygo"
	"github.com/binderex/catsync/internal/database"
)

var (
	mode    string
	resume  bool
	mediums string
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	flag.StringVar(&mode, "mode", "new", "crawl mode: new (GetNewReleases) or all (GetAllReleases)")
	flag.BoolVar(&resume, "resume", false, "continue from ygo-releases-state.json (only with -mode all)")
	flag.StringVar(&mediums, "mediums", "", "comma-separated set mediums (TCG,OCG,Rush Duel)")
	flag.Parse()

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "new" && mode != "all" {
		return fmt.Errorf("invalid -mode %q: use new or all", mode)
	}
	if resume && mode != "all" {
		return fmt.Errorf("-resume is only valid with -mode all")
	}

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

	provider := ygo.NewYGOCatalogProvider()

	var (
		sets      []catalog.Set
		scrapeErr error
	)
	switch mode {
	case "all":
		sets, scrapeErr = provider.GetAllReleases(ctx, resume, splitMediums(mediums)...)
	case "new":
		sets, scrapeErr = provider.GetNewReleases(ctx, splitMediums(mediums)...)
	}
	log.Printf("ygo: mode=%s scraped %d sets", mode, len(sets))

	// Persist partial results even when the crawl was cancelled. The
	// checkpoint has already been written by GetAllReleases.
	persistCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	service := catalog.NewService(catalog.NewRepository(db))
	if err := persistSets(persistCtx, service, sets); err != nil {
		return fmt.Errorf("persist ygo catalog: %w", err)
	}

	if scrapeErr != nil {
		if errors.Is(scrapeErr, context.Canceled) || errors.Is(scrapeErr, context.DeadlineExceeded) {
			if mode == "all" {
				log.Printf("ygo: crawl interrupted; checkpoint saved for -mode all -resume")
			} else {
				log.Printf("ygo: crawl interrupted")
			}
			return nil
		}
		return scrapeErr
	}

	return nil
}

func splitMediums(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func persistSets(ctx context.Context, service *catalog.Service, sets []catalog.Set) error {
	infos := make(map[string]*catalog.ItemInfo)
	for setIndex := range sets {
		set := &sets[setIndex]
		addItemInfo(infos, set.ItemInfo)
		for printIndex := range set.CardsPrinted {
			addItemInfo(infos, set.CardsPrinted[printIndex].ItemInfo)
		}
	}

	brands := make(map[catalog.Brand]bool)
	for _, info := range infos {
		brands[info.Brand] = true
	}

	existingBySource := make(map[string]catalog.ItemInfo)
	for brand := range brands {
		existing, err := service.GetItemInfosByBrand(ctx, brand)
		if err != nil {
			return fmt.Errorf("get item infos for brand %q: %w", brand, err)
		}
		for _, info := range existing {
			existingBySource[info.Source] = info
		}
	}

	infosToUpsert := make([]catalog.ItemInfo, 0, len(infos))
	for source, info := range infos {
		if existing, ok := existingBySource[source]; ok {
			// The source conflict keeps the persisted ID, so reuse it before
			// updating the mutable columns.
			info.ID = existing.ID
		}
		infosToUpsert = append(infosToUpsert, *info)
	}

	if err := service.UpsertItemInfos(ctx, infosToUpsert); err != nil {
		return fmt.Errorf("upsert %d item infos: %w", len(infosToUpsert), err)
	}

	printCount := 0
	for setIndex := range sets {
		set := &sets[setIndex]
		set.ItemInfo = resolvedItemInfo(set.ItemInfo, infos)
		if set.ItemInfo != nil {
			set.ItemInfoID = &set.ItemInfo.ID
		}

		for printIndex := range set.CardsPrinted {
			print := &set.CardsPrinted[printIndex]
			print.ItemInfo = resolvedItemInfo(print.ItemInfo, infos)
			if print.ItemInfo != nil {
				print.ItemInfoID = &print.ItemInfo.ID
			}
		}
		printCount += len(set.CardsPrinted)

		if err := service.UpsertSetWithPrints(ctx, set, set.CardsPrinted); err != nil {
			return fmt.Errorf("upsert set %q: %w", set.Name, err)
		}
	}

	log.Printf(
		"ygo: upserted %d item infos, %d sets, %d prints",
		len(infos),
		len(sets),
		printCount,
	)
	return nil
}

func addItemInfo(infos map[string]*catalog.ItemInfo, info *catalog.ItemInfo) {
	if info == nil || info.Source == "" {
		return
	}
	infos[info.Source] = info
}

func resolvedItemInfo(info *catalog.ItemInfo, infos map[string]*catalog.ItemInfo) *catalog.ItemInfo {
	if info == nil {
		return nil
	}
	if resolved, ok := infos[info.Source]; ok {
		return resolved
	}
	return info
}
