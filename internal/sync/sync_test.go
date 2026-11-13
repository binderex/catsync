package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/binderex/catsync/internal/catalog"
	"github.com/binderex/catsync/internal/catprov"
	"github.com/binderex/catsync/internal/database"
	"github.com/joho/godotenv"
)

func TestSyncCatalogOjamaUpsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network/database integration test in short mode")
	}

	_ = godotenv.Load(filepath.Join("..", "..", ".env"))

	dsn := os.Getenv("DB_URL")
	if dsn == "" {
		t.Skip("DB_URL is not set")
	}

	db, err := database.New(dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}

	// Keep test data isolated: every write is rolled back at the end.
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	ctx := context.Background()
	catalogService := catalog.NewService(catalog.NewRepository(tx))
	syncer := New(catprov.NewYGOCatProv(), catalogService)

	if err := syncer.SyncCatalog(ctx, "ojama"); err != nil {
		t.Fatalf("SyncCatalog(ojama): %v", err)
	}

	items, err := catalogService.GetByName(ctx, "Ojama Yellow")
	if err != nil {
		t.Fatalf("GetByName(Ojama Yellow): %v", err)
	}
	if len(items) == 0 {
		t.Fatal("Ojama Yellow was not inserted by the sync")
	}

	original := items[0]
	for _, item := range items {
		if item.Code != "" {
			original = item
			break
		}
	}

	modified := original
	modified.ID = 0
	modified.Description = original.Description + " [upsert check]"
	modified.SetType = "Upsert Check"
	modified.GuaranteedCopies = original.GuaranteedCopies + 7

	if err := catalogService.Upsert(ctx, []catalog.CatalogItem{modified}); err != nil {
		t.Fatalf("upsert modified item: %v", err)
	}

	items, err = catalogService.GetByName(ctx, "Ojama Yellow")
	if err != nil {
		t.Fatalf("GetByName after modify: %v", err)
	}
	updated := findByIdentity(items, original)
	if updated == nil {
		t.Fatal("modified item was not found after upsert")
	}
	if updated.Description != modified.Description {
		t.Errorf("Description = %q, want %q", updated.Description, modified.Description)
	}
	if updated.SetType != modified.SetType {
		t.Errorf("SetType = %q, want %q", updated.SetType, modified.SetType)
	}
	if updated.GuaranteedCopies != modified.GuaranteedCopies {
		t.Errorf("GuaranteedCopies = %d, want %d", updated.GuaranteedCopies, modified.GuaranteedCopies)
	}

	// Restore the original mutable values to prove the upsert updates them again.
	restored := original
	restored.ID = 0

	if err := catalogService.Upsert(ctx, []catalog.CatalogItem{restored}); err != nil {
		t.Fatalf("upsert restored item: %v", err)
	}

	items, err = catalogService.GetByName(ctx, "Ojama Yellow")
	if err != nil {
		t.Fatalf("GetByName after restore: %v", err)
	}
	current := findByIdentity(items, original)
	if current == nil {
		t.Fatal("restored item was not found after upsert")
	}
	if current.Description != original.Description {
		t.Errorf("Description after restore = %q, want %q", current.Description, original.Description)
	}
	if current.SetType != original.SetType {
		t.Errorf("SetType after restore = %q, want %q", current.SetType, original.SetType)
	}
	if current.GuaranteedCopies != original.GuaranteedCopies {
		t.Errorf("GuaranteedCopies after restore = %d, want %d", current.GuaranteedCopies, original.GuaranteedCopies)
	}
}

func findByIdentity(items []catalog.CatalogItem, want catalog.CatalogItem) *catalog.CatalogItem {
	for i := range items {
		item := items[i]
		if item.ExternalID != want.ExternalID ||
			item.Brand != want.Brand ||
			item.Type != want.Type ||
			item.Lang != want.Lang ||
			item.Code != want.Code ||
			item.Rarity != want.Rarity ||
			item.Edition != want.Edition ||
			item.ExtendedArtwork != want.ExtendedArtwork ||
			item.SetName != want.SetName {
			continue
		}
		return &items[i]
	}
	return nil
}
