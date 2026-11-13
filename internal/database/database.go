package database

import (
	"fmt"

	"github.com/binderex/catsync/internal/catalog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// New opens a GORM connection to PostgreSQL using dsn and runs the schema
// migrations for every model registered in the application.
func New(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := Migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

// Migrate creates or updates the tables and indexes required by the catalog models.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&catalog.CatalogItem{}); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	return nil
}
