package gospell

import (
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
	a, ok := aff.AffixMap["A"]
	if !ok {
		t.Fatalf("Didn't get Affix for A")
	}
	if a.Type != "PFX" {
		t.Fatalf("A Affix should be PFX, got %s", a.Type)
	}
	if !a.CrossProduct {
		t.Fatalf("A Affix should be a cross product")
	}

	a, ok = aff.AffixMap["D"]
	if !ok {
		t.Fatalf("Didn't get Affix for D")
	}
	if a.Type != "SFX" {
		t.Fatalf("Affix D is not a SFX")
	}
	if len(a.Rules) != 4 {
		t.Fatalf("Affix should have 4 rules, got %d", len(a.Rules))
	}
}
