package gospell

import (
	"testing"
)

func TestCaseStye(t *testing.T) {
	cases := []struct {
		word string
		want WordCase
	}{
		{"lower", AllLower},
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
