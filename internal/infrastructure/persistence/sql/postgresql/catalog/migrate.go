package catalog

import (
	"fmt"

	"gorm.io/gorm"
)

// Migrate creates or updates the denormalized items table and its indexes.
// Existing item_infos data should be backfilled before dropping the old
// columns; this function only prepares the new shape.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&itemModel{}); err != nil {
		return fmt.Errorf("migrate items: %w", err)
	}

	// Store-owned items share the items table. Global catalog rows (the only
	// ones Catsync writes) keep store_id NULL.
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS stores (
			id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name       text NOT NULL DEFAULT '',
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		return fmt.Errorf("create stores table: %w", err)
	}

	if err := db.Exec(`ALTER TABLE items ADD COLUMN IF NOT EXISTS store_id uuid`).Error; err != nil {
		return fmt.Errorf("add store_id column: %w", err)
	}

	if err := db.Exec(`ALTER TABLE items DROP CONSTRAINT IF EXISTS items_store_id_fkey`).Error; err != nil {
		return fmt.Errorf("drop store foreign key: %w", err)
	}
	if err := db.Exec(`
		ALTER TABLE items
		ADD CONSTRAINT items_store_id_fkey
		FOREIGN KEY (store_id) REFERENCES stores(id)
		ON DELETE CASCADE
	`).Error; err != nil {
		return fmt.Errorf("add store foreign key: %w", err)
	}

	if err := db.Exec(`
		ALTER TABLE items
		ADD COLUMN IF NOT EXISTS variant_key text
		GENERATED ALWAYS AS (
			md5(coalesce((properties->'variant')::text, 'null'))
		) STORED
	`).Error; err != nil {
		return fmt.Errorf("add variant_key generated column: %w", err)
	}

	if err := db.Exec(`DROP INDEX IF EXISTS idx_items_identity`).Error; err != nil {
		return fmt.Errorf("drop item identity index: %w", err)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX idx_items_identity
		ON items (
			store_id,
			kind,
			brand,
			lang,
			code,
			set_id,
			rarity,
			edition,
			extended_art,
			source,
			variant_key
		)
		NULLS NOT DISTINCT
	`).Error; err != nil {
		return fmt.Errorf("create item identity index: %w", err)
	}

	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_items_store_id ON items (store_id)`,
		`CREATE INDEX IF NOT EXISTS idx_items_release_at ON items (release_at)`,
		`CREATE INDEX IF NOT EXISTS idx_items_source ON items (source)`,
		`CREATE INDEX IF NOT EXISTS idx_items_properties ON items USING gin (properties jsonb_path_ops)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("create item index: %w", err)
		}
	}

	// Global catalog rows must have a source. Store-owned rows may not.
	if err := db.Exec(`ALTER TABLE items DROP CONSTRAINT IF EXISTS items_source_not_empty`).Error; err != nil {
		return fmt.Errorf("drop source constraint: %w", err)
	}
	if err := db.Exec(`
		ALTER TABLE items
		ADD CONSTRAINT items_source_not_empty CHECK (store_id IS NOT NULL OR source <> '')
	`).Error; err != nil {
		return fmt.Errorf("add source constraint: %w", err)
	}

	// "unknown" is a placeholder, not a real catalog entry. Catsync only
	// cleans the global catalog; store-owned rows are untouched.
	if err := db.Exec(`DELETE FROM items WHERE store_id IS NULL AND lower(btrim(name)) = 'unknown'`).Error; err != nil {
		return fmt.Errorf("delete unknown items: %w", err)
	}

	// Only regular monsters, spells and traps belong to the catalog. Tokens,
	// strategy/tip/checklist cards, skills and other non-standard types are
	// removed.
	if err := db.Exec(`
		DELETE FROM items
		WHERE store_id IS NULL
		  AND kind = 'CARD'
		  AND (
			properties->>'card_types' IS NULL
			OR btrim(properties->>'card_types') = ''
			OR properties->>'card_types' ILIKE '%Token%'
			OR properties->>'card_types' ILIKE '%Strategy%'
			OR properties->>'card_types' ILIKE '%Skill%'
			OR properties->>'card_types' ILIKE '%Tip%'
			OR properties->>'card_types' ILIKE '%Checklist%'
			OR properties->>'card_types' ILIKE '%Non-game%'
			OR (
				properties->>'card_types' NOT ILIKE '%Monster%'
				AND properties->>'card_types' NOT ILIKE '%Spell%'
				AND properties->>'card_types' NOT ILIKE '%Trap%'
			)
		  )
	`).Error; err != nil {
		return fmt.Errorf("delete non-standard cards: %w", err)
	}

	// Cards whose set was filtered out would be unreachable. Only global
	// cards and sets are considered.
	if err := db.Exec(`
		DELETE FROM items c
		WHERE c.store_id IS NULL
		  AND c.kind = 'CARD'
		  AND c.set_id IS NOT NULL
		  AND NOT EXISTS (
			SELECT 1 FROM items s
			WHERE s.id = c.set_id AND s.kind = 'SET' AND s.store_id IS NULL
		  )
	`).Error; err != nil {
		return fmt.Errorf("delete orphan cards: %w", err)
	}

	return nil
}
