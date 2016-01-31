package gospell

import (
	"regexp"
	"strings"
	"unicode"
)

// number form, may include dots, commas and dashes
var numberRegexp = regexp.MustCompile("^([0-9]+[.,-]?)+$")

// number form with units, e.g. 123ms, 12in  1ft
var numberUnitsRegexp = regexp.MustCompile("^[0-9]+[a-zA-Z]+$")

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

func isNumber(s string) bool {
	return numberRegexp.MatchString(s)
}

// is word in the form of a "number with units", e.g. "101ms", "3ft",
// "5GB" if true, return the units, if not return empty string This is
// highly English based and not sure how applicable it is to other
// languages.
func isNumberUnits(s string) string {
	// regexp.FindAllStringSubmatch is too confusing
	if !numberUnitsRegexp.MatchString(s) {
		return ""
	}
	// ok starts with a number
	for idx, ch := range s {
		if ch >= '0' && ch <= '9' {
			continue
		}
		return s[idx:]
	}
	panic("assertion failed")
}
