package gospell

import (
	"os"
	"testing"
)

func TestLevenshteinSuggester(t *testing.T) {
	aff, err := os.Open("hunspell-en_US-2026/en_US.aff")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = aff.Close() }()

	dic, err := os.Open("hunspell-en_US-2026/en_US.dic")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dic.Close() }()

	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatal(err)
	}

	suggester := NewLevenshteinSuggester(LevenshteinOptions{MaxDistance: 2})
	if err := gs.SetSuggester(suggester); err != nil {
		t.Fatal(err)
	}

	got, err := gs.Suggest("sillly", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one suggestion")
	}
	if got[0].Word != "silly" {
		t.Fatalf("want top suggestion %q, got %q", "silly", got[0].Word)
	}
	if got[0].Score != 1 {
		t.Fatalf("want top score 1, got %d", got[0].Score)
	}
}

func TestTrigramSuggester(t *testing.T) {
	aff, err := os.Open("hunspell-en_US-2026/en_US.aff")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = aff.Close() }()

	dic, err := os.Open("hunspell-en_US-2026/en_US.dic")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dic.Close() }()

	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatal(err)
	}

	suggester := NewTrigramSuggester(TrigramOptions{
		RerankLimit:   32,
		MaxLengthDiff: 4,
	})
	if err := gs.SetSuggester(suggester); err != nil {
		t.Fatal(err)
	}

	got, err := gs.Suggest("sillly", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one suggestion")
	}
	if got[0].Word != "silly" {
		t.Fatalf("want top suggestion %q, got %q", "silly", got[0].Word)
	}
}

func TestSuggestWithoutSuggester(t *testing.T) {
	gs := &GoSpell{}
	if _, err := gs.Suggest("foo", 5); err == nil {
		t.Fatal("expected error when no suggester is configured")
	}
}
