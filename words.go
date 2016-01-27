package gospell

import (
	"strings"
	"unicode"
)

// Splitter splits a text into words
// Highly likely this implementation will change so we are encapsulating.
type Splitter struct {
	fn func(c rune) bool
}

// Split is the function to split an input into a `[]string`
func (s *Splitter) Split(in string) []string {
	return strings.FieldsFunc(in, s.fn)
}

// NewSplitter creates a new splitter.  The input is a string in
// UTF-8 encoding.  Each rune in the stirng will be considered to be a
// valid word character.  Runes that are NOT here are deemed a word
// boundy Current implementation uses
// https://golang.org/pkg/strings/#FieldsFunc
func NewSplitter(chars string) *Splitter {
	s := Splitter{}
	s.fn = (func(c rune) bool {
		// break if it's not a letter, and not another special character
		return !unicode.IsLetter(c) && -1 == strings.IndexRune(chars, c)
	})
	return &s
}
