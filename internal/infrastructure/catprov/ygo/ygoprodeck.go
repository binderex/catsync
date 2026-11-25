package ygo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/binderex/catsync/internal/domain"
)

type cardImage struct {
	URL        string `json:"image_url"`
	URLSmall   string `json:"image_url_small"`
	URLCropped string `json:"image_url_cropped"`
}

type miscInfo struct {
	TCGDate string `json:"tcg_date"`
	OCGDate string `json:"ocg_date"`
}

type ygoproCard struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
	Types       string      `json:"humanReadableCardType"`
	Description string      `json:"desc"`
	Archetype   string      `json:"archetype,omitempty"`
	ATK         int         `json:"atk,omitempty"`
	DEF         int         `json:"def,omitempty"`
	Race        string      `json:"race,omitempty"`
	Attribute   string      `json:"attribute,omitempty"`
	Level       int         `json:"level,omitempty"`
	Scale       int         `json:"scale,omitempty"`
	LinkValue   int         `json:"linkval,omitempty"`
	LinkMarkers []string    `json:"linkmarkers"`
	Images      []cardImage `json:"card_images"`
	MiscInfo    []miscInfo  `json:"misc_info"`
}

// MarshalJSON clears the race field for Spell and Trap cards, since it has no
// meaning for those card types.
func (c ygoproCard) MarshalJSON() ([]byte, error) {
	type card ygoproCard

	if strings.Contains(c.Types, "Trap") || strings.Contains(c.Types, "Spell") {
		c.Race = ""
	}

	return json.Marshal(card(c))
}

// toItem maps the card to a domain.Item that carries the shared card info,
// using source as the item source identifier. The card types are split into an
// array and reversed so the most specific type comes first.
func (c ygoproCard) toItem(source string) *domain.Item {
	images := make([]domain.Image, 0, len(c.Images))
	for _, image := range c.Images {
		images = append(images, domain.Image{
			URLSmall: image.URLSmall,
			URLLarge: image.URL,
		})
	}

	var cardTypes []string
	if c.Types != "" {
		cardTypes = strings.Split(c.Types, " ")
		slices.Reverse(cardTypes)
	}

	properties := map[string]any{
		"card_types":  cardTypes,
		"atk":         c.ATK,
		"def":         c.DEF,
		"race":        c.Race,
		"attribute":   c.Attribute,
		"level":       c.Level,
		"scale":       c.Scale,
		"linkval":     c.LinkValue,
		"linkmarkers": c.LinkMarkers,
	}
	for key, value := range properties {
		if reflect.ValueOf(value).IsZero() {
			delete(properties, key)
		}
	}

	info := newCardItem(
		domain.BrandYGO,
		images,
		source,
		c.Archetype,
		strings.Join(cardTypes, " "),
		properties,
	)
	info.VerifiedSource = true

	return info
}

func findYGOPROCard(name string, cards []ygoproCard) (*ygoproCard, error) {
	name = cleanText(name)
	for i := range cards {
		if strings.EqualFold(cleanText(cards[i].Name), name) {
			return &cards[i], nil
		}
	}

	return nil, fmt.Errorf("ygopro card %q not found", name)
}

// fetchYGOPRODECKCards downloads the YGOPRODeck card catalog and returns the
// cards as ygoproCard values.
func (p *Provider) fetchYGOPRODECKCards(ctx context.Context, name string) ([]ygoproCard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/cardinfo.php?misc=yes", ygoprodeckBaseURL)
	if name != "" {
		reqURL = fmt.Sprintf("%s/cardinfo.php?fname=%s&misc=yes", ygoprodeckBaseURL, url.QueryEscape(name))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		var netErr net.Error
		if (errors.As(err, &netErr) && netErr.Timeout()) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrClientTimeout
		}
		return nil, fmt.Errorf("request ygopro cards: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusNotFound:
		return nil, ErrCardsNotFound
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	var response struct {
		Data []ygoproCard `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode ygopro cards: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, ErrCardsNotFound
	}

	return response.Data, nil
}

// ygoproCatalog loads the full YGOPRODeck catalog once and caches it. The set
// release flow uses it so it does not need one API request per card.
func (p *Provider) ygoproCatalog(ctx context.Context) ([]ygoproCard, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.catalogLoaded {
		return p.catalog, p.catalogErr
	}

	cards, err := p.fetchYGOPRODECKCards(ctx, "")
	p.catalog = cards
	p.catalogErr = err
	p.catalogLoaded = true
	if err == nil {
		p.catalogByName = make(map[string]*ygoproCard, len(cards))
		for index := range cards {
			p.catalogByName[strings.ToLower(cleanText(cards[index].Name))] = &cards[index]
		}
	}

	return cards, err
}

// findItemInfoYGOPRODECK resolves the shared card info of a card. When the
// full catalog was preloaded it searches it; otherwise it performs a single
// name query so direct scraper calls do not download the whole domain.
func (p *Provider) findItemInfoYGOPRODECK(ctx context.Context, name, source string) (*domain.Item, error) {
	name = cleanText(name)
	if name == "" {
		return nil, ErrCardsNotFound
	}

	p.mu.Lock()
	loaded := p.catalogLoaded
	cardByName := p.catalogByName[strings.ToLower(cleanText(name))]
	p.mu.Unlock()

	if loaded && cardByName != nil {
		return cardByName.toItem(source), nil
	}

	cards, err := p.fetchYGOPRODECKCards(ctx, name)
	if err != nil {
		return nil, err
	}

	card, err := findYGOPROCard(name, cards)
	if err != nil {
		return nil, err
	}

	return card.toItem(source), nil
}
