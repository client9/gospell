package gospell

import (
	"fmt"
	"sort"

	"github.com/agnivade/levenshtein"
)

// SymSpellOptions controls the delete-index suggestion engine.
type SymSpellOptions struct {
	MaxDistance  int
	PrefixLength int
}

// SymSpellSuggester implements a SymSpell-style delete index.
//
// The dictionary is indexed once by storing every word under the strings that
// can be produced by deleting up to MaxDistance runes from the first
// PrefixLength runes. Queries use the same delete generation to recover a
// small candidate set, which is then reranked by edit distance.
type SymSpellSuggester struct {
	opts    SymSpellOptions
	words   []string
	deletes map[string][]int
}

var _ Suggestions = (*SymSpellSuggester)(nil)

// NewSymSpellSuggester creates a delete-index suggester.
func NewSymSpellSuggester(opts SymSpellOptions) *SymSpellSuggester {
	if opts.MaxDistance <= 0 {
		opts.MaxDistance = 2
	}
	if opts.PrefixLength <= 0 {
		opts.PrefixLength = 7
	}
	if opts.PrefixLength < opts.MaxDistance {
		opts.PrefixLength = opts.MaxDistance
	}
	return &SymSpellSuggester{opts: opts}
}

// Init snapshots the dictionary words and builds the delete index.
func (s *SymSpellSuggester) Init(src SuggestionSource) error {
	if src == nil {
		return fmt.Errorf("nil suggestion source")
	}

	s.words = make([]string, 0, src.WordCount())
	src.ForEachWord(func(word string) bool {
		s.words = append(s.words, word)
		return true
	})

	s.deletes = make(map[string][]int, len(s.words)*8)
	for i, word := range s.words {
		s.addDeletes(i, word)
	}
	return nil
}

// Suggest returns the best matches ordered by edit distance, then word.
func (s *SymSpellSuggester) Suggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 || len(s.words) == 0 || word == "" {
		return nil, nil
	}

	candidates := make(map[int]struct{}, 32)
	s.collectCandidates(word, func(id int) {
		candidates[id] = struct{}{}
	})

	if len(candidates) == 0 {
		return nil, nil
	}

	type scored struct {
		word string
		dist int
	}

	results := make([]scored, 0, len(candidates))
	for id := range candidates {
		candidate := s.words[id]
		if candidate == word {
			continue
		}
		if absIntSymSpell(len(candidate)-len(word)) > s.opts.MaxDistance {
			continue
		}

		dist := levenshtein.ComputeDistance(word, candidate)
		if dist > s.opts.MaxDistance {
			continue
		}
		results = append(results, scored{word: candidate, dist: dist})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].dist != results[j].dist {
			return results[i].dist < results[j].dist
		}
		return results[i].word < results[j].word
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]Suggestion, len(results))
	for i := range results {
		out[i] = Suggestion{Word: results[i].word, Score: results[i].dist}
	}
	return out, nil
}

func (s *SymSpellSuggester) addDeletes(id int, word string) {
	seen := make(map[string]struct{}, 16)
	runes := []rune(word)
	if len(runes) == 0 {
		return
	}
	if max := s.opts.PrefixLength; max > 0 && len(runes) > max {
		runes = runes[:max]
	}
	symSpellGenerateDeletes(runes, s.opts.MaxDistance, seen, func(key string) {
		s.deletes[key] = append(s.deletes[key], id)
	})
}

func (s *SymSpellSuggester) collectCandidates(word string, visit func(id int)) {
	seenKeys := make(map[string]struct{}, 16)
	runes := []rune(word)
	if len(runes) == 0 {
		return
	}
	if max := s.opts.PrefixLength; max > 0 && len(runes) > max {
		runes = runes[:max]
	}
	symSpellGenerateDeletes(runes, s.opts.MaxDistance, seenKeys, func(key string) {
		for _, id := range s.deletes[key] {
			visit(id)
		}
	})
}

func symSpellGenerateDeletes(runes []rune, maxDeletes int, seen map[string]struct{}, visit func(string)) {
	if len(runes) == 0 {
		return
	}
	if maxDeletes < 0 {
		maxDeletes = 0
	}

	buf := make([]rune, 0, len(runes))
	var walk func(pos, deleted int)
	walk = func(pos, deleted int) {
		if deleted > maxDeletes {
			return
		}
		if pos == len(runes) {
			key := string(buf)
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			visit(key)
			return
		}

		buf = append(buf, runes[pos])
		walk(pos+1, deleted)
		buf = buf[:len(buf)-1]

		walk(pos+1, deleted+1)
	}

	walk(0, 0)
}

func absIntSymSpell(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
