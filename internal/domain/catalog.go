package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Item is the single denormalized catalog entity. Every catalog row lives in
// the items table, including sets, printed cards, sleeves and playmats.
type Item struct {
	ID uuid.UUID `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

	// Identity and common query fields.
	Kind        Kind       `json:"kind" gorm:"type:text;not null;index:idx_items_kind"`
	Brand       Brand      `json:"brand" gorm:"type:text;not null;index:idx_items_brand"`
	Lang        Lang       `json:"lang" gorm:"type:char(2);not null;index:idx_items_lang"`
	Code        string     `json:"code" gorm:"type:varchar(50);not null;default:'';index:idx_items_code"`
	SetID       *uuid.UUID `json:"set_id,omitempty" gorm:"type:uuid;index:idx_items_set_id"`
	Rarity      string     `json:"rarity" gorm:"type:varchar(50);not null;default:''"`
	Edition     string     `json:"edition" gorm:"type:text;not null;default:''"`
	ExtendedArt bool       `json:"extended_art" gorm:"not null;default:false"`

	// Source belongs to the identity. VerifiedSource is mutable: changing it
	// updates every row that shares the same source.
	Source         string `json:"source" gorm:"type:text;not null;default:''"`
	VerifiedSource bool   `json:"verified_source" gorm:"not null;default:false"`

	// Common display and query fields.
	Name        string    `json:"name" gorm:"type:text;not null;default:'';index:idx_items_name"`
	Description string    `json:"description" gorm:"type:text;not null;default:''"`
	Image       Image     `json:"image" gorm:"type:jsonb;serializer:json;not null;default:'{}'"`
	ReleaseAt   time.Time `json:"release_at"`

	// Properties holds every non-identity field, including the former ItemInfo
	// fields, card/set details and the variant attributes.
	Properties map[string]any `json:"properties" gorm:"type:jsonb;serializer:json;not null;default:'{}'"`

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// Image holds the small and large variants of an image URL.
type Image struct {
	URLSmall string `json:"url_small"`
	URLLarge string `json:"url_large"`
}

// Kind defines the kind of catalog item.
type Kind string

const (
	KindCard    Kind = "CARD"
	KindSet     Kind = "SET"
	KindPlaymat Kind = "PLAYMAT"
	KindSleeve  Kind = "SLEEVE"
)

// Brand defines the catalog brand.
type Brand string

const (
	BrandYGO   Brand = "YGO"
	BrandYGORD Brand = "YGORD"
	BrandDS    Brand = "DRAGON_SHIELD"
)

// Lang defines the language of the catalog item.
type Lang string

const (
	LangEN Lang = "EN"
	LangFR Lang = "FR"
	LangDE Lang = "DE"
	LangIT Lang = "IT"
	LangPT Lang = "PT"
	LangES Lang = "ES"
	LangJP Lang = "JP"
	LangAE Lang = "AE"
	LangKR Lang = "KR"
	LangTC Lang = "TC"
	LangSC Lang = "SC"
)

// IdentityKey returns a canonical representation of the upsert identity.
// Metadata changes such as Name or Description do not change this key.
func (i Item) IdentityKey() string {
	payload := map[string]any{
		"kind":         i.Kind,
		"brand":        i.Brand,
		"lang":         i.Lang,
		"code":         i.Code,
		"set_id":       i.SetID,
		"rarity":       i.Rarity,
		"edition":      i.Edition,
		"extended_art": i.ExtendedArt,
		"source":       i.Source,
		"variant":      i.Variant(),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(
			"%v|%v|%v|%v|%v|%v|%v|%v|%v|%v",
			payload["kind"], payload["brand"], payload["lang"], payload["code"],
			payload["set_id"], payload["rarity"], payload["edition"],
			payload["extended_art"], payload["source"], payload["variant"],
		)
	}
	return string(data)
}

// DeduplicateItems removes items that share an identity, keeping the last
// occurrence. When a dropped item is a set referenced by another item's SetID,
// the reference is remapped to the kept set, so the upsert never receives two
// rows with the same identity. Callers must assign IDs before calling it.
func DeduplicateItems(items []Item) []Item {
	unique, aliases := dedup(items)
	if len(aliases) == 0 {
		return unique
	}

	applySetAliases(unique, aliases)

	// Remapping a set reference can merge two prints that pointed to duplicate
	// sets. A second pass guarantees the database never receives the same
	// identity twice in one upsert.
	unique, _ = dedup(unique)

	return unique
}

func dedup(items []Item) ([]Item, map[uuid.UUID]uuid.UUID) {
	unique := make([]Item, 0, len(items))
	positions := make(map[string]int, len(items))
	aliases := make(map[uuid.UUID]uuid.UUID, len(items))

	for _, item := range items {
		key := item.IdentityKey()
		if position, ok := positions[key]; ok {
			existing := unique[position]
			if item.ID != existing.ID {
				aliases[item.ID] = existing.ID
			}
			item.ID = existing.ID
			unique[position] = item
			continue
		}

		positions[key] = len(unique)
		unique = append(unique, item)
	}

	return unique, aliases
}

func applySetAliases(items []Item, aliases map[uuid.UUID]uuid.UUID) {
	for index := range items {
		if items[index].SetID == nil {
			continue
		}
		if id, ok := aliases[*items[index].SetID]; ok {
			setID := id
			items[index].SetID = &setID
		}
	}
}

// Variant returns the variant attributes stored inside Properties.
func (i Item) Variant() map[string]any {
	if i.Properties == nil {
		return nil
	}
	variant, _ := i.Properties[PropertyVariant].(map[string]any)
	return variant
}

// NewCardPrinted marks item as a printed card and applies its specific fields.
func NewCardPrinted(
	item Item,
	setID *uuid.UUID,
	serialCode, rarity, rarityCode, edition string,
	extendedArt bool,
	guaranteedCopies uint,
) *Item {
	item.Kind = KindCard
	item.SetID = setID
	item.Rarity = rarity
	item.Edition = edition
	item.ExtendedArt = extendedArt
	item.Properties = ensureProperties(item.Properties)
	item.Properties[PropertySerialCode] = serialCode
	item.Properties[PropertyRarityCode] = rarityCode
	item.Properties[PropertyGuaranteedCopies] = guaranteedCopies
	return &item
}

// NewSet marks item as a set and applies its specific fields.
func NewSet(item Item, regionalCode string, cardsPerPack, packsPerSet uint) *Item {
	item.Kind = KindSet
	item.Properties = ensureProperties(item.Properties)
	item.Properties[PropertyRegionalCode] = regionalCode
	item.Properties[PropertyQuantityCardsPerPack] = cardsPerPack
	item.Properties[PropertyQuantityPacksPerSet] = packsPerSet
	return &item
}

// NewSleeve marks item as a sleeve and stores its variant attributes.
func NewSleeve(item Item, variant map[string]any) *Item {
	item.Kind = KindSleeve
	item.Properties = ensureProperties(item.Properties)
	item.Properties[PropertyVariant] = variant
	return &item
}

// NewPlaymat marks item as a playmat and stores its variant attributes.
func NewPlaymat(item Item, variant map[string]any) *Item {
	item.Kind = KindPlaymat
	item.Properties = ensureProperties(item.Properties)
	item.Properties[PropertyVariant] = variant
	return &item
}

func ensureProperties(properties map[string]any) map[string]any {
	if properties == nil {
		return make(map[string]any)
	}
	return properties
}

// Property keys used by the denormalized Item.Properties bag.
const (
	PropertyVariant              = "variant"
	PropertyImages               = "images"
	PropertySerialCode           = "serial_code"
	PropertyRarityCode           = "rarity_code"
	PropertyGuaranteedCopies     = "guaranteed_copies"
	PropertyRegionalCode         = "regional_code"
	PropertyQuantityCardsPerPack = "quantity_cards_per_pack"
	PropertyQuantityPacksPerSet  = "quantity_packs_per_set"
	PropertySetType              = "set_type"
	PropertySetCode              = "set_code"
	PropertyArchetype            = "archetype"
	PropertyCardTypes            = "card_types"
)

// Property returns a raw property value.
func (i Item) Property(key string) any {
	if i.Properties == nil {
		return nil
	}
	return i.Properties[key]
}

// SetProperty stores a property value.
func (i *Item) SetProperty(key string, value any) {
	i.Properties = ensureProperties(i.Properties)
	if value == nil {
		delete(i.Properties, key)
		return
	}
	i.Properties[key] = value
}

// PropertyString returns a string property.
func (i Item) PropertyString(key string) string {
	value, _ := i.Property(key).(string)
	return value
}

// PropertyUint returns an unsigned property. JSON round trips turn numbers into
// float64, so numeric forms are accepted.
func (i Item) PropertyUint(key string) uint {
	switch value := i.Property(key).(type) {
	case uint:
		return value
	case int:
		if value > 0 {
			return uint(value)
		}
	case float64:
		if value > 0 {
			return uint(value)
		}
	case string:
		if parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64); err == nil {
			return uint(parsed)
		}
	}
	return 0
}

// PropertyBool returns a boolean property.
func (i Item) PropertyBool(key string) bool {
	value, _ := i.Property(key).(bool)
	return value
}

// PropertyImages returns the shared images stored in Properties.
func (i Item) PropertyImages() []Image {
	switch value := i.Property(PropertyImages).(type) {
	case []Image:
		return value
	case []any:
		images := make([]Image, 0, len(value))
		for _, entry := range value {
			images = append(images, imageFromAny(entry))
		}
		return images
	default:
		return nil
	}
}

// SetPropertyImages stores the shared images in Properties.
func (i *Item) SetPropertyImages(images []Image) {
	if len(images) == 0 {
		i.SetProperty(PropertyImages, nil)
		return
	}
	i.SetProperty(PropertyImages, images)
}

func imageFromAny(value any) Image {
	entry, ok := value.(map[string]any)
	if !ok {
		return Image{}
	}
	small, _ := entry["url_small"].(string)
	large, _ := entry["url_large"].(string)
	return Image{URLSmall: small, URLLarge: large}
}
