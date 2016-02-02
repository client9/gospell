package gospell

import (
	"unicode"
)

// WordCase is an enum of various word casing styles
type WordCase int

// Various WordCase types.. likely to be not correct
const (
	AllLower WordCase = iota
	AllUpper
	Title
	Mixed
	Camel
)

// CaseStyle returns what case style a word is in
func CaseStyle(word string) WordCase {
	hasTitle := false
	upperCount := 0
	lowerCount := 0
	runeCount := 0

	// this iterates over RUNES not BYTES
	for _, r := range word {
		// ASCII apostrophe doesn't count
		//  want words like "don't" to have
		//  upper case forms when adding to dictionary
		if r == 0x0027 {
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

		//???
	}

	switch {
	case runeCount == lowerCount:
		return AllLower
	case runeCount == upperCount:
		return AllUpper
	case hasTitle && runeCount-1 == lowerCount:
		return Title
	default:
		return Mixed
	}
}
