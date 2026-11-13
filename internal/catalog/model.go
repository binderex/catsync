package catalog

import "time"

// CatalogItem represents a single entry in the catalog, which can be either a
// card or a set. The combination of ExternalID, Brand, Type, Lang, Code,
// Rarity, Edition, ExtendedArtwork and SetName forms the composite unique
// index idx_catalog_identity that prevents duplicated entries.
type CatalogItem struct {
	ID         uint        `json:"id" gorm:"primaryKey;autoIncrement"`
	ExternalID string      `json:"external_id" gorm:"type:text;not null;uniqueIndex:idx_catalog_identity"`
	Brand      Brand       `json:"brand" gorm:"type:text;not null;uniqueIndex:idx_catalog_identity"`
	Type       CatalogType `json:"type" gorm:"type:text;not null;uniqueIndex:idx_catalog_identity"`
	Lang       Lang        `json:"lang" gorm:"type:char(2);not null;uniqueIndex:idx_catalog_identity"`

	Name        string         `json:"name" gorm:"type:text;not null"`
	Description string         `json:"description" gorm:"type:text"`
	Archetype   string         `json:"archetype" gorm:"type:text"`
	CardInfo    map[string]any `json:"card_info" gorm:"type:jsonb;serializer:json"`
	CardImages  []CardImage    `json:"card_images" gorm:"type:jsonb;serializer:json"`

	Code             string `json:"code" gorm:"type:varchar(50);uniqueIndex:idx_catalog_identity"`
	SerialCode       string `json:"serial_code" gorm:"type:varchar(50)"`
	Rarity           string `json:"rarity" gorm:"type:varchar(50);uniqueIndex:idx_catalog_identity"`
	RarityCode       string `json:"rarity_code" gorm:"type:varchar(20)"`
	Edition          string `json:"edition" gorm:"type:text;not null;uniqueIndex:idx_catalog_identity"`
	ExtendedArtwork  bool   `json:"extended_artwork" gorm:"not null;default:false;uniqueIndex:idx_catalog_identity"`
	GuaranteedCopies uint   `json:"guaranteed_copies" gorm:"not null;default:0"`
	PrintImage       Image  `json:"print_image" gorm:"type:jsonb;serializer:json"`

	SetName         string `json:"set_name" gorm:"type:text;not null;uniqueIndex:idx_catalog_identity"`
	SetType         string `json:"set_type" gorm:"type:varchar(50)"`
	SetCode         string `json:"set_code" gorm:"type:varchar(50)"`
	SetRegionalCode string `json:"set_regional_code" gorm:"type:varchar(50)"`
	QuantityPerSet  uint   `json:"quantity_per_set" gorm:"not null;default:0"`
	QuantityPerBox  uint   `json:"quantity_per_box" gorm:"not null;default:0"`
	SetImage        Image  `json:"set_image" gorm:"type:jsonb;serializer:json"`
	SetBoxImage     Image  `json:"set_box_image" gorm:"type:jsonb;serializer:json"`

	ReleasedAt time.Time `json:"released_at"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// Image holds the small and large variants of an image URL and is stored as
// JSONB inside CatalogItem.
type Image struct {
	URLSmall string `json:"image_url_small"`
	URLLarge string `json:"image_url_large"`
}

// CardImage holds the available image variants of a card and is stored as a
// JSONB array inside CatalogItem.
type CardImage struct {
	URL        string `json:"image_url"`
	URLSmall   string `json:"image_url_small"`
	URLCropped string `json:"image_url_cropped"`
}

// Brand defines the brand of the catalog item
type Brand string

const (
	// Yu-Gi-Oh!
	BrandYGO Brand = "YGO"

	// Pokémon
	BrandPKM Brand = "PKM"
)

// CatalogType defines the type of catalog item
type CatalogType string

const (
	// Card
	CatalogTypeCard CatalogType = "CARD"

	// Set
	CatalogTypeSet CatalogType = "SET"
)

// Lang defines the language of the catalog item
type Lang string

const (
	// English
	LangEN Lang = "EN"
	// French
	LangFR Lang = "FR"
	// German
	LangDE Lang = "DE"
	// Italian
	LangIT Lang = "IT"
	// Portuguese
	LangPT Lang = "PT"
	// Spanish
	LangES Lang = "ES"

	// Japanese
	LangJP Lang = "JP"
	// Asian-English
	LangAE Lang = "AE"
	// Korean
	LangKR Lang = "KR"
	// Traditional Chinese
	LangTC Lang = "TC"
	// Simplified Chinese
	LangSC Lang = "SC"
)
