package gospell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testDictDir = "hunspell-en_US"

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

func TestAddDictionaryReader(t *testing.T) {
	// Minimal .aff with a suffix rule: flag S appends "s" to any word.
	aff := "SFX S Y 1\nSFX S   0     s .\n"
	dic := "2\nhello\nworld/S\n"
	gs, err := NewGoSpellReader(strings.NewReader(aff), strings.NewReader(dic))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	if !gs.Spell("hello") {
		t.Error("baseline: \"hello\" should spell correctly before AddDictionaryReader")
	}
	if !gs.Spell("worlds") {
		t.Error("baseline: \"worlds\" should spell correctly (suffix expansion) before AddDictionaryReader")
	}

	extra := "2\nfrobulate\nwidget/S\n"
	if err := gs.AddDictionaryReader(strings.NewReader(extra)); err != nil {
		t.Fatalf("AddDictionaryReader: %v", err)
	}

	// Original words still work.
	if !gs.Spell("hello") {
		t.Error("\"hello\" should still spell correctly after AddDictionaryReader")
	}
	// Supplemental stem with no flags.
	if !gs.Spell("frobulate") {
		t.Error("\"frobulate\" (supplemental stem) should spell correctly")
	}
	// Supplemental stem with affix expansion — the key improvement over WordList.
	if !gs.Spell("widget") {
		t.Error("\"widget\" (supplemental stem) should spell correctly")
	}
	if !gs.Spell("widgets") {
		t.Error("\"widgets\" (affix-expanded from supplemental dic) should spell correctly")
	}
	// Unknown words still fail.
	if gs.Spell("zzzquux") {
		t.Error("\"zzzquux\" should still be flagged as misspelled")
	}
}

func TestAddDictionaryReader_NoCountHeader(t *testing.T) {
	gs, err := NewGoSpellReader(strings.NewReader(""), strings.NewReader("1\nhello\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	// .dic without a word-count header line — first line treated as a word entry.
	if err := gs.AddDictionaryReader(strings.NewReader("widget\n")); err != nil {
		t.Fatalf("AddDictionaryReader without header: %v", err)
	}
	if !gs.Spell("widget") {
		t.Error("\"widget\" should spell correctly when supplemental dic has no count header")
	}
}

func TestAddDictionaryFile(t *testing.T) {
	aff := "SFX S Y 1\nSFX S   0     s .\n"
	gs, err := NewGoSpellReader(strings.NewReader(aff), strings.NewReader("1\nhello\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	tmp := t.TempDir()
	dicPath := filepath.Join(tmp, "extra.dic")
	if err := os.WriteFile(dicPath, []byte("1\nwidget/S\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := gs.AddDictionaryFile(dicPath); err != nil {
		t.Fatalf("AddDictionaryFile: %v", err)
	}
	if !gs.Spell("widget") {
		t.Error("\"widget\" should spell correctly after AddDictionaryFile")
	}
	if !gs.Spell("widgets") {
		t.Error("\"widgets\" (affix-expanded) should spell correctly after AddDictionaryFile")
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

func TestAddDicFromFile(t *testing.T) {
	aff := "SFX S Y 1\nSFX S   0     s .\n"
	gs, err := NewGoSpellReader(strings.NewReader(aff), strings.NewReader("1\nhello\n"))
	if err != nil {
		t.Fatalf("NewGoSpellReader: %v", err)
	}
	tmp := t.TempDir()
	dicPath := filepath.Join(tmp, "extra.dic")
	if err := os.WriteFile(dicPath, []byte("1\nwidget/S\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := AddDic(gs, dicPath, nil); err != nil {
		t.Fatalf("AddDic: %v", err)
	}
	if !gs.Spell("widget") {
		t.Error("Spell(widget) = false after AddDic")
	}
	if !gs.Spell("widgets") {
		t.Error("Spell(widgets) = false after AddDic (affix expansion)")
	}
}

func TestAddDicNotFound(t *testing.T) {
	if err := AddDic(makeTestGoSpell(t), "no_such_dict", []string{"/nonexistent"}); err == nil {
		t.Error("expected error for missing supplement, got nil")
	}
}

func TestOpenSupplementFromFile(t *testing.T) {
	tmp := t.TempDir()
	dicPath := filepath.Join(tmp, "tech.dic")
	if err := os.WriteFile(dicPath, []byte("2\nkubernetes\nkubectl\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	wl, err := OpenSupplement(dicPath, nil)
	if err != nil {
		t.Fatalf("OpenSupplement: %v", err)
	}
	if !wl.HasWord("kubernetes") {
		t.Error("HasWord(kubernetes) = false")
	}
}

func TestOpenSupplementNotFound(t *testing.T) {
	if _, err := OpenSupplement("no_such_supplement", []string{"/nonexistent"}); err == nil {
		t.Error("expected error for missing supplement, got nil")
	}
}

func TestSearchPathsDICPATH(t *testing.T) {
	t.Setenv("DICPATH", "/custom/dict"+string(filepath.ListSeparator)+"/other/dict")
	paths := SearchPaths()
	if paths[0] != "/custom/dict" || paths[1] != "/other/dict" {
		t.Errorf("SearchPaths with DICPATH: first two = %v, want [/custom/dict /other/dict]", paths[:2])
	}
}
