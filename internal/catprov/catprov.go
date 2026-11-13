package catprov

import (
	"errors"
	"time"

	"github.com/binderex/catsync/internal/catalog"
)

var (
	ErrClientTimeout   = errors.New("client timeout exceeded")
	ErrTooManyRequests = errors.New("too many requests, please try again later")
	ErrPageNotFound    = errors.New("page not found")
	ErrInvalidPage     = errors.New("page response from provider")
	ErrClientForbidden = errors.New("client forbidden")

	ErrCardsNotFound      = errors.New("cards not found")
	ErrCardPrintsNotFound = errors.New("card prints not found")
	ErrSetNotFound        = errors.New("set not found")

	// errNextCandidate means the current page is not usable and the next
	// candidate can be tried.
	errNextCandidate = errors.New("try next candidate")
)

const (
	browserUserAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
	cardPrintsMaxAttempts      = 3
	cardPrintsTooManyAttempts  = 4
	cardPrintsBlockedAttempts  = 3
	cardPrintsRetryDelay       = 2 * time.Second
	cardPrintsTooManyBaseDelay = 5 * time.Second
	cardPrintsBlockedBaseDelay = 30 * time.Second
	cardPrintsMaxDelay         = 2 * time.Minute
)

type CatalogProvider interface {
	GetCards(name string, max time.Time) ([]catalog.CatalogItem, error)

	GetCardPrints(card catalog.CatalogItem) ([]catalog.CatalogItem, error)

	GetSets(cards []catalog.CatalogItem) ([]catalog.CatalogItem, error)
}
