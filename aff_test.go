package gospell

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// SmokeTest for AFF parser.  Contains a little bit of everything.
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
	aff, err := newDictConfig(strings.NewReader(sample))
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

	if len(aff.IconvReplacements) != 2 {
		t.Errorf("Didn't get ICONV replacement")
	} else {
		if aff.IconvReplacements[0] != "a" || aff.IconvReplacements[1] != "b" {
			t.Errorf("Replacement isnt a->b, got %v", aff.IconvReplacements)
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
	if a.Type != prefix {
		t.Fatalf("A Affix should be PFX %v, got %v", prefix, a.Type)
	}
	if !a.CrossProduct {
		t.Fatalf("A Affix should be a cross product")
	}

	variations := a.expand("define", "", 0, compoundRules{}, flagASCII, false, nil)
	if len(variations) != 1 {
		t.Fatalf("Expected 1 variation got %d", len(variations))
	}
	if variations[0].word != "redefine" {
		t.Errorf("Expected %s got %s", "redefine", variations[0].word)
	}

	a, ok = aff.AffixMap["D"]
	if !ok {
		t.Fatalf("Didn't get Affix for D")
	}
	if a.Type != suffix {
		t.Fatalf("Affix D is not a SFX %v", suffix)
	}
	if len(a.Rules) != 4 {
		t.Fatalf("Affix should have 4 rules, got %d", len(a.Rules))
	}
	variations = a.expand("accept", "", 0, compoundRules{}, flagASCII, false, nil)
	if len(variations) != 1 {
		t.Fatalf("D Affix should have %d rules, got %d", 1, len(variations))
	}
	if variations[0].word != "accepted" {
		t.Errorf("Expected %s got %s", "accepted", variations[0].word)
	}
}

func TestSurfaceRecordsLoaded(t *testing.T) {
	sampleAff := `
SET UTF-8
ONLYINCOMPOUND O
`
	sampleDic := `2
foo/O
bar
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	if len(gs.surfaces["foo"]) == 0 {
		t.Fatalf("foo surface record missing")
	}
	if len(gs.surfaces["bar"]) == 0 {
		t.Fatalf("bar surface record missing")
	}
	if len(gs.surfaces["foo"][0].RawFlags) == 0 {
		t.Fatalf("foo raw flags not recorded")
	}
}

func TestSpellExactUsesSurfaceRecords(t *testing.T) {
	gs := &GoSpell{
		dict: map[string]struct{}{
			"foo": {},
		},
		surfaces: map[string][]surfaceEntry{
			"foo": {{
				Word:              "foo",
				StandaloneAllowed: false,
			}},
		},
	}
	if got := gs.spellExact("foo"); got {
		t.Fatalf("expected surface-restricted foo to be rejected")
	}
	gs.surfaces["foo"][0].StandaloneAllowed = true
	if got := gs.spellExact("foo"); !got {
		t.Fatalf("expected surface-allowed foo to be accepted")
	}
}

func TestCompoundUsesSurfaceRecords(t *testing.T) {
	gs := &GoSpell{
		compoundMin: 1,
		surfaces: map[string][]surfaceEntry{
			"start": {{
				Word:                  "start",
				CompoundStartAllowed:  false,
				CompoundMiddleAllowed: false,
				CompoundEndAllowed:    false,
			}},
			"mid": {{
				Word:                  "mid",
				CompoundStartAllowed:  false,
				CompoundMiddleAllowed: false,
				CompoundEndAllowed:    false,
			}},
			"end": {{
				Word:                  "end",
				CompoundStartAllowed:  false,
				CompoundMiddleAllowed: false,
				CompoundEndAllowed:    false,
			}},
		},
		compoundBegin:  map[string]struct{}{"start": {}},
		compoundMiddle: map[string]struct{}{"mid": {}},
		compoundEnd:    map[string]struct{}{"end": {}},
	}
	if gs.compoundStartPart("start") {
		t.Fatalf("surface metadata should block start even when legacy map allows it")
	}
	if gs.compoundMiddlePart("mid") {
		t.Fatalf("surface metadata should block middle even when legacy map allows it")
	}
	if gs.compoundFinalPart("end", allLower) {
		t.Fatalf("surface metadata should block end even when legacy map allows it")
	}
	gs.surfaces["start"][0].CompoundStartAllowed = true
	gs.surfaces["mid"][0].CompoundMiddleAllowed = true
	gs.surfaces["end"][0].CompoundEndAllowed = true
	if !gs.compoundStartPart("start") {
		t.Fatalf("expected start to be allowed once surface metadata permits it")
	}
	if !gs.compoundMiddlePart("mid") {
		t.Fatalf("expected middle to be allowed once surface metadata permits it")
	}
	if !gs.compoundFinalPart("end", allLower) {
		t.Fatalf("expected end to be allowed once surface metadata permits it")
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
	aff, err := newDictConfig(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("Unable to parse sample: %s", err)
	}

	cases := []struct {
		word string
		want []string
	}{
		{"hello", []string{"hello"}},
		{"try/B", []string{"try", "tried"}},
		{"work/AB", []string{"work", "worked", "reworked", "rework"}},
	}
	for pos, tt := range cases {
		got, err := aff.expand(tt.word, nil)
		if err != nil {
			t.Errorf("%d: affix expansions error: %s", pos, err)
		}
		if !reflect.DeepEqual(tt.want, got) {
			t.Errorf("%d: affix expansion want %v got %v", pos, tt.want, got)
		}
	}
}

func TestCompound(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDMIN 1
ONLYINCOMPOUND c
COMPOUNDRULE 2
COMPOUNDRULE n*1t
COMPOUNDRULE n*mp
WORDCHARS 0123456789
`
	sampleDic := `23
0/nm
0th/pt
1/n1
1st/p
1th/tc
2/nm
2nd/p
2th/tc
3/nm
3rd/p
3th/tc
4/nm
4th/pt
5/nm
5th/pt
6/nm
6th/pt
7/nm
7th/pt
8/nm
8th/pt
9/nm
9th/pt
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
		{"0", true},
		{"1", true},
		{"2", true},
		{"3", true},
		{"4", true},
		{"5", true},
		{"6", true},
		{"7", true},
		{"8", true},
		{"9", true},
		{"1st", true},
		{"21st", true},
		{"11th", true},
		{"1th", false},
		{"10", true},
		{"21", true},
		{"32", true},
		{"43", true},
		{"54", true},
		{"65", true},
		{"76", true},
		{"87", true},
		{"98", true},
		{"99", true},
		{"12th", true},
		{"2th", false},
		{"13th", true},
		{"3th", false},
		{"3rd", true},
		{"33rd", true},
		{"4th", true},
		{"5th", true},
		{"6th", true},
		{"7th", true},
		{"8th", true},
		{"9th", true},
		{"14th", true},
		{"15th", true},
		{"16th", true},
		{"17th", true},
		{"18th", true},
		{"19th", true},
		{"111", true},
		{"111st", false},
		{"111th", true},
	}
	for pos, tt := range cases {
		if gs.Spell(tt.word) != tt.spell {
			t.Errorf("%d %q was not %v", pos, tt.word, tt.spell)
		}
	}
}

func TestCompoundCaseFallback(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDMIN 1
ONLYINCOMPOUND c
COMPOUNDRULE 2
COMPOUNDRULE n*1t
COMPOUNDRULE n*mp
WORDCHARS 0123456789
`
	sampleDic := `23
0/nm
0th/pt
1/n1
1st/p
1th/tc
2/nm
2nd/p
2th/tc
3/nm
3rd/p
3th/tc
4/nm
4th/pt
5/nm
5th/pt
6/nm
6th/pt
7/nm
7th/pt
8/nm
8th/pt
9/nm
9th/pt
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}

	cases := []struct {
		word string
		want bool
	}{
		{"42nd", true},
		{"42ND", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundFlagFixtures(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDMIN 3
COMPOUNDFLAG A
`
	sampleDic := `4
foo/A
bar/A
xy/A
yz/A
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foobar", true},
		{"barfoo", true},
		{"foobarfoo", true},
		{"xyyz", false},
		{"fooxy", false},
		{"xyfoo", false},
		{"fooxybar", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundAffixFlags(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDFLAG X
COMPOUNDPERMITFLAG Y

PFX P Y 1
PFX P   0     pre/Y         .

SFX S Y 1
SFX S   0     suf/Y         .
`
	sampleDic := `2
foo/XPS
bar/XPS
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"prefoo", true},
		{"foosuf", true},
		{"prefoosuf", true},
		{"prefoobarsuf", true},
		{"foosufbar", true},
		{"fooprebarsuf", true},
		{"prefooprebarsuf", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundAffixRegression(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDFLAG X

PFX P Y 1
PFX P   0     pre         .

SFX S Y 1
SFX S   0     suf         .
`
	sampleDic := `2
foo/XPS
bar/XPS
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foosufbar", false},
		{"prefoobarsuf", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundAffixExpansionBounded(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDFLAG X

PFX P Y 1
PFX P   0     pre         .

SFX S Y 1
SFX S   0     suf         .
`
	aff, err := newDictConfig(strings.NewReader(sampleAff))
	if err != nil {
		t.Fatalf("Unable to create dict config: %v", err)
	}

	got, err := aff.expand("foo/XPS", nil)
	if err != nil {
		t.Fatalf("expand failed: %v", err)
	}
	want := []string{"foo", "foosuf", "prefoo", "prefoosuf"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expand mismatch: got %v want %v", got, want)
	}
}

func TestCompoundAffixDeepChainRegression(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDMIN 1

SFX A Y 1
SFX A 0 s/123 .

SFX 1 Y 1
SFX 1 0 bar .

SFX 2 Y 1
SFX 2 0 baz .

PFX 3 Y 1
PFX 3 0 un .
`
	aff, err := newDictConfig(strings.NewReader(sampleAff))
	if err != nil {
		t.Fatalf("Unable to create dict config: %v", err)
	}

	got, err := aff.expand("foo/A3", nil)
	if err != nil {
		t.Fatalf("expand failed: %v", err)
	}
	want := []string{"foo", "foos", "foosbar", "foosbaz", "unfoo", "unfoos", "unfoosbar", "unfoosbaz"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expand mismatch: got %v want %v", got, want)
	}
}

func TestCompoundForbidFlags(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDFLAG X
COMPOUNDPERMITFLAG Y
COMPOUNDFORBIDFLAG Z

PFX P Y 1
PFX P   0     pre/Z         .

SFX S Y 1
SFX S   0     suf/Z         .
`
	sampleDic := `2
foo/XPS
bar/XPS
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"foofoo", true},
		{"prefoo", true},
		{"foosuf", true},
		{"prefoosuf", true},
		{"prefoobarsuf", false},
		{"foosufbar", false},
		{"fooprebar", false},
		{"foosufprebar", false},
		{"fooprebarsuf", false},
		{"prefooprebarsuf", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundForbidRegression(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDFLAG X
COMPOUNDPERMITFLAG Y
COMPOUNDFORBIDFLAG Z

SFX S Y 2
SFX S   0     bar/YX         .
SFX S   0     baz/YX         .
`
	sampleDic := `3
foo/S
example/X
foobaz/Z
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foobaz", true},
		{"foobazexample", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestForceUcaseCompound(t *testing.T) {
	sampleAff := `
SET UTF-8
TRY F
FORCEUCASE A
COMPOUNDFLAG C
`
	sampleDic := `3
foo/C
bar/C
baz/CA
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	if !gs.compoundFinalPart("foo", allLower) {
		t.Fatalf("foo should be accepted as a compound final part")
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"bar", true},
		{"baz", true},
		{"foobar", true},
		{"Foobaz", true},
		{"foobaz", false},
		{"foobazbar", true},
		{"Foobarbaz", true},
		{"foobarbaz", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestLimitMultipleCompoundingRegression(t *testing.T) {
	sampleAff := `
SET UTF-8
TRY esianrtolcdugmphbyfvkwz'
COMPOUNDFLAG x
`
	sampleDic := `6
foo/x
bar/x
baz/x
goobar
goobarbaz
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foobar", true},
		{"foobarbaz", false},
		{"goobar", true},
		{"goobarbaz", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundPatternRegression2970240(t *testing.T) {
	sampleAff := `
SET UTF-8
CHECKCOMPOUNDPATTERN 1
CHECKCOMPOUNDPATTERN le fi
COMPOUNDFLAG c
`
	sampleDic := `3
first/c
middle/c
last/c
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"firstmiddlelast", true},
		{"lastmiddlefirst", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundPatternDefaultLatin1Regression(t *testing.T) {
	sampleAff := []byte(`
# forbid compounds with spec. pattern at word bounds
COMPOUNDFLAG A
CHECKCOMPOUNDPATTERN 2
CHECKCOMPOUNDPATTERN nny ny
CHECKCOMPOUNDPATTERN ssz sz
`)
	sampleDic := []byte{
		'4', '\n',
		'k', 0xf6, 'n', 'n', 'y', '/', 'A', '\n',
		'n', 'y', 'e', 'l', 0xe9, 's', '/', 'A', '\n',
		'h', 'o', 's', 's', 'z', '/', 'A', '\n',
		's', 'z', 0xe1, 'm', 0xed, 't', 0xe1, 's', '/', 'A', '\n',
	}
	gs, err := NewGoSpellReader(bytes.NewReader(sampleAff), bytes.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"könnyszámítás", true},
		{"hossznyelés", true},
		{"könnynyelés", false},
		{"hosszszámítás", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundPatternRegression2970242(t *testing.T) {
	sampleAff := `
SET UTF-8
CHECKCOMPOUNDPATTERN 1
CHECKCOMPOUNDPATTERN /a /b
COMPOUNDFLAG c
`
	sampleDic := `3
foo/ac
bar/c
baz/bc
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foobar", true},
		{"foobaz", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestCompoundBeginEndRegression2999225(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDRULE 1
COMPOUNDRULE ab
COMPOUNDBEGIN A
COMPOUNDEND B
`
	sampleDic := `3
foo/aA
bar/b
baz/B
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foobar", true},
		{"foobaz", true},
		{"barfoo", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestOnlyInCompoundRegression(t *testing.T) {
	sampleAff := `
SET UTF-8
ONLYINCOMPOUND O
COMPOUNDFLAG A
SFX B Y 1
SFX B 0 s .
`
	sampleDic := `2
foo/A
pseudo/OAB
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	if _, ok := gs.compoundOnlyRoot["pseudo"]; !ok {
		t.Fatalf("pseudo not recorded as a compound-only root surface")
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"pseudo", false},
		{"pseudofoo", true},
		{"foopseudo", true},
		{"foopseudos", true},
		{"pseudos", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestOnlyInCompoundRegression2(t *testing.T) {
	sampleAff := `
SET UTF-8
ONLYINCOMPOUND O
COMPOUNDFLAG A
COMPOUNDPERMITFLAG P

SFX B Y 1
SFX B 0 s/OP .

CHECKCOMPOUNDPATTERN 1
CHECKCOMPOUNDPATTERN 0/B /A
`
	sampleDic := `2
foo/A
pseudo/AB
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"foopseudo", true},
		{"pseudosfoo", true},
		{"pseudos", false},
		{"foopseudos", false},
		{"pseudofoo", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestOnlyInCompoundHomonymRegression1592880(t *testing.T) {
	sampleAff := `
SET UTF-8

SFX N Y 1
SFX N 0 n .

SFX S Y 1
SFX S 0 s .

SFX P Y 1
SFX P 0 en .

SFX Q Y 2
SFX Q 0 e .
SFX Q 0 en .

COMPOUNDEND z
COMPOUNDPERMITFLAG c
ONLYINCOMPOUND o
`
	sampleDic := `3
weg/Qoz
weg/P
wege
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %v", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"weg", true},
		{"wege", true},
		{"wegen", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestDigitsInWordsCompoundRule(t *testing.T) {
	sampleAff := `
SET UTF-8
COMPOUNDMIN 1
COMPOUNDRULE 1
COMPOUNDRULE a*b
ONLYINCOMPOUND c
WORDCHARS 0123456789-
`
	sampleDic := `11
0/a
1/a
2/a
3/a
4/a
5/a
6/a
7/a
8/a
9/a
-jährig/bc
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}

	cases := []struct {
		word string
		want bool
	}{
		{"1-jährig", true},
		{"-jährig", false},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestSpell(t *testing.T) {
	sampleAff := `
SET UTF-8
WORDCHARS 0123456789

PFX A Y 1
PFX A 0 re .

SFX B Y 2
SFX B 0 ed [^y]
SFX B y ied y
`

	sampleDic := `4
hello
try/B
work/AB
GB
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
		{"100", true},
		{"1", true},
		{"1.1", true},
		{"4,2", true},
		{"42-42", true},
		{"100GB", false},
		{"100mi", false},
		{"0xFF", false},
		{"0x12ff", false},
	}
	for pos, tt := range cases {
		if gs.Spell(tt.word) != tt.spell {
			t.Errorf("%d %q was not %v", pos, tt.word, tt.spell)
		}
	}
}

// TestSpellCaseFallback verifies that Spell's case-folding matches hunspell's
// behaviour: case variants are resolved at lookup time rather than pre-stored.
func TestSpellCaseFallback(t *testing.T) {
	// "hello" is allLower; "London" is titleCase; "NASA" is allUpper.
	// "McDonald" is mixedCase — its all-caps form must also be accepted.
	sampleAff := `
SET UTF-8
`
	sampleDic := `4
hello
London
NASA
McDonald
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}

	cases := []struct {
		word string
		want bool
		note string
	}{
		// allLower "hello": accepted in all three standard case forms
		{"hello", true, "allLower exact"},
		{"Hello", true, "allLower → titleCase fallback"},
		{"HELLO", true, "allLower → allUpper fallback"},
		{"hElLo", false, "allLower → mixedCase rejected"},

		// titleCase "London": accepted as-is and in allUpper; lowercase rejected
		{"London", true, "titleCase exact"},
		{"LONDON", true, "titleCase → allUpper fallback"},
		{"london", false, "titleCase → allLower rejected (hunspell behaviour)"},

		// allUpper "NASA": only the exact form accepted
		{"NASA", true, "allUpper exact"},
		{"nasa", false, "allUpper → allLower rejected"},
		{"Nasa", false, "allUpper → titleCase rejected"},

		// mixedCase "McDonald": exact and allUpper accepted
		{"McDonald", true, "mixedCase exact"},
		{"MCDONALD", true, "mixedCase → allUpper accepted (stored explicitly)"},
		{"mcdonald", false, "mixedCase → allLower rejected"},
	}
	for _, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%q (%s): got %v, want %v", tt.word, tt.note, got, tt.want)
		}
	}
}

func TestIconvSpell(t *testing.T) {
	sampleAff := `
SET UTF-8

ICONV 4
ICONV ş ș
ICONV ţ ț
ICONV Ş Ș
ICONV Ţ Ț
`
	sampleDic := `4
Chișinău
Țepes
ț
Ș
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"Chișinău", true},
		{"Chişinău", true},
		{"Țepes", true},
		{"Ţepes", true},
		{"Ş", true},
		{"ţ", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

func TestIconvLongestMatch(t *testing.T) {
	sampleAff := `
SET UTF-8

ICONV 6
ICONV Da DA
ICONV Ga GA
ICONV Gag GAG
ICONV Gagg GAGG
ICONV Na NA
ICONV Nan NAN
`
	sampleDic := `4
GAG
GAGGNA
GANA
NANDA
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("Unable to create GoSpell: %s", err)
	}
	cases := []struct {
		word string
		want bool
	}{
		{"GaNa", true},
		{"Gag", true},
		{"GaggNa", true},
		{"NanDa", true},
	}
	for pos, tt := range cases {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%d %q got %v want %v", pos, tt.word, got, tt.want)
		}
	}
}

// TestFlagUTF8ChainedSuffix: FLAG UTF-8 without SET should treat the file as
// UTF-8. Multi-byte flag chars must survive and allow chained suffix expansion.
func TestFlagUTF8ChainedSuffix(t *testing.T) {
	sampleAff := "FLAG UTF-8\n\nSFX A Y 1\nSFX A 0 s/\xc3\x96\xc3\xbc\xc3\x9c .\n\nSFX \xc3\x96 Y 1\nSFX \xc3\x96 0 bar .\n\nSFX \xc3\xbc Y 1\nSFX \xc3\xbc 0 baz .\n\nPFX \xc3\x9c Y 1\nPFX \xc3\x9c 0 un .\n"
	sampleDic := "1\nfoo/A\xc3\x9c\n"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"foo", true}, {"foos", true}, {"foosbar", true}, {"foosbaz", true},
		{"unfoo", true}, {"unfoos", true}, {"unfoosbar", true}, {"unfoosbaz", true},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// TestCheckCompoundPatternSandhi: 3-argument CHECKCOMPOUNDPATTERN.
// Sandhi rule "o b z": foo+bar → fozar; raw "foobar" blocked.
func TestCheckCompoundPatternSandhi(t *testing.T) {
	sampleAff := "SET UTF-8\nCOMPOUNDFLAG A\nCHECKCOMPOUNDPATTERN 2\nCHECKCOMPOUNDPATTERN o b z\nCHECKCOMPOUNDPATTERN oo ba u\nCOMPOUNDMIN 1\n"
	sampleDic := "2\nfoo/A\nbar/A\n"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"barfoo", true}, {"fozar", true}, {"foobar", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// TestCheckCompoundPatternSandhiWithFlags: sandhi with per-word flag guards.
func TestCheckCompoundPatternSandhiWithFlags(t *testing.T) {
	sampleAff := "SET UTF-8\nCOMPOUNDFLAG A\nCHECKCOMPOUNDPATTERN 1\nCHECKCOMPOUNDPATTERN o/X b/Y z\nCOMPOUNDMIN 1\n"
	sampleDic := "4\nfoo/A\nboo/AX\nbar/A\nban/AY\n"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"bozan", true}, {"barfoo", true}, {"foobar", true},
		{"boobar", true}, {"booban", false}, {"foobanbar", true},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// TestCheckCompoundPatternSandhiTelugu: sandhi with Unicode replacement.
func TestCheckCompoundPatternSandhiTelugu(t *testing.T) {
	sampleAff := "SET UTF-8\nCOMPOUNDFLAG x\nCOMPOUNDMIN 1\nCHECKCOMPOUNDPATTERN 2\nCHECKCOMPOUNDPATTERN a/A u/A O\nCHECKCOMPOUNDPATTERN u/B u/B u\n"
	sampleDic := "4\nsUrya/Ax\nudayaM/Ax\npEru/Bx\nunna/Bx\n"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"sUryOdayaM", true}, {"pErunna", true},
		{"sUryaudayaM", false}, {"pEruunna", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("%q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier 1: CHECKCOMPOUNDCASE
// ---------------------------------------------------------------------------

func TestCheckCompoundCase(t *testing.T) {
	sampleAff := `CHECKCOMPOUNDCASE
COMPOUNDFLAG A
`
	sampleDic := `4
foo/A
Bar/A
BAZ/A
-/A
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		// GOOD: no uppercase at letter-to-letter boundaries
		{"Barfoo", true},
		{"foo-Bar", true},
		{"foo-BAZ", true},
		{"BAZ-foo", true},
		{"BAZ-Bar", true},
		// WRONG: uppercase letter at a letter-to-letter boundary
		{"fooBar", false},
		{"BAZBar", false},
		{"BAZfoo", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("CHECKCOMPOUNDCASE %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestCheckCompoundCaseUTF(t *testing.T) {
	sampleAff := `SET UTF-8
CHECKCOMPOUNDCASE
COMPOUNDFLAG A
`
	sampleDic := `2
áoó/A
Óoá/A
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"áoóáoó", true},
		{"Óoááoó", true},
		{"áoóÓoá", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("CHECKCOMPOUNDCASE UTF %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier 1: CHECKCOMPOUNDDUP
// ---------------------------------------------------------------------------

func TestCheckCompoundDup(t *testing.T) {
	sampleAff := `CHECKCOMPOUNDDUP
COMPOUNDFLAG A
`
	sampleDic := `2
foo/A
bar/A
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		// GOOD: no adjacent identical parts
		{"barfoo", true},
		{"foobar", true},
		{"foofoobar", true},
		{"foobarfoo", true},
		{"barfoobarfoo", true},
		// WRONG: adjacent identical parts
		{"foofoo", false},
		{"foofoofoo", false},
		{"foobarbar", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("CHECKCOMPOUNDDUP %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier 1: CHECKCOMPOUNDTRIPLE + SIMPLIFIEDTRIPLE
// ---------------------------------------------------------------------------

func TestCheckCompoundTriple(t *testing.T) {
	sampleAff := `CHECKCOMPOUNDTRIPLE
COMPOUNDFLAG A
`
	sampleDic := `4
foo/A
opera/A
eel/A
bare/A
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		// GOOD: no triple letter at boundary
		{"operafoo", true},
		{"operaeel", true},
		{"operabare", true},
		{"eelbare", true},
		{"eelfoo", true},
		{"eelopera", true},
		// WRONG: triple letters at compound boundary
		{"fooopera", false},
		{"bareeel", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("CHECKCOMPOUNDTRIPLE %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestSimplifiedTriple(t *testing.T) {
	sampleAff := `CHECKCOMPOUNDTRIPLE
SIMPLIFIEDTRIPLE
COMPOUNDMIN 2
COMPOUNDFLAG A
`
	sampleDic := `2
glass/A
sko/A
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"glass", true},
		{"sko", true},
		// simplified form (glasssko → glassko by dropping one 's')
		{"glassko", true},
		// raw triple form is still wrong
		{"glasssko", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("SIMPLIFIEDTRIPLE %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier 2: CHECKCOMPOUNDREP
// ---------------------------------------------------------------------------

func TestCheckCompoundRep(t *testing.T) {
	// UTF-8 regression case: fa+ajtó should not be rejected because
	// the ó→o replacement applied at position 2 transforms "fa" into "fo"
	// which was incorrectly combined with "ajtó" to produce "fojtó".
	sampleAff := `SET UTF-8
COMPOUNDMIN 2
COMPOUNDFLAG x
CHECKCOMPOUNDREP

REP 1
REP ó o
`
	sampleDic := `3
fa/x
ajtó/x
fojtó
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"fa", true},
		{"ajtó", true},
		{"ajtófa", true},
		{"faajtó", true},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("CHECKCOMPOUNDREP %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Tier 3: COMPOUNDMIDDLE + CHECKCOMPOUNDPATTERN flag checks for derived forms
// ---------------------------------------------------------------------------

func TestCompoundMiddleFlag(t *testing.T) {
	// A simple 3-part compound: only the middle part is allowed in the middle.
	sampleAff := `SET UTF-8
COMPOUNDBEGIN B
COMPOUNDMIDDLE M
COMPOUNDEND E
`
	sampleDic := `3
start/B
mid/M
end/E
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"startmidend", true},
		{"startend", true},       // 2-part compound: start+end is valid without a middle
		{"midstartend", false},   // wrong order: mid cannot be start
		{"startstartend", false}, // start has no M flag, cannot be in middle position
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("COMPOUNDMIDDLE %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestCheckCompoundPatternDerivedFlags(t *testing.T) {
	// CHECKCOMPOUNDPATTERN /Ch /Xs should block schoonheids+port
	// even though schoonheids is derived (SFX Ch) and does not carry Ch
	// as one of its own output flags.
	sampleAff := `FLAG long
COMPOUNDBEGIN Ca
COMPOUNDMIDDLE Cb
COMPOUNDEND Cc
COMPOUNDPERMITFLAG Cp
ONLYINCOMPOUND Cx

CHECKCOMPOUNDPATTERN 1
CHECKCOMPOUNDPATTERN /Ch /Xs

SFX Ch Y 2
SFX Ch 0 s/CaCbCxCp .
SFX Ch 0 s-/CaCbCcCp .
`
	sampleDic := `3
schoonheid/Ch
port/CcXs
sport/Cc
`
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"schoonheidssport", true},
		{"schoonheidsport", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("opentaal_cpdpat %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// NEEDAFFIX
// ---------------------------------------------------------------------------

func TestNeedAffixBasic(t *testing.T) {
	// needaffix.* fixture: foo/YXA means "foo" needs an affix (X flag).
	// "foos" (via SFX A output, no X) is valid. Compound "barfoos" is valid.
	sampleAff := `NEEDAFFIX X
COMPOUNDFLAG Y

SFX A Y 1
SFX A 0 s/Y .
`
	sampleDic := "2\nfoo/YXA\nbar/Y"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"bar", true},
		{"foos", true},
		{"barfoos", true},
		{"foo", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("NeedAffixBasic %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestNeedAffixHomonym(t *testing.T) {
	// needaffix2.* fixture: "foo" appears three times; "foo/Y" (no X) makes
	// "foo" a valid standalone word despite another entry carrying X.
	sampleAff := `NEEDAFFIX X
COMPOUNDFLAG Y`
	sampleDic := "4\nfoo\nfoo/YX\nfoo/Y\nbar/Y"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"bar", true},
		{"foobar", true},
		{"barfoo", true},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("NeedAffixHomonym %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestNeedAffixOnAffix(t *testing.T) {
	// needaffix3.* fixture: X on an affix output makes the derived form
	// a virtual stem. "foos" (X on SFX A output) is wrong; "foosbaz"
	// (SFX B applied on top) clears the X and is valid.
	sampleAff := `NEEDAFFIX X

SFX A Y 1
SFX A 0 s/XB .

SFX B Y 1
SFX B 0 baz .
`
	sampleDic := "1\nfoo/A"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"foosbaz", true},
		{"foos", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("NeedAffixOnAffix %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestNeedAffixHomonym4(t *testing.T) {
	// needaffix4.* fixture: three foo entries with different flag combos;
	// "foo/Y" (no X) provides a valid standalone form.
	sampleAff := `NEEDAFFIX X
COMPOUNDFLAG Y`
	sampleDic := "4\nfoo/X\nfoo/Y\nfoo/YX\nbar/Y"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"bar", true},
		{"foobar", true},
		{"barfoo", true},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("NeedAffixHomonym4 %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}

func TestNeedAffixPrefixSuffix(t *testing.T) {
	// needaffix5.* fixture: X on prefix output requires a suffix; X on
	// suffix output requires a prefix (without X). Both X clears only when
	// a further affix (without X) resolves it.
	sampleAff := `NEEDAFFIX X

SFX A Y 2
SFX A 0 suf/B .
SFX A 0 pseudosuf/XB .

SFX B Y 1
SFX B 0 bar .

PFX C Y 2
PFX C 0 pre .
PFX C 0 pseudopre/X .
`
	sampleDic := "1\nfoo/AC"
	gs, err := NewGoSpellReader(strings.NewReader(sampleAff), strings.NewReader(sampleDic))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tt := range []struct {
		word string
		want bool
	}{
		{"foo", true},
		{"prefoo", true},
		{"foosuf", true},
		{"prefoosuf", true},
		{"foosufbar", true},
		{"prefoosufbar", true},
		{"pseudoprefoosuf", true},
		{"pseudoprefoosufbar", true},
		{"pseudoprefoopseudosufbar", true},
		{"prefoopseudosuf", true},
		{"prefoopseudosufbar", true},
		{"pseudoprefoo", false},
		{"foopseudosuf", false},
		{"pseudoprefoopseudosuf", false},
	} {
		if got := gs.Spell(tt.word); got != tt.want {
			t.Errorf("NeedAffixPrefixSuffix %q: got %v want %v", tt.word, got, tt.want)
		}
	}
}
