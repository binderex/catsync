// Package catalog defines the catalog model, identity rules and persistence.
package catalog

import (
	"time"

	"github.com/google/uuid"
)

// Item is the shared persisted part of a catalog entry. CardPrinted and Set
// embed Item and share the items table. ItemInfoID is optional because external
// products such as playmats and sleeves may not have shared card/set
// information.
type Item struct {
	ID          uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ItemInfoID  *uuid.UUID     `json:"item_info_id,omitempty" gorm:"type:uuid;index:idx_items_item_info_id"`
	Kind        Kind           `json:"kind" gorm:"type:text;not null;index:idx_items_kind"`
	Subtypes    map[string]any `json:"subtypes,omitempty" gorm:"type:jsonb;serializer:json"`
	Brand       Brand          `json:"brand" gorm:"type:text;not null;index:idx_items_brand"`
	Lang        Lang           `json:"lang" gorm:"type:char(2);not null;index:idx_items_lang"`
	Code        string         `json:"code" gorm:"type:varchar(50);not null;default:'';index:idx_items_code"`
	Name        string         `json:"name" gorm:"type:text;not null;index:idx_items_name"`
	Description string         `json:"description" gorm:"type:text"`
	Image       Image          `json:"image" gorm:"type:jsonb;serializer:json"`
	ReleaseAt   time.Time      `json:"release_at"`
	CreatedAt   time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
}

type CardPrinted struct {
	Item             `gorm:"embedded"`
	SerialCode       string     `json:"serial_code" gorm:"type:varchar(50);not null;default:''"`
	Rarity           string     `json:"rarity" gorm:"type:varchar(50);not null;default:''"`
	RarityCode       string     `json:"rarity_code" gorm:"type:varchar(20);not null;default:''"`
	Edition          string     `json:"edition" gorm:"type:text;not null;default:''"`
	ExtendedArt      bool       `json:"extended_art" gorm:"not null;default:false"`
	GuaranteedCopies uint       `json:"guaranteed_copies" gorm:"not null;default:0"`
	SetID            *uuid.UUID `json:"set_id,omitempty" gorm:"type:uuid;index:idx_items_set_id"`
}

type Set struct {
	Item                 `gorm:"embedded"`
	RegionalCode         string `json:"regional_code" gorm:"type:varchar(50);not null;default:'';index:idx_items_regional_code"`
	QuantityCardsPerPack uint   `json:"quantity_cards_per_pack" gorm:"not null;default:0"`
	QuantityPacksPerSet  uint   `json:"quantity_packs_per_set" gorm:"not null;default:0"`
	// CardsPrinted is the inverse of CardPrinted.SetID. When preloading it,
	// filter by kind = KindCard because items also contains sets, playmats and
	// sleeves that may reference the same set.
	CardsPrinted []CardPrinted `json:"cards_printed,omitempty" gorm:"foreignKey:SetID;references:ID;-:migration"`
}

func NewItem(
	infoID *uuid.UUID,
	kind Kind,
	lang Lang,
	code, name, description string,
	image Image,
	releaseAt time.Time,
) *Item {
	return &Item{
		ItemInfoID:  infoID,
		Kind:        kind,
		Lang:        lang,
		Code:        code,
		Name:        name,
		Description: description,
		Image:       image,
		ReleaseAt:   releaseAt,
	}
}

func NewCardPrinted(
	item Item,
	setID *uuid.UUID,
	serialCode, rarity, rarityCode, edition string,
	extendedArt bool,
	guaranteedCopies uint,
) *CardPrinted {
	item.Kind = KindCard
	return &CardPrinted{
		Item:             item,
		SetID:            setID,
		SerialCode:       serialCode,
		Rarity:           rarity,
		RarityCode:       rarityCode,
		Edition:          edition,
		ExtendedArt:      extendedArt,
		GuaranteedCopies: guaranteedCopies,
	}
}

func NewSet(
	item Item,
	regionalCode string,
	cardsPerPack, packsPerSet uint,
) *Set {
	item.Kind = KindSet
	return &Set{
		Item:                 item,
		RegionalCode:         regionalCode,
		QuantityCardsPerPack: cardsPerPack,
		QuantityPacksPerSet:  packsPerSet,
	}
}

