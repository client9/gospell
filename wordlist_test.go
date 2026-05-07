package gospell

import (
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
