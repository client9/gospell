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
	}
	for pos, tt := range cases {
		got := caseVariations(tt.word, caseStyle(tt.word))
		if !reflect.DeepEqual(tt.want, got) {
			t.Errorf("Case %d %q: want %v got %v", pos, tt.word, tt.want, got)
		}
	}
}
