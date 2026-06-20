package gospell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestGoSpell(t *testing.T) *GoSpell {
	t.Helper()
	gs, err := NewGoSpellReader(
		strings.NewReader(""),
		strings.NewReader("3\nfoo\nbar\nbaz\n"),
	)
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	return gs
}

func TestWordListParse(t *testing.T) {
	input := `
# comment line
golang
Kubernetes
*irregardless
  *  spaced

`
	wl, err := NewWordList(strings.NewReader(input))
	if err != nil {
		t.Fatalf("NewWordList: %v", err)
	}

	for _, w := range []string{"golang", "Kubernetes"} {
		if !wl.HasWord(w) {
			t.Errorf("HasWord(%q) = false, want true", w)
		}
	}
	for _, w := range []string{"irregardless", "spaced"} {
		if !wl.IsForbidden(w) {
			t.Errorf("IsForbidden(%q) = false, want true", w)
		}
	}
	if wl.HasWord("irregardless") {
		t.Error("HasWord(irregardless) = true, want false")
	}
	if wl.IsForbidden("golang") {
		t.Error("IsForbidden(golang) = true, want false")
	}
}

func TestWordListNilSafe(t *testing.T) {
	var wl *WordList
	if wl.HasWord("anything") {
		t.Error("nil WordList HasWord should return false")
	}
	if wl.IsForbidden("anything") {
		t.Error("nil WordList IsForbidden should return false")
	}
}

func TestWordListAddForbid(t *testing.T) {
	wl := &WordList{}
	wl.Add("foo")
	wl.Forbid("bar")
	if !wl.HasWord("foo") {
		t.Error("HasWord(foo) = false after Add")
	}
	if !wl.IsForbidden("bar") {
		t.Error("IsForbidden(bar) = false after Forbid")
	}
}

func TestCheckerSpellFallsThrough(t *testing.T) {
	c := NewChecker(makeTestGoSpell(t))
	for _, w := range []string{"foo", "bar", "baz"} {
		if !c.Spell(w) {
			t.Errorf("Spell(%q) = false, want true (base dict word)", w)
		}
	}
	if c.Spell("unknown") {
		t.Error("Spell(unknown) = true, want false")
	}
}

func TestCheckerWordListAllows(t *testing.T) {
	c := NewChecker(makeTestGoSpell(t))
	wl := &WordList{}
	wl.Add("golang")
	c.AddWordList(wl)

	if !c.Spell("golang") {
		t.Error("Spell(golang) = false, want true (in WordList)")
	}
	if !c.Spell("foo") {
		t.Error("Spell(foo) = false, want true (in base dict)")
	}
}

func TestCheckerWordListForbids(t *testing.T) {
	c := NewChecker(makeTestGoSpell(t))
	wl := &WordList{}
	wl.Forbid("foo") // foo is in the base dict
	c.AddWordList(wl)

	if c.Spell("foo") {
		t.Error("Spell(foo) = true, want false (forbidden in WordList)")
	}
	if !c.Spell("bar") {
		t.Error("Spell(bar) = false, want true (not forbidden)")
	}
}

func TestCheckerMultipleWordLists(t *testing.T) {
	c := NewChecker(makeTestGoSpell(t))
	wl1 := &WordList{}
	wl1.Add("alpha")
	wl2 := &WordList{}
	wl2.Add("beta")
	wl2.Forbid("foo")
	c.AddWordList(wl1)
	c.AddWordList(wl2)

	if !c.Spell("alpha") {
		t.Error("Spell(alpha) = false, want true")
	}
	if !c.Spell("beta") {
		t.Error("Spell(beta) = false, want true")
	}
	if c.Spell("foo") {
		t.Error("Spell(foo) = true, want false (forbidden in wl2)")
	}
}

