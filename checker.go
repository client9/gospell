package gospell

import (
	"sort"
)

// Checker combines a base GoSpell dictionary with zero or more WordLists.
// WordLists are consulted before the base dictionary: the first forbidden match
// rejects the word; the first allowed match accepts it; otherwise the base
// dictionary decides.
type Checker struct {
	base  *GoSpell
	lists []*WordList
}

// NewChecker creates a Checker backed by base.
func NewChecker(base *GoSpell) *Checker {
	return &Checker{base: base}
}

// InputConversion applies the base dictionary's ICONV rules to raw input.
func (c *Checker) InputConversion(raw []byte) string {
	return c.base.InputConversion(raw)
}

// AddWordList appends wl to the active word lists.
func (c *Checker) AddWordList(wl *WordList) {
	c.lists = append(c.lists, wl)
}

// RemoveWordList removes wl from the active word lists by pointer identity.
func (c *Checker) RemoveWordList(wl *WordList) {
	for i, l := range c.lists {
		if l == wl {
			c.lists = append(c.lists[:i], c.lists[i+1:]...)
			return
		}
	}
}

// Spell reports whether word is correctly spelled.
func (c *Checker) Spell(word string) bool {
	word = c.base.inputConversionString(word)
	for _, wl := range c.lists {
		if wl.IsForbidden(word) {
			return false
		}
	}
	for _, wl := range c.lists {
		if wl.HasWord(word) {
			return true
		}
	}
	return c.base.spellConverted(word)
}

// Suggest returns spelling suggestions. The base dictionary suggester provides
// the primary results; active WordLists are scanned with brute-force edit
// distance and merged into the final set.
//
// Known limitation: words in active WordLists are not indexed by the base
// suggester. Only the brute-force scan covers them, which is fast given
// typical WordList sizes.
func (c *Checker) Suggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 {
		return nil, nil
	}
	results, err := c.base.Suggest(word, limit)
	if err != nil {
		return nil, err
	}

	// Filter base suggestions against WordList forbidden entries.
	if len(c.lists) > 0 {
		filtered := results[:0]
		for _, s := range results {
			forbidden := false
			for _, wl := range c.lists {
				if wl.IsForbidden(s.Word) {
					forbidden = true
					break
				}
			}
			if !forbidden {
				filtered = append(filtered, s)
			}
		}
		results = filtered
	}

	seen := make(map[string]struct{}, len(results))
	for _, s := range results {
		seen[s.Word] = struct{}{}
	}

	for _, wl := range c.lists {
		for w := range wl.allowed {
			if w == word {
				continue
			}
			if _, ok := seen[w]; ok {
				continue
			}
			forbidden := false
			for _, fl := range c.lists {
				if fl.IsForbidden(w) {
					forbidden = true
					break
				}
			}
			if forbidden {
				continue
			}
			seen[w] = struct{}{}
			dist := osaDistance(word, w)
			results = append(results, Suggestion{Word: w, Score: dist})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score < results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
