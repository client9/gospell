package gospell

import (
	"regexp"
	"strings"
	"unicode"
)

// number form, may include dots, commas and dashes
var numberRegexp = regexp.MustCompile("^([0-9]+[.,-]?)+$")

// number form with units, e.g. 123ms, 12in, 1ft
var numberUnitsRegexp = regexp.MustCompile("^[0-9]+[a-zA-Z]+$")

// 0x12FF or 0x1B or x12FF
var numberHexRegexp = regexp.MustCompile("^0?[x][0-9A-Fa-f]+$")

var numberBinaryRegexp = regexp.MustCompile("^0[b][01]+$")

// SHA-1 / SHA-256 style hex hash (40 or 64 hex chars)
var shaHashRegexp = regexp.MustCompile("^[0-9a-fA-F]{40}$")

type splitter struct {
	fn func(c rune) bool
}

func (s *splitter) split(in string) []string {
	return strings.FieldsFunc(in, s.fn)
}

func newSplitter(chars string) *splitter {
	s := splitter{}
	s.fn = func(c rune) bool {
		return !unicode.IsLetter(c) && !strings.ContainsRune(chars, c)
	}
	return &s
}

func isNumber(s string) bool {
	return numberRegexp.MatchString(s)
}

func isNumberBinary(s string) bool {
	return numberBinaryRegexp.MatchString(s)
}

func isNumberUnits(s string) string {
	if !numberUnitsRegexp.MatchString(s) {
		return ""
	}
	for idx, ch := range s {
		if ch >= '0' && ch <= '9' {
			continue
		}
		return s[idx:]
	}
	panic("assertion failed")
}

func isNumberHex(s string) bool {
	return numberHexRegexp.MatchString(s)
}

func isHash(s string) bool {
	return shaHashRegexp.MatchString(s)
}
