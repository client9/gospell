package gospell

import (
	"fmt"
	"sort"
	"unicode/utf8"

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
	deletes map[string][]int
	words   []string
	opts    SymSpellOptions
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

	// Capacity estimate: each word produces O(PrefixLength * MaxDistance) unique
	// delete keys, typically ~8x the word count for default options.
	s.deletes = make(map[string][]int, len(s.words)*8)
	seen := make(map[string]struct{}, 32)
	for i, word := range s.words {
		s.addDeletes(i, word, seen)
		for k := range seen {
			delete(seen, k)
		}
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
		if absIntLocal(utf8.RuneCountInString(candidate)-utf8.RuneCountInString(word)) > s.opts.MaxDistance {
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

func (s *SymSpellSuggester) addDeletes(id int, word string, seen map[string]struct{}) {
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

// symSpellGenerateDeletes emits every subsequence of runes reachable by
// deleting 0..maxDeletes positions. It uses an explicit stack to avoid heap
// allocation of a recursive closure.
func symSpellGenerateDeletes(runes []rune, maxDeletes int, seen map[string]struct{}, visit func(string)) {
	if len(runes) == 0 {
		return
	}
	if maxDeletes < 0 {
		maxDeletes = 0
	}

	type frame struct {
		pos, deleted int
	}

	buf := make([]rune, 0, len(runes))
	// Each stack entry records the buf length at the time the frame was pushed
	// so we can restore it when we unwind past a "keep" branch.
	type stackEntry struct {
		frame
		bufLen int
	}

	stack := make([]stackEntry, 0, len(runes)*(maxDeletes+1))
	stack = append(stack, stackEntry{frame{0, 0}, 0})

	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// Restore buf to the length saved when this frame was pushed.
		buf = buf[:top.bufLen]

		if top.deleted > maxDeletes {
			continue
		}
		if top.pos == len(runes) {
			key := string(buf)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				visit(key)
			}
			continue
		}

		// Push delete branch first (explored second, LIFO) so that the keep
		// branch is explored depth-first in the same order as the recursive
		// version.
		stack = append(stack, stackEntry{frame{top.pos + 1, top.deleted + 1}, len(buf)})

		buf = append(buf, runes[top.pos])
		stack = append(stack, stackEntry{frame{top.pos + 1, top.deleted}, len(buf)})
	}
}
