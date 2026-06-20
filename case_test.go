package gospell

import (
	"reflect"
	"testing"
)

func TestCaseStyle(t *testing.T) {
	cases := []struct {
		word string
		want wordCase
	}{
		{"lower", allLower},
		{"what's", allLower},
		{"42nd", allLower},
		{"42ND", allUpper},
		{"UPPER", allUpper},
		{"Title", titleCase},
		{"CamelCase", mixedCase},
		{"camelCase", mixedCase},
	}

	for pos, tt := range cases {
		got := caseStyle(tt.word)
		if tt.want != got {
			t.Errorf("Case %d %q: want %v got %v", pos, tt.word, tt.want, got)
		}
	}
}

func TestCaseVariations(t *testing.T) {
	cases := []struct {
		word string
		want []string
	}{
		{"that's", []string{"that's", "That's", "THAT'S"}},
		// Multi-byte UTF-8 first rune: title-case must not slice on bytes.
		{"über", []string{"über", "Über", "ÜBER"}},
		{"ñoño", []string{"ñoño", "Ñoño", "ÑOÑO"}},
	}
	for pos, tt := range cases {
		got := caseVariations(tt.word, caseStyle(tt.word))
		if !reflect.DeepEqual(tt.want, got) {
			t.Errorf("Case %d %q: want %v got %v", pos, tt.word, tt.want, got)
		}
	}
}

func TestCaseVariationsAllUpper(t *testing.T) {
	got := caseVariations("UPPER", allUpper)
	if len(got) != 1 || got[0] != "UPPER" {
		t.Errorf("caseVariations(allUpper) = %v, want [UPPER]", got)
	}
}

func TestCaseVariationsTitleCase(t *testing.T) {
	// default branch: returns [word, strings.ToUpper(word)]
	got := caseVariations("Hello", titleCase)
	want := []string{"Hello", "HELLO"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("caseVariations(titleCase) = %v, want %v", got, want)
	}
}

func TestCaseVariationsMixedCase(t *testing.T) {
	// default branch: returns [word, strings.ToUpper(word)]
	got := caseVariations("camelCase", mixedCase)
	want := []string{"camelCase", "CAMELCASE"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("caseVariations(mixedCase) = %v, want %v", got, want)
	}
}
