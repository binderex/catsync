// Package database opens the PostgreSQL connection and runs catalog migrations.
package database

import (
	"fmt"
	"time"

	"github.com/binderex/catsync/internal/catalog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// New opens a GORM connection to PostgreSQL using dsn and runs the schema
// migrations for every model registered in the application.
func New(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// CardPrinted and Set share the items table. The self-referencing
		// set_id relation is nullable, but it is kept out of AutoMigrate to
		// avoid STI-specific constraint issues.
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying sql database: %w", err)
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(1 * time.Minute)

	if err := Migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

// Migrate creates or updates the tables and indexes required by the catalog models.
func Migrate(db *gorm.DB) error {
	// GORM deduplicates models by table name inside a single AutoMigrate call.
	// Item, CardPrinted and Set share the items table, so they must be
	// migrated separately; otherwise only one of them contributes columns and
	// fields such as rarity never reach the table.
	models := []any{
		&catalog.Item{},
		&catalog.CardPrinted{},
		&catalog.Set{},
		&catalog.ItemInfo{},
	}
	for _, model := range models {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}

	if err := ensureItemIdentityIndex(db); err != nil {
		return err
	}

	if err := ensureItemInfoIdentityIndexes(db); err != nil {
		return err
	}

	return nil
}

// ensureItemIdentityIndex creates the composite identity of an item row. It
// includes kind because CardPrinted and Set share the items table, and subtypes
// because product variants (for example sleeve sizes) may otherwise collide.
func ensureItemIdentityIndex(db *gorm.DB) error {
	if err := db.Exec(`DROP INDEX IF EXISTS idx_items_identity`).Error; err != nil {
		return fmt.Errorf("drop item identity index: %w", err)
	}

	err := db.Exec(`
		CREATE UNIQUE INDEX idx_items_identity
		ON items (
			kind,
			brand,
			lang,
			code,
			item_info_id,
			rarity,
			edition,
			extended_art,
			set_id,
			subtypes
		)
		NULLS NOT DISTINCT
	`).Error
	if err != nil {
		return fmt.Errorf("create item identity index: %w", err)
	}

	return nil
}

// ensureItemInfoIdentityIndexes creates the composite identity of an item info
// row and the unique index for its source URL.
func ensureItemInfoIdentityIndexes(db *gorm.DB) error {
	if err := db.Exec(`DROP INDEX IF EXISTS idx_item_infos_identity`).Error; err != nil {
		return fmt.Errorf("drop item info identity index: %w", err)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX idx_item_infos_identity
		ON item_infos (kind, brand, source)
		NULLS NOT DISTINCT
	`).Error; err != nil {
		return fmt.Errorf("create item info identity index: %w", err)
	}

	if err := db.Exec(`DROP INDEX IF EXISTS idx_item_infos_source`).Error; err != nil {
		return fmt.Errorf("drop item info source index: %w", err)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX idx_item_infos_source
		ON item_infos (source)
		NULLS NOT DISTINCT
	`).Error; err != nil {
		return fmt.Errorf("create item info source index: %w", err)
	}

	return nil
}
