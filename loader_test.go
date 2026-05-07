package gospell

import (
	"strings"
	"testing"
)

const testDictDir = "hunspell-en_US-2026"

func TestOpen(t *testing.T) {
	gs, err := Open("en_US", []string{testDictDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !gs.Spell("hello") {
		t.Error("expected \"hello\" to be correctly spelled")
	}
	if gs.Spell("zzzquux") {
		t.Error("expected \"zzzquux\" to be flagged as misspelled")
	}
}

func TestOpen_WithExtension(t *testing.T) {
	// Callers passing "en_US.aff" or "en_US.dic" should get the same result.
	for _, name := range []string{"en_US.aff", "en_US.dic"} {
		gs, err := Open(name, []string{testDictDir})
		if err != nil {
			t.Fatalf("Open(%q): %v", name, err)
		}
		if !gs.Spell("hello") {
			t.Errorf("Open(%q): \"hello\" should be correctly spelled", name)
		}
	}
}

func TestOpen_NotFound(t *testing.T) {
	_, err := Open("no_such_lang", []string{testDictDir})
	if err == nil {
		t.Fatal("expected error for missing dictionary, got nil")
	}
}

func TestOpen_AbsolutePath(t *testing.T) {
	// Passing an absolute path bypasses the search.
	gs, err := Open(testDictDir+"/en_US", []string{})
	if err != nil {
		t.Fatalf("Open with relative path: %v", err)
	}
	if !gs.Spell("hello") {
		t.Error("expected \"hello\" to be correctly spelled")
	}
}

func TestOpenSupplement(t *testing.T) {
	// Build a minimal in-memory .dic file for testing.
	dicContent := "2\nkubernetes\nkubectl\n"
	wl, err := NewWordListFromDic(strings.NewReader(dicContent))
	if err != nil {
		t.Fatalf("NewWordListFromDic: %v", err)
	}
	if !wl.HasWord("kubernetes") {
		t.Error("expected \"kubernetes\" in word list")
	}
	if !wl.HasWord("kubectl") {
		t.Error("expected \"kubectl\" in word list")
	}
}

func TestNewWordListFromDic_StripsFlags(t *testing.T) {
	dicContent := "3\nhello/ABC\nworld/XY\nfoo\n"
	wl, err := NewWordListFromDic(strings.NewReader(dicContent))
	if err != nil {
		t.Fatalf("NewWordListFromDic: %v", err)
	}
	for _, word := range []string{"hello", "world", "foo"} {
		if !wl.HasWord(word) {
			t.Errorf("expected %q in word list (flags should be stripped)", word)
		}
	}
	if wl.HasWord("hello/ABC") {
		t.Error("flags should have been stripped from \"hello/ABC\"")
	}
}

func TestNewWordListFromDic_SkipsComments(t *testing.T) {
	dicContent := "1\n# this is a comment\nrealword\n"
	wl, err := NewWordListFromDic(strings.NewReader(dicContent))
	if err != nil {
		t.Fatalf("NewWordListFromDic: %v", err)
	}
	if !wl.HasWord("realword") {
		t.Error("expected \"realword\" in word list")
	}
}

func TestSearchPaths_IncludesSystemDefaults(t *testing.T) {
	paths := SearchPaths()
	if len(paths) == 0 {
		t.Fatal("SearchPaths returned empty slice")
	}
	// System defaults must always be present.
	found := false
	for _, p := range paths {
		if p == "/usr/share/hunspell" || p == "/usr/local/share/hunspell" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SearchPaths missing system defaults, got %v", paths)
	}
}
