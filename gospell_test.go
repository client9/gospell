package gospell

import (
	"errors"
	"strings"
	"testing"
)

type failingSuggester struct{}

func (f *failingSuggester) Init(_ SuggestionSource) error                 { return errors.New("init always fails") }
func (f *failingSuggester) Suggest(_ string, _ int) ([]Suggestion, error) { return nil, nil }

func TestGoSpellWordCount(t *testing.T) {
	if got := makeTestGoSpell(t).WordCount(); got != 3 {
		t.Errorf("WordCount() = %d, want 3", got)
	}
}

func TestGoSpellMaxWordLen(t *testing.T) {
	gs, err := NewGoSpellReader(strings.NewReader(""), strings.NewReader("2\nhello\nworldly\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	if got := gs.MaxWordLen(); got < 7 {
		t.Errorf("MaxWordLen() = %d, want >= 7 (len of 'worldly')", got)
	}
}

func TestGoSpellForEachWord(t *testing.T) {
	gs := makeTestGoSpell(t)
	collected := make(map[string]struct{})
	gs.ForEachWord(func(w string) bool {
		collected[w] = struct{}{}
		return true
	})
	for _, w := range []string{"foo", "bar", "baz"} {
		if _, ok := collected[w]; !ok {
			t.Errorf("ForEachWord did not yield %q", w)
		}
	}
}

func TestGoSpellForEachWordEarlyStop(t *testing.T) {
	count := 0
	makeTestGoSpell(t).ForEachWord(func(_ string) bool {
		count++
		return false
	})
	if count != 1 {
		t.Errorf("expected early stop after 1 word, got count=%d", count)
	}
}

func TestGoSpellAddWordRaw(t *testing.T) {
	gs := makeTestGoSpell(t)
	if !gs.AddWordRaw("quux") {
		t.Error("AddWordRaw(quux): want true (new word)")
	}
	if !gs.Spell("quux") {
		t.Error("Spell(quux) = false after AddWordRaw")
	}
	if gs.AddWordRaw("quux") {
		t.Error("AddWordRaw(quux) second call: want false (already present)")
	}
}

func TestGoSpellAddWordRawWithSpace(t *testing.T) {
	gs := makeTestGoSpell(t)
	if !gs.AddWordRaw("spell check") {
		t.Error("AddWordRaw('spell check'): want true")
	}
	if gs.blockedCompound == nil {
		t.Fatal("blockedCompound map is nil after adding spaced word")
	}
	if _, ok := gs.blockedCompound["spellcheck"]; !ok {
		t.Error("expected 'spellcheck' in blockedCompound")
	}
}

func TestGoSpellAddWordRawUpdatesRuneLenIndex(t *testing.T) {
	gs := makeTestGoSpell(t)
	gs.rebuildRuneLenIndex()
	before := len(gs.dictByRuneLen[6])
	gs.AddWordRaw("foobar") // 6 runes
	if got := len(gs.dictByRuneLen[6]); got != before+1 {
		t.Errorf("dictByRuneLen[6] grew by %d, want 1", got-before)
	}
}

func TestGoSpellInputConversion(t *testing.T) {
	gs, err := NewGoSpellReader(
		strings.NewReader("ICONV 1\nICONV ph f\n"),
		strings.NewReader("1\nfun\n"),
	)
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	if got := gs.InputConversion([]byte("phun")); got != "fun" {
		t.Errorf("InputConversion = %q, want %q", got, "fun")
	}
}

func TestSetSuggesterNil(t *testing.T) {
	gs := makeTestGoSpell(t)
	if err := gs.SetSuggester(nil); err != nil {
		t.Errorf("SetSuggester(nil) error: %v", err)
	}
	if gs.suggester != nil {
		t.Error("suggester should be nil after SetSuggester(nil)")
	}
}

func TestSetSuggesterInitError(t *testing.T) {
	if err := makeTestGoSpell(t).SetSuggester(&failingSuggester{}); err == nil {
		t.Error("SetSuggester with failing Init: want error, got nil")
	}
}

func TestGoSpellSuggestNoSuggester(t *testing.T) {
	gs, err := NewGoSpellReader(strings.NewReader(""), strings.NewReader("1\nfoo\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	_ = gs.SetSuggester(nil)
	if _, err := gs.Suggest("fo", 5); err == nil {
		t.Error("Suggest with no suggester: want error, got nil")
	}
}

func TestEnsureLazySurfaceIdempotent(t *testing.T) {
	gs := makeTestGoSpell(t)
	gs.ensureLazySurface("foo")
	gs.ensureLazySurface("foo") // second call hits the early-return branch
	if !gs.Spell("foo") {
		t.Error("Spell(foo) = false after double ensureLazySurface")
	}
}
