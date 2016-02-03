package gospell

import (
	"testing"
)

func TestCaseStyle(t *testing.T) {
	cases := []struct {
		word string
		want WordCase
	}{
		{"lower", AllLower},
		{"what's", AllLower},
		{"UPPER", AllUpper},
		{"Title", Title},
		{"CamelCase", Mixed},
		{"camelCase", Mixed},
	}

	for pos, tt := range cases {
		got := CaseStyle(tt.word)
		if tt.want != got {
			t.Errorf("Case %d %q: want %v got %v", pos, tt.word, tt.want, got)
		}
	}
}
