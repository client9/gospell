package gospell

import (
	"reflect"
	"strings"
	"testing"
)

// SmokeTest for AFF parser.  Contains a little bit of everything.
//
func TestAFFSmoke(t *testing.T) {
	sample := `
#

TRY abc
WORDCHARS 123
ICONV 1
ICONV a b
PFX A Y 1
PFX A   0     re .
SFX D Y 4
SFX D   0     d          e
SFX D   y     ied        [^aeiou]y
SFX D   0     ed         [^ey]
SFX D   0     ed         [aeiou]y
REP 1
REP a ei
COMPOUNDMIN 2
`
	aff, err := NewAFF(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Unable to parse sample: %s", err)
	}

	if aff.TryChars != "abc" {
		t.Errorf("TRY stanza is %s", aff.TryChars)
	}

	if aff.WordChars != "123" {
		t.Errorf("WORDCHARS stanza is %s", aff.WordChars)
	}

	if aff.CompoundMin != 2 {
		t.Errorf("COMPOUNDMIN stanza not processed, want 2 got %d", aff.CompoundMin)
	}

	if len(aff.IconvReplacements) != 1 {
		t.Errorf("Didn't get ICONV replacement")
	} else {
		pair := aff.IconvReplacements[0]
		if pair[0] != "a" || pair[1] != "b" {
			t.Errorf("Replacement isnt a->b, got %v", pair)
		}
	}

	if len(aff.Replacements) != 1 {
		t.Errorf("Didn't get REPlacement")
	} else {
		pair := aff.Replacements[0]
		if pair[0] != "a" || pair[1] != "ei" {
			t.Errorf("Replacement isnt [a ie] got %v", pair)
		}
	}

	if len(aff.AffixMap) != 2 {
		t.Errorf("AffixMap is wrong size")
	}
	a, ok := aff.AffixMap[rune('A')]
	if !ok {
		t.Fatalf("Didn't get Affix for A")
	}
	if a.Type != Prefix {
		t.Fatalf("A Affix should be PFX %v, got %v", Prefix, a.Type)
	}
	if !a.CrossProduct {
		t.Fatalf("A Affix should be a cross product")
	}

	variations := a.Expand("define", nil)
	if len(variations) != 1 {
		t.Fatalf("Expected 1 variation got %d", len(variations))
	}
	if variations[0] != "redefine" {
		t.Errorf("Expected %s got %s", "redefine", variations[0])
	}

	a, ok = aff.AffixMap[rune('D')]
	if !ok {
		t.Fatalf("Didn't get Affix for D")
	}
	if a.Type != Suffix {
		t.Fatalf("Affix D is not a SFX %v", Suffix)
	}
	if len(a.Rules) != 4 {
		t.Fatalf("Affix should have 4 rules, got %d", len(a.Rules))
	}
	variations = a.Expand("accept", nil)
	if len(variations) != 1 {
		t.Fatalf("D Affix should have %d rules, got %d", 1, len(variations))
	}
	if variations[0] != "accepted" {
		t.Errorf("Expected %s got %s", "accepted", variations[0])
	}
}

func TestExpand(t *testing.T) {
	sample := `
SET UTF-8
TRY esianrtolcdugmphbyfvkwzESIANRTOLCDUGMPHBYFVKWZ'

REP 2
REP f ph
REP ph f

PFX A Y 1
PFX A 0 re .

SFX B Y 2
SFX B 0 ed [^y]
SFX B y ied y
`
	aff, err := NewAFF(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Unable to parse sample: %s", err)
	}

	cases := []struct {
		word string
		want []string
	}{
		{"hello", []string{"hello"}},
		{"try/B", []string{"try", "tried"}},
		{"work/AB", []string{"work", "worked", "rework", "reworked"}},
	}
	for pos, tt := range cases {
		got, err := aff.Expand(tt.word, nil)
		if err != nil {
			t.Errorf("%d: affix expansions error: %s", pos, err)
		}
		if !reflect.DeepEqual(tt.want, got) {
			t.Errorf("%d: affix expansion want %v got %v", pos, tt.want, got)
		}
	}
}

func TestSpell(t *testing.T) {
	sampleAff := `
SET UTF-8
TRY esianrtolcdugmphbyfvkwzESIANRTOLCDUGMPHBYFVKWZ'

REP 2
REP f ph
REP ph f

PFX A Y 1
PFX A 0 re .

SFX B Y 2
SFX B 0 ed [^y]
SFX B y ied y
`

	sampleDic := `3
hello
try/B
work/AB
`

	aff := strings.NewReader(sampleAff)
	dic := strings.NewReader(sampleDic)
	gs, err := NewGoSpellReader(aff, dic)
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}

	cases := []struct {
		word  string
		spell bool
	}{
		{"hello", true},
		{"try", true},
		{"tried", true},
		{"work", true},
		{"worked", true},
		{"rework", true},
		{"reworked", true},
		{"junk", false},
	}
	for pos, tt := range cases {
		if gs.Spell(tt.word) != tt.spell {
			t.Errorf("%d %q was not %v", pos, tt.word, tt.spell)
		}
	}
}
