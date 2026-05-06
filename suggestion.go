package gospell

import (
	"fmt"
	"sort"

	"github.com/agnivade/levenshtein"
)

// SuggestionSource provides read-only access to a loaded dictionary so a
// suggester can build indexes or other internal structures.
type SuggestionSource interface {
	ForEachWord(func(word string) bool)
	HasWord(word string) bool
	WordCount() int
	MaxWordLen() int
}

// Suggestion is a ranked candidate returned by a suggestion engine.
type Suggestion struct {
	Word  string
	Score int
}

// Suggestions is the pluggable suggestion engine interface used by GoSpell.
type Suggestions interface {
	Init(src SuggestionSource) error
	Suggest(word string, limit int) ([]Suggestion, error)
}

// LevenshteinOptions controls the default brute-force suggestion engine.
type LevenshteinOptions struct {
	MaxDistance int
}

// LevenshteinSuggester is a simple reference implementation that scans every
// loaded dictionary word and ranks candidates by edit distance.
type LevenshteinSuggester struct {
	opts  LevenshteinOptions
	words []string
}

var (
	_ SuggestionSource = (*GoSpell)(nil)
	_ Suggestions      = (*LevenshteinSuggester)(nil)
)

// NewLevenshteinSuggester creates a brute-force suggester backed by
// agnivade/levenshtein.
func NewLevenshteinSuggester(opts LevenshteinOptions) *LevenshteinSuggester {
	if opts.MaxDistance == 0 {
		opts.MaxDistance = -1
	}
	return &LevenshteinSuggester{opts: opts}
}

// Init snapshots the current dictionary words.
func (s *LevenshteinSuggester) Init(src SuggestionSource) error {
	if src == nil {
		return fmt.Errorf("nil suggestion source")
	}

	s.words = make([]string, 0, src.WordCount())
	src.ForEachWord(func(word string) bool {
		s.words = append(s.words, word)
		return true
	})
	sort.Strings(s.words)
	return nil
}

// Suggest returns the best matches ordered by edit distance, then score, then
// lexicographic order.
func (s *LevenshteinSuggester) Suggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 {
		return nil, nil
	}
	if len(s.words) == 0 {
		return nil, nil
	}
	if word == "" {
		return nil, nil
	}

	best := make([]Suggestion, 0, limit)
	for _, candidate := range s.words {
		if candidate == word {
			continue
		}
		if s.opts.MaxDistance >= 0 && absIntLocal(len(candidate)-len(word)) > s.opts.MaxDistance {
			continue
		}

		dist := levenshtein.ComputeDistance(word, candidate)
		if s.opts.MaxDistance >= 0 && dist > s.opts.MaxDistance {
			continue
		}

		item := Suggestion{Word: candidate, Score: -dist}
		pos := sort.Search(len(best), func(i int) bool {
			if best[i].Score != item.Score {
				return best[i].Score < item.Score
			}
			return best[i].Word > item.Word
		})

		if pos == len(best) {
			if len(best) < limit {
				best = append(best, item)
			}
			continue
		}

		best = append(best, Suggestion{})
		copy(best[pos+1:], best[pos:])
		best[pos] = item
		if len(best) > limit {
			best = best[:limit]
		}
	}

	for i := range best {
		best[i].Score = -best[i].Score
	}
	return best, nil
}

func absIntLocal(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
