package catalog

import (
	"testing"

	"github.com/binderex/catsync/internal/domain"
)

func TestSourceVerificationValues(t *testing.T) {
	values := sourceVerificationValues([]domain.Item{
		{Source: "a", VerifiedSource: false},
		{Source: "a", VerifiedSource: true},
		{Source: "b", VerifiedSource: true},
		{Source: "", VerifiedSource: true},
	})

	if !values["a"] {
		t.Error("last value for source a was not kept")
	}
	if !values["b"] {
		t.Error("source b was not kept")
	}
	if _, ok := values[""]; ok {
		t.Error("empty source must be ignored")
	}
}

func TestSplitSets(t *testing.T) {
	sets, others := splitSets([]domain.Item{
		{Kind: domain.KindSet},
		{Kind: domain.KindCard},
		{Kind: domain.KindSleeve},
	})

	if len(sets) != 1 || len(others) != 2 {
		t.Fatalf("sets=%d others=%d", len(sets), len(others))
	}
}
