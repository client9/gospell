package gospell

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/agnivade/levenshtein"
)

// MutationOptions controls the query-time mutation suggester.
type MutationOptions struct {
	CandidateCap int
}

// MutationSuggester generates one-edit candidates from the query word and
// checks each candidate directly against the dictionary. It does not build an
// inverted index up front.
type MutationSuggester struct {
	opts MutationOptions
	src  SuggestionSource
}

var _ Suggestions = (*MutationSuggester)(nil)

// NewMutationSuggester creates a mutation-based suggester.
func NewMutationSuggester(opts MutationOptions) *MutationSuggester {
	if opts.CandidateCap <= 0 {
		opts.CandidateCap = 256
	}
	return &MutationSuggester{opts: opts}
}

// Init stores the suggestion source for later membership checks.
func (s *MutationSuggester) Init(src SuggestionSource) error {
	if src == nil {
		return fmt.Errorf("nil suggestion source")
	}
	s.src = src
	return nil
}

// Suggest returns the best matches ordered by edit distance, then keyboard
// penalty, then lexicographic order.
func (s *MutationSuggester) Suggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 || s.src == nil || word == "" {
		return nil, nil
	}

	// penalty: 0 = transposition/keyboard neighbor, 1 = generic alphabet, 2 = deletion.
	best := make(map[string]int, 32)
	for _, variant := range mutationCaseVariants(word) {
		s.collectCandidates(variant, func(candidate string, penalty int) bool {
			if candidate == "" || candidate == word {
				return true
			}
			if existing, ok := best[candidate]; ok && existing <= penalty {
				return true
			}
			if !s.src.HasWord(candidate) {
				return true
			}
			best[candidate] = penalty
			return len(best) < s.opts.CandidateCap
		})
		// Stop early across variants once cap is reached; the callback already
		// stops collectCandidates internally, but we also skip remaining variants.
		if len(best) >= s.opts.CandidateCap {
			break
		}
	}

	if len(best) == 0 {
		return nil, nil
	}

	type scored struct {
		word    string
		dist    int
		penalty int
	}

	results := make([]scored, 0, len(best))
	for candidate, penalty := range best {
		results = append(results, scored{
			word:    candidate,
			dist:    levenshtein.ComputeDistance(word, candidate),
			penalty: penalty,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].dist != results[j].dist {
			return results[i].dist < results[j].dist
		}
		if results[i].penalty != results[j].penalty {
			return results[i].penalty < results[j].penalty
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

func (s *MutationSuggester) collectCandidates(word string, visit func(candidate string, penalty int) bool) {
	runes := []rune(word)
	if len(runes) == 0 {
		return
	}

	seen := make(map[string]int, 64)
	emit := func(candidate string, penalty int) bool {
		if candidate == word {
			return true
		}
		if old, ok := seen[candidate]; ok && old <= penalty {
			return true
		}
		seen[candidate] = penalty
		return visit(candidate, penalty)
	}

	// 1. Adjacent transpositions (penalty 0 — most natural single-key error).
	for i := 0; i+1 < len(runes); i++ {
		if runes[i] == runes[i+1] {
			continue
		}
		cp := append([]rune(nil), runes...)
		cp[i], cp[i+1] = cp[i+1], cp[i]
		if !emit(string(cp), 0) {
			return
		}
	}

	// 2. Keyboard-biased substitutions (penalty 0) then generic alphabet (penalty 1).
	for i, r := range runes {
		neighbors := qwertyNeighborsForRune(r)
		for _, repl := range neighbors {
			cp := append([]rune(nil), runes...)
			cp[i] = preserveRuneCase(r, repl)
			if !emit(string(cp), 0) {
				return
			}
		}
		for _, repl := range englishAlphabetRunes {
			if unicode.ToLower(r) == repl {
				continue
			}
			cp := append([]rune(nil), runes...)
			cp[i] = preserveRuneCase(r, repl)
			if !emit(string(cp), 1) {
				return
			}
		}
	}

	// 3. Keyboard-biased insertions (penalty 0) then generic alphabet (penalty 1).
	for i := 0; i <= len(runes); i++ {
		neighbors := qwertyInsertionNeighbors(runes, i)
		for _, repl := range neighbors {
			candidate := insertRune(runes, i, repl)
			if !emit(candidate, 0) {
				return
			}
		}
		for _, repl := range englishAlphabetRunes {
			candidate := insertRune(runes, i, preserveInsertionCase(runes, i, repl))
			if !emit(candidate, 1) {
				return
			}
		}
	}

	// 4. Deletions last (penalty 2 — least likely to be the intended correction).
	for i := range runes {
		candidate := string(append(append([]rune(nil), runes[:i]...), runes[i+1:]...))
		if !emit(candidate, 2) {
			return
		}
	}
}

func mutationCaseVariants(word string) []string {
	runes := []rune(word)
	// title-cases a rune slice without byte-slicing.
	toTitle := func(r []rune) string {
		if len(r) == 0 {
			return ""
		}
		return string(unicode.ToUpper(r[0])) + string(r[1:])
	}
	switch caseStyle(word) {
	case allLower:
		return []string{word, toTitle(runes), strings.ToUpper(word)}
	case titleCase:
		return []string{word, strings.ToLower(word)}
	case allUpper:
		lower := []rune(strings.ToLower(word))
		return []string{word, string(lower), toTitle(lower)}
	default:
		return []string{word, strings.ToLower(word)}
	}
}

func qwertyNeighborsForRune(r rune) []rune {
	if repls, ok := qwertyNeighbors[unicode.ToLower(r)]; ok {
		out := make([]rune, len(repls))
		for i, repl := range repls {
			out[i] = preserveRuneCase(r, repl)
		}
		return out
	}
	return nil
}

func qwertyInsertionNeighbors(runes []rune, pos int) []rune {
	seen := make(map[rune]struct{}, 8)
	var out []rune
	add := func(base, repl rune) {
		r := preserveRuneCase(base, repl)
		key := unicode.ToLower(r)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	if pos > 0 {
		for _, repl := range qwertyNeighborsForRune(runes[pos-1]) {
			add(runes[pos-1], repl)
		}
	}
	if pos < len(runes) {
		for _, repl := range qwertyNeighborsForRune(runes[pos]) {
			add(runes[pos], repl)
		}
	}
	return out
}

func preserveRuneCase(base, repl rune) rune {
	if unicode.IsUpper(base) {
		return unicode.ToUpper(repl)
	}
	return unicode.ToLower(repl)
}

func preserveInsertionCase(runes []rune, pos int, repl rune) rune {
	if pos > 0 {
		return preserveRuneCase(runes[pos-1], repl)
	}
	if pos < len(runes) {
		return preserveRuneCase(runes[pos], repl)
	}
	return repl
}

func insertRune(runes []rune, pos int, repl rune) string {
	out := make([]rune, 0, len(runes)+1)
	out = append(out, runes[:pos]...)
	out = append(out, repl)
	out = append(out, runes[pos:]...)
	return string(out)
}

var englishAlphabetRunes = []rune("abcdefghijklmnopqrstuvwxyz")

var qwertyNeighbors = map[rune][]rune{
	'q': {'w', 'a'},
	'w': {'q', 'e', 'a', 's'},
	'e': {'w', 'r', 's', 'd'},
	'r': {'e', 't', 'd', 'f'},
	't': {'r', 'y', 'f', 'g'},
	'y': {'t', 'u', 'g', 'h'},
	'u': {'y', 'i', 'h', 'j'},
	'i': {'u', 'o', 'j', 'k'},
	'o': {'i', 'p', 'k', 'l'},
	'p': {'o', 'l'},
	'a': {'q', 'w', 's', 'z'},
	's': {'a', 'w', 'e', 'd', 'x', 'z'},
	'd': {'s', 'e', 'r', 'f', 'c', 'x'},
	'f': {'d', 'r', 't', 'g', 'v', 'c'},
	'g': {'f', 't', 'y', 'h', 'b', 'v'},
	'h': {'g', 'y', 'u', 'j', 'n', 'b'},
	'j': {'h', 'u', 'i', 'k', 'm', 'n'},
	'k': {'j', 'i', 'o', 'l', 'm'},
	'l': {'k', 'o', 'p'},
	'z': {'a', 's', 'x'},
	'x': {'z', 's', 'd', 'c'},
	'c': {'x', 'd', 'f', 'v'},
	'v': {'c', 'f', 'g', 'b'},
	'b': {'v', 'g', 'h', 'n'},
	'n': {'b', 'h', 'j', 'm'},
	'm': {'n', 'j', 'k'},
}
