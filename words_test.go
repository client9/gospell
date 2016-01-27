package gospell

import (
	"reflect"
	"testing"
)

func TestSplitter(t *testing.T) {

	s := NewSplitter("012345689")

	cases := []struct {
		word string
		want []string
	}{
		{"abc", []string{"abc"}},
		{"abc xyz", []string{"abc", "xyz"}},
		{"abc! xyz!", []string{"abc", "xyz"}},
		{"1st 2nd x86 amd64", []string{"1st", "2nd", "x86", "amd64"}},
	}

	for pos, tt := range cases {
		got := s.Split(tt.word)
		if !reflect.DeepEqual(tt.want, got) {
			t.Errorf("%d want %v  got %v", pos, tt.want, got)
		}
	}
}
