package gospell

import (
	"fmt"
	"slices"
	"unicode/utf8"
)

// affixMatcher holds a pre-parsed hunspell condition pattern and knows
// whether to test it against the start (prefix) or end (suffix) of a word.
// It replaces regexp.Regexp for this purpose: hunspell's pattern syntax is
// a strict subset of regex (no alternation, no repetition), so a direct
// character-by-character matcher is far cheaper than compiling an NFA.
type affixMatcher struct {
	elems    []matchElem
	isPrefix bool
}

// matchElem represents one character's worth of pattern: any char, a literal,
// an inclusive class [abc], or an exclusive class [^abc].
type matchElem struct {
	class   []rune // non-nil only for matchClass / matchNegClass
	literal rune
	kind    matchElemKind
}

type matchElemKind byte

const (
	matchAny      matchElemKind = iota // '.'    — any single character
	matchLiteral                       // 'x'    — exact rune
	matchClass                         // [abc]  — any rune in set
	matchNegClass                      // [^abc] — any rune NOT in set
)

// parseAffixPattern converts a hunspell condition string into an affixMatcher.
//
// Hunspell pattern syntax (from man 5 hunspell):
//
//	.       matches any single character
//	[abc]   matches any character in the set
//	[^abc]  matches any character NOT in the set
//	c       matches the literal character c
//
// The returned matcher is always anchored: isPrefix=true checks the word start,
// isPrefix=false checks the word end.
//
// Returns nil (not an error) when pat is "." — the hunspell convention meaning
// "no condition; always apply the rule."
func parseAffixPattern(pat string, isPrefix bool) (*affixMatcher, error) {
	// "." is the hunspell wildcard meaning "match everything"
	if pat == "." {
		return nil, nil
	}

	runes := []rune(pat)
	elems := make([]matchElem, 0, len(runes))

	for i := 0; i < len(runes); {
		switch runes[i] {
		case '.':
			elems = append(elems, matchElem{kind: matchAny})
			i++

		case '[':
			i++ // consume '['
			negate := false
			if i < len(runes) && runes[i] == '^' {
				negate = true
				i++
			}
			var class []rune
			for i < len(runes) && runes[i] != ']' {
				class = append(class, runes[i])
				i++
			}
			if i >= len(runes) {
				return nil, fmt.Errorf("unclosed '[' in affix pattern %q", pat)
			}
			i++ // consume ']'
			kind := matchClass
			if negate {
				kind = matchNegClass
			}
			elems = append(elems, matchElem{kind: kind, class: class})

		default:
			elems = append(elems, matchElem{kind: matchLiteral, literal: runes[i]})
			i++
		}
	}

	return &affixMatcher{elems: elems, isPrefix: isPrefix}, nil
}

// MatchString reports whether the pattern matches word at the appropriate boundary.
// Prefix matchers test the first len(elems) runes; suffix matchers test the last.
// No heap allocation is performed — the word is decoded on the fly via utf8.
func (m *affixMatcher) MatchString(word string) bool {
	n := len(m.elems)
	if m.isPrefix {
		pos := 0
		for _, elem := range m.elems {
			if pos >= len(word) {
				return false // word exhausted before pattern
			}
			r, size := utf8.DecodeRuneInString(word[pos:])
			if !matchRune(elem, r) {
				return false
			}
			pos += size
		}
		return true
	}

	// Suffix: walk backward from the end to find where the last n runes begin,
	// then walk forward matching each element.
	pos := len(word)
	for range n {
		if pos == 0 {
			return false // word has fewer runes than pattern needs
		}
		_, size := utf8.DecodeLastRuneInString(word[:pos])
		pos -= size
	}
	for _, elem := range m.elems {
		r, size := utf8.DecodeRuneInString(word[pos:])
		if !matchRune(elem, r) {
			return false
		}
		pos += size
	}
	return true
}

// matchRune tests a single decoded rune against one pattern element.
func matchRune(elem matchElem, c rune) bool {
	switch elem.kind {
	case matchLiteral:
		return c == elem.literal
	case matchClass:
		return slices.Contains(elem.class, c)
	case matchNegClass:
		return !slices.Contains(elem.class, c)
	default: // matchAny
		return true
	}
}
