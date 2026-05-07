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

func TestSymSpellSuggester(t *testing.T) {
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

	suggester := NewSymSpellSuggester(SymSpellOptions{
		MaxDistance:  2,
		PrefixLength: 7,
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
	if got[0].Score != 1 {
		t.Fatalf("want top score 1, got %d", got[0].Score)
	}
	for _, sug := range got {
		if sug.Word == "sillly" {
			t.Fatalf("unexpected exact match in suggestions: %#v", got)
		}
	}

	again, err := gs.Suggest("sillly", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(got) {
		t.Fatalf("repeat suggest changed result length: first=%d second=%d", len(got), len(again))
	}
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("repeat suggest changed result at %d: first=%#v second=%#v", i, got[i], again[i])
		}
	}
}

func TestMutationSuggester(t *testing.T) {
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

	suggester := NewMutationSuggester(MutationOptions{
		CandidateCap: 256,
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
	if got[0].Score != 1 {
		t.Fatalf("want top score 1, got %d", got[0].Score)
	}

	again, err := gs.Suggest("sillly", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(got) {
		t.Fatalf("repeat suggest changed result length: first=%d second=%d", len(got), len(again))
	}
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("repeat suggest changed result at %d: first=%#v second=%#v", i, got[i], again[i])
		}
	}
}

func TestSuggestWithoutSuggester(t *testing.T) {
	gs := &GoSpell{}
	if _, err := gs.Suggest("foo", 5); err == nil {
		t.Fatal("expected error when no suggester is configured")
	}
}
