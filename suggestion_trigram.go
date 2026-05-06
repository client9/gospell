package gospell

import (
	"fmt"
	"sort"
	"slices"

	"github.com/agnivade/levenshtein"
)

// TrigramOptions controls the hashed-trigram suggestion engine.
type TrigramOptions struct {
	RerankLimit   int
	MaxLengthDiff int
}

// TrigramSuggester builds a hashed trigram inverted index once and then
// narrows candidates by postings-list overlap before reranking with Levenshtein.
type TrigramSuggester struct {
	opts          TrigramOptions
	words         []string
	postings      map[uint64][]int
	wordGramCount  []int
	wordLen        []int
	maxWordLen     int
}

var _ Suggestions = (*TrigramSuggester)(nil)

// NewTrigramSuggester creates a trigram-index suggester.
func NewTrigramSuggester(opts TrigramOptions) *TrigramSuggester {
	if opts.MaxLengthDiff == 0 {
		opts.MaxLengthDiff = 4
	}
	if opts.RerankLimit < 0 {
		opts.RerankLimit = 0
	}
	return &TrigramSuggester{opts: opts}
}

// Init snapshots the dictionary and builds the trigram postings index.
func (s *TrigramSuggester) Init(src SuggestionSource) error {
	if src == nil {
		return fmt.Errorf("nil suggestion source")
	}

	s.maxWordLen = src.MaxWordLen()
	s.words = make([]string, 0, src.WordCount())
	src.ForEachWord(func(word string) bool {
		s.words = append(s.words, word)
		return true
	})
	slices.Sort(s.words)

	s.postings = make(map[uint64][]int, len(s.words)*6)
	s.wordGramCount = make([]int, len(s.words))
	s.wordLen = make([]int, len(s.words))

	scratch := make([]uint64, 0, s.maxWordLen+2)
	for i, word := range s.words {
		grams := trigramHashesInto(scratch, word)
		slices.Sort(grams)
		grams = dedupeSortedUint64Trigram(grams)

		s.wordGramCount[i] = len(grams)
		s.wordLen[i] = len(word)
		for _, gram := range grams {
			s.postings[gram] = append(s.postings[gram], i)
		}
	}

	return nil
}

// Suggest returns the best matches ordered by edit distance, then trigram score.
func (s *TrigramSuggester) Suggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 {
		return nil, nil
	}
	if len(s.words) == 0 || word == "" {
		return nil, nil
	}

	queryGrams := trigramHashesInto(make([]uint64, 0, len(word)+2), word)
	slices.Sort(queryGrams)
	queryGrams = dedupeSortedUint64Trigram(queryGrams)
	if len(queryGrams) == 0 {
		return nil, nil
	}

	counts := make([]uint16, len(s.words))
	touched := make([]int, 0, len(s.words)/4)
	for _, gram := range queryGrams {
		for _, id := range s.postings[gram] {
			if counts[id] == 0 {
				touched = append(touched, id)
			}
			counts[id]++
		}
	}

	type scored struct {
		word   string
		shared int
		score  int
		dist   int
	}

	candidates := make([]scored, 0, len(touched))
	for _, id := range touched {
		shared := int(counts[id])
		counts[id] = 0
		if shared == 0 {
			continue
		}
		if s.opts.MaxLengthDiff >= 0 && absIntTrigram(s.wordLen[id]-len(word)) > s.opts.MaxLengthDiff {
			continue
		}

		score := (2 * shared * 1000000) / (len(queryGrams) + s.wordGramCount[id])
		candidates = append(candidates, scored{
			word:   s.words[id],
			shared: shared,
			score:  score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].shared != candidates[j].shared {
			return candidates[i].shared > candidates[j].shared
		}
		return candidates[i].word < candidates[j].word
	})

	if s.opts.RerankLimit > 0 && len(candidates) > s.opts.RerankLimit {
		candidates = candidates[:s.opts.RerankLimit]
	}

	for i := range candidates {
		candidates[i].dist = levenshtein.ComputeDistance(word, candidates[i].word)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].dist != candidates[j].dist {
			return candidates[i].dist < candidates[j].dist
		}
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].word < candidates[j].word
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]Suggestion, len(candidates))
	for i := range candidates {
		out[i] = Suggestion{
			Word:  candidates[i].word,
			Score: candidates[i].dist,
		}
	}
	return out, nil
}

func hash3Runes(a, b, c rune) uint64 {
	x := uint64(a)*0x9e3779b97f4a7c15 ^
		(uint64(b)+0xbf58476d1ce4e5b9) ^
		(uint64(c)<<1 | 1)
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

func trigramHashesInto(dst []uint64, word string) []uint64 {
	const pad = rune(0)

	dst = dst[:0]
	prev2, prev1 := pad, pad
	for _, r := range word {
		dst = append(dst, hash3Runes(prev2, prev1, r))
		prev2, prev1 = prev1, r
	}
	dst = append(dst, hash3Runes(prev2, prev1, pad))
	dst = append(dst, hash3Runes(prev1, pad, pad))
	return dst
}

func dedupeSortedUint64Trigram(dst []uint64) []uint64 {
	if len(dst) < 2 {
		return dst
	}

	n := 1
	for i := 1; i < len(dst); i++ {
		if dst[i] != dst[n-1] {
			dst[n] = dst[i]
			n++
		}
	}
	return dst[:n]
}

func absIntTrigram(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

