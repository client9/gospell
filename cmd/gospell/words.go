package main

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

func splitWords(in string) []string {
	return strings.FieldsFunc(in, func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '\''
	})
}

func isNumber(s string) bool {
	return numberRegexp.MatchString(s)
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