func TestCheckerSuggestSkipsForbidden(t *testing.T) {
	gs, err := NewGoSpellReader(
		strings.NewReader(""),
		strings.NewReader("1\nfoo\n"),
	)
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	c := NewChecker(gs)

	wl1 := &WordList{}
	wl1.Add("colour")
	wl2 := &WordList{}
	wl2.Forbid("colour")
	c.AddWordList(wl1)
	c.AddWordList(wl2)

	if c.Spell("colour") {
		t.Error("Spell(colour) = true, want false (forbidden)")
	}

	suggestions, err := c.Suggest("color", 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	for _, s := range suggestions {
		if s.Word == "colour" {
			t.Errorf("Suggest returned forbidden word %q", s.Word)
		}
	}
}

func TestCheckerSuggestFiltersBaseDictForbidden(t *testing.T) {
	// A word in the base dictionary that is forbidden by an active WordList
	// must not appear in Suggest results.
	gs, err := NewGoSpellReader(
		strings.NewReader(""),
		strings.NewReader("2\ncolour\ncolor\n"),
	)
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	c := NewChecker(gs)
	wl := &WordList{}
	wl.Forbid("colour")
	c.AddWordList(wl)

	suggestions, err := c.Suggest("colur", 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	for _, s := range suggestions {
		if s.Word == "colour" {
			t.Errorf("Suggest returned base-dict word %q forbidden by WordList", s.Word)
		}
	}
}

func TestCheckerRemoveWordList(t *testing.T) {
	c := NewChecker(makeTestGoSpell(t))
	wl := &WordList{}
	wl.Add("tempword")
	c.AddWordList(wl)

	if !c.Spell("tempword") {
		t.Error("Spell(tempword) = false before remove")
	}
	c.RemoveWordList(wl)
	if c.Spell("tempword") {
		t.Error("Spell(tempword) = true after remove, want false")
	}
}

func TestNewWordListFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "custom.txt")
	if err := os.WriteFile(path, []byte("golang\n*irregardless\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	wl, err := NewWordListFile(path)
	if err != nil {
		t.Fatalf("NewWordListFile: %v", err)
	}
	if !wl.HasWord("golang") {
		t.Error("HasWord(golang) = false")
	}
	if !wl.IsForbidden("irregardless") {
		t.Error("IsForbidden(irregardless) = false")
	}
}

func TestNewWordListFileNotFound(t *testing.T) {
	if _, err := NewWordListFile("/no/such/file/here.txt"); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestCheckerInputConversion(t *testing.T) {
	aff := "ICONV 1\nICONV th t\n"
	gs, err := NewGoSpellReader(strings.NewReader(aff), strings.NewReader("1\ntest\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	if got := NewChecker(gs).InputConversion([]byte("thest")); got != "test" {
		t.Errorf("InputConversion = %q, want %q", got, "test")
	}
}

func TestCheckerSuggestZeroLimit(t *testing.T) {
	got, err := NewChecker(makeTestGoSpell(t)).Suggest("foo", 0)
	if err != nil {
		t.Fatalf("Suggest(limit=0): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Suggest(limit=0) = %v, want empty", got)
	}
}

func TestCheckerSuggestWordListWordForbiddenByOtherList(t *testing.T) {
	gs, err := NewGoSpellReader(strings.NewReader(""), strings.NewReader("1\nfoo\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	c := NewChecker(gs)
	wl1 := &WordList{}
	wl1.Add("colour")
	wl2 := &WordList{}
	wl2.Forbid("colour")
	c.AddWordList(wl1)
	c.AddWordList(wl2)
	suggestions, err := c.Suggest("color", 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	for _, s := range suggestions {
		if s.Word == "colour" {
			t.Error("Suggest returned WordList word forbidden by another list")
		}
	}
}

func TestCheckerSuggestSkipsQueryWord(t *testing.T) {
	gs, err := NewGoSpellReader(strings.NewReader(""), strings.NewReader("1\nfoo\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	c := NewChecker(gs)
	wl := &WordList{}
	wl.Add("bar")
	c.AddWordList(wl)
	for _, s := range func() []Suggestion {
		s, _ := c.Suggest("bar", 10)
		return s
	}() {
		if s.Word == "bar" {
			t.Errorf("Suggest returned the query word %q itself", s.Word)
		}
	}
}

func TestCheckerSuggestDeduplicatesWordList(t *testing.T) {
	gs, err := NewGoSpellReader(strings.NewReader(""), strings.NewReader("2\nfoo\nfob\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	c := NewChecker(gs)
	wl := &WordList{}
	wl.Add("foo") // already in base dict, so it lands in `seen`
	c.AddWordList(wl)
	suggestions, err := c.Suggest("fop", 10)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	count := 0
	for _, s := range suggestions {
		if s.Word == "foo" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("Suggest returned %q %d times, want at most 1", "foo", count)
	}
}
