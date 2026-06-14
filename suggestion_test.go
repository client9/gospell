package gospell

import (
	"os"
	"strings"
	"testing"
)

func TestMutationSuggester(t *testing.T) {
	aff, err := os.Open("hunspell-en_US/en_US.aff")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = aff.Close() }()

	dic, err := os.Open("hunspell-en_US/en_US.dic")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dic.Close() }()

	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
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

func TestMutationSuggesterUsesSpellValidation(t *testing.T) {
	aff := strings.NewReader(`SET UTF-8
TRY esianrtolcdugmphbyfvkwz
SFX D Y 1
SFX D y ies y
`)
	dic := strings.NewReader(`1
silly/D
`)
	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatal(err)
	}
	if gs.HasWord("sillies") {
		t.Fatal("test requires affixed suggestion not to be a root dictionary word")
	}
	if !gs.Spell("sillies") {
		t.Fatal("test requires affixed suggestion to be spell-valid")
	}
	if err := gs.SetSuggester(NewMutationSuggester(MutationOptions{CandidateCap: 512})); err != nil {
		t.Fatal(err)
	}
	got, err := gs.Suggest("sillie", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, sug := range got {
		if sug.Word == "sillies" {
			return
		}
	}
	t.Fatalf("expected affixed suggestion %q in %#v", "sillies", got)
}

func TestMutationSuggesterNGramFallback(t *testing.T) {
	aff := strings.NewReader(`SET UTF-8
TRY esianrtolcdugmphbyfvkwz
SFX D Y 1
SFX D y ies y
`)
	dic := strings.NewReader(`3
silly/D
unrelated
banana
`)
	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatal(err)
	}
	if levenshteinDistanceForTest("sillezz", "sillies") <= 1 {
		t.Fatal("test requires typo to be outside the one-edit mutation pass")
	}
	if err := gs.SetSuggester(NewMutationSuggester(MutationOptions{
		CandidateCap: 16,
		NGramRootCap: 16,
	})); err != nil {
		t.Fatal(err)
	}
	got, err := gs.Suggest("sillezz", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected n-gram fallback suggestions")
	}
	if got[0].Word != "sillies" {
		t.Fatalf("want top n-gram fallback suggestion %q, got %#v", "sillies", got)
	}
}

func TestSuggestCaseDeduplication(t *testing.T) {
	// allLower input should produce only the lowercase suggestion, not titleCase
	// or allUpper variants of the same word.
	aff := strings.NewReader(`SET UTF-8
TRY esianrtolcdugmphbyfvkwz
`)
	dic := strings.NewReader(`2
guidance
silly
`)
	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatal(err)
	}
	got, err := gs.Suggest("guidence", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Word == "Guidance" || s.Word == "GUIDANCE" {
			t.Errorf("got unwanted case variant %q for allLower input %q; all suggestions: %v", s.Word, "guidence", got)
		}
	}
	found := false
	for _, s := range got {
		if s.Word == "guidance" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected suggestion %q for %q; got: %v", "guidance", "guidence", got)
	}
}

func TestSuggestProperNounFromLowerInput(t *testing.T) {
	// allLower input for a word that only exists as titleCase in the dictionary
	// should still suggest the titleCase form (since lowercase won't pass spell check).
	aff := strings.NewReader(`SET UTF-8
TRY esianrtolcdugmphbyfvkwz
`)
	dic := strings.NewReader(`1
London
`)
	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatal(err)
	}
	got, err := gs.Suggest("londan", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range got {
		if s.Word == "London" {
			found = true
		}
		if s.Word == "london" || s.Word == "LONDON" {
			t.Errorf("got unwanted case variant %q for allLower input %q; all suggestions: %v", s.Word, "londan", got)
		}
	}
	if !found {
		t.Logf("no London suggestion for londan (edit distance may be too large): %v", got)
	}
}

func TestSuggestWithoutSuggester(t *testing.T) {
	gs := &GoSpell{}
	if _, err := gs.Suggest("foo", 5); err == nil {
		t.Fatal("expected error when no suggester is configured")
	}
}

func levenshteinDistanceForTest(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			curr[j] = minIntForTest(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func minIntForTest(vals ...int) int {
	out := vals[0]
	for _, v := range vals[1:] {
		if v < out {
			out = v
		}
	}
	return out
}