// NewPlaymat creates a playmat item. Playmats carry their own brand and
// subtypes; they do not belong to a set.
func NewPlaymat(
	infoID *uuid.UUID,
	brand Brand,
	subtypes map[string]any,
	lang Lang,
	code, name, description string,
	image Image,
	releaseAt time.Time,
) *Item {
	return newPlainItem(infoID, KindPlaymat, brand, subtypes, lang, code, name, description, image, releaseAt)
}

// NewSleeve creates a sleeve item. Sleeves carry their own brand and subtypes;
// they do not belong to a set.
func NewSleeve(
	infoID *uuid.UUID,
	brand Brand,
	subtypes map[string]any,
	lang Lang,
	code, name, description string,
	image Image,
	releaseAt time.Time,
) *Item {
	return newPlainItem(infoID, KindSleeve, brand, subtypes, lang, code, name, description, image, releaseAt)
}

func newPlainItem(
	infoID *uuid.UUID,
	kind Kind,
	brand Brand,
	subtypes map[string]any,
	lang Lang,
	code, name, description string,
	image Image,
	releaseAt time.Time,
) *Item {
	item := NewItem(infoID, kind, lang, code, name, description, image, releaseAt)
	item.Brand = brand
	item.Subtypes = subtypes
	return item
}

// ItemInfo holds shared information for cards, sets and, optionally, other
// products such as playmats and sleeves.
type ItemInfo struct {
	ID             uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Kind           Kind           `json:"kind" gorm:"type:text;not null;uniqueIndex:idx_item_infos_identity,priority:1;index:idx_item_infos_kind"`
	Brand          Brand          `json:"brand" gorm:"type:text;not null;uniqueIndex:idx_item_infos_identity,priority:2;index:idx_item_infos_brand"`
	Images         []Image        `json:"images" gorm:"type:jsonb;serializer:json"`
	Source         string         `json:"source" gorm:"type:text;not null;uniqueIndex:idx_item_infos_identity,priority:3;uniqueIndex:idx_item_infos_source"`
	VerifiedSource bool           `json:"verified_source" gorm:"not null;default:false"`
	SetType        string         `json:"set_type" gorm:"type:varchar(50);not null;default:''"`
	SetCode        string         `json:"set_code" gorm:"type:varchar(50);not null;default:''"`
	Archetype      string         `json:"archetype" gorm:"type:text;not null;default:''"`
	CardTypes      string         `json:"card_types" gorm:"type:text;not null;default:''"`
	Properties     map[string]any `json:"properties" gorm:"type:jsonb;serializer:json"`
	CreatedAt      time.Time      `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      time.Time      `json:"updated_at" gorm:"autoUpdateTime"`
}

func (i *ItemInfo) SetVerifiedSource(verified bool) {
	i.VerifiedSource = verified
}

func NewCardInfo(brand Brand, images []Image, source, archetype, cardTypes string, properties map[string]any) *ItemInfo {
	return &ItemInfo{
		ID:         uuid.New(),
		Kind:       KindCard,
		Brand:      brand,
		Images:     images,
		Source:     source,
		Archetype:  archetype,
		CardTypes:  cardTypes,
		Properties: properties,
	}
}

func NewSetInfo(brand Brand, images []Image, source, setType, setCode string) *ItemInfo {
	return &ItemInfo{
		ID:      uuid.New(),
		Kind:    KindSet,
		Brand:   brand,
		Images:  images,
		Source:  source,
		SetType: setType,
		SetCode: setCode,
	}
}

func (Item) TableName() string        { return "items" }
func (CardPrinted) TableName() string { return "items" }
func (Set) TableName() string         { return "items" }
func (ItemInfo) TableName() string    { return "item_infos" }

// Image holds the small and large variants of an image URL and is stored as
// JSONB inside Item and ItemInfo.
type Image struct {
	URLSmall string `json:"url_small"`
	URLLarge string `json:"url_large"`
}

// Brand defines the brand of the catalog item
type Brand string

const (
	// Yu-Gi-Oh!
	BrandYGO Brand = "YGO"

	// Yu-Gi-Oh! Rush Duel
	BrandYGORD Brand = "YGORD"

	BrandDS Brand = "DRAGON_SHIELD"
)

// Kind defines the kind of catalog item
type Kind string

const (
	// Card
	KindCard Kind = "CARD"

	// Set
	KindSet Kind = "SET"

	// Playmat
	KindPlaymat Kind = "PLAYMAT"

	// Sleeve
	KindSleeve Kind = "SLEEVE"
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
