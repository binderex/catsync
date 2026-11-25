package retrier

import (
	"bytes"

	"github.com/PuerkitoBio/goquery"

	"github.com/binderex/catsync/internal/domain"
)

// RejectSelector returns a filter that marks a response invalid when any of the
// CSS selectors matches the response body.
func RejectSelector(selectors ...string) domain.Filter {
	return func(response domain.Response) bool {
		if len(response.Body) == 0 || len(selectors) == 0 {
			return false
		}

		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(response.Body))
		if err != nil {
			return false
		}

		for _, selector := range selectors {
			if doc.Find(selector).Length() > 0 {
				return true
			}
		}

		return false
	}
}

// RejectWhen adapts a predicate to a Filter.
func RejectWhen(predicate func(domain.Response) bool) domain.Filter {
	return domain.Filter(predicate)
}
