package gospell

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type wordCase int

const (
	allLower wordCase = iota
	allUpper
	titleCase
	mixedCase
)

func caseStyle(word string) wordCase {
	hasTitle := false
	upperCount := 0
	lowerCount := 0
	runeCount := 0

	for _, r := range word {
		// ASCII apostrophe doesn't count — want "don't" to have upper forms
		if r == 0x0027 || unicode.IsDigit(r) {
			continue
		}
		runeCount++
		if unicode.IsLower(r) {
			lowerCount++
			continue
		}
		if unicode.IsUpper(r) {
			if runeCount == 1 {
				hasTitle = true
			}
			upperCount++
			continue
		}
	}

	switch {
	case runeCount == lowerCount:
		return allLower
	case runeCount == upperCount:
		return allUpper
	case hasTitle && runeCount-1 == lowerCount:
		return titleCase
	default:
		return mixedCase
	}
}

func caseVariations(word string, style wordCase) []string {
	switch style {
	case allLower:
		_, size := utf8.DecodeRuneInString(word)
		return []string{word, strings.ToUpper(word[:size]) + word[size:], strings.ToUpper(word)}
	case allUpper:
		return []string{strings.ToUpper(word)}
	default:
		return []string{word, strings.ToUpper(word)}
	}
}
