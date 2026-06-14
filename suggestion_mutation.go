package gospell

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/agnivade/levenshtein"
)

// MutationOptions controls the query-time mutation suggester.
type MutationOptions struct {
	CandidateCap int
	NGramRootCap int
}

// MutationSuggester generates one-edit candidates from the query word and
// checks each candidate directly against the dictionary. It does not build an
// inverted index up front.
type MutationSuggester struct {
	src        SuggestionSource
	spell      func(string) bool
	tryChars   []rune
	ngramOnce  sync.Once
	ngramRoots []mutationNGramRoot
	opts       MutationOptions
}

var _ Suggestions = (*MutationSuggester)(nil)

// NewMutationSuggester creates a mutation-based suggester.
func NewMutationSuggester(opts MutationOptions) *MutationSuggester {
	if opts.CandidateCap <= 0 {
		opts.CandidateCap = 256
	}
	if opts.NGramRootCap <= 0 {
		opts.NGramRootCap = 64
	}
	return &MutationSuggester{opts: opts}
}

// Init stores the suggestion source for later membership checks.
func (s *MutationSuggester) Init(src SuggestionSource) error {
	if src == nil {
		return fmt.Errorf("nil suggestion source")
	}
	s.src = src
	s.spell = src.HasWord
	if spellSrc, ok := src.(interface{ Spell(string) bool }); ok {
		s.spell = spellSrc.Spell
	}
	if gs, ok := src.(*GoSpell); ok && gs.affix != nil && gs.affix.TryChars != "" {
		s.tryChars = uniqueLowerRunes(gs.affix.TryChars)
	} else {
		s.tryChars = nil
	}
	s.ngramOnce = sync.Once{}
	s.ngramRoots = nil
	return nil
}

// Suggest returns the best matches ordered by edit distance, then keyboard
// penalty, then lexicographic order.
func (s *MutationSuggester) Suggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 || s.src == nil || s.spell == nil || word == "" {
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
			if !s.spell(candidate) {
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
		return s.ngramSuggest(word, limit)
	}

	// Deduplicate case variants: "guidance", "Guidance", "GUIDANCE" are all
	// one edit away from "guidence" and all pass spell check (via case fallback
	// in spellConverted). Keep only the candidate whose case style best matches
	// the query's case style; when two candidates tie on case priority, keep
	// the lower penalty one.
	best = dedupCaseVariants(word, best)

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

func (s *MutationSuggester) ngramSuggest(word string, limit int) ([]Suggestion, error) {
	if limit <= 0 {
		return nil, nil
	}
	gs, ok := s.src.(*GoSpell)
	if !ok || gs.affix == nil || len(gs.entriesByRoot) == 0 {
		return nil, nil
	}

	roots := s.topNGramRoots(gs, word)
	if len(roots) == 0 {
		return nil, nil
	}

	queryPrep := newNgramQuery(word)

	type scored struct {
		word  string
		dist  int
		score int
	}
	best := make(map[string]scored, s.opts.CandidateCap)
	for _, root := range roots {
		for _, entry := range gs.entriesByRoot[root.word] {
			if !entrySuggestable(entry.rawFlags, gs.affix) {
				continue
			}
			records, err := gs.affix.expandRecords(entry.line)
			if err != nil {
				continue
			}
			for _, rec := range records {
				candidate := rec.word
				if candidate == "" || candidate == word {
					continue
				}
				if _, ok := best[candidate]; ok {
					continue
				}
				if !s.spell(candidate) {
					continue
				}
				score := ngramSuggestionScore(queryPrep, candidate)
				best[candidate] = scored{
					word:  candidate,
					dist:  levenshtein.ComputeDistance(word, candidate),
					score: score,
				}
				if len(best) >= s.opts.CandidateCap {
					break
				}
			}
			if len(best) >= s.opts.CandidateCap {
				break
			}
		}
		if len(best) >= s.opts.CandidateCap {
			break
		}
	}
	if len(best) == 0 {
		return nil, nil
	}

	results := make([]scored, 0, len(best))
	for _, item := range best {
		results = append(results, item)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
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

type ngramRootCandidate struct {
	word  string
	score int
}

type mutationNGramRoot struct {
	word     string
	runes    []rune
	trigrams []uint64
}

func buildMutationNGramRoots(gs *GoSpell) []mutationNGramRoot {
	roots := make([]mutationNGramRoot, 0, len(gs.entriesByRoot))
	for root, entries := range gs.entriesByRoot {
		if !anyEntrySuggestable(entries, gs.affix) {
			continue
		}
		lower := strings.ToLower(root)
		roots = append(roots, mutationNGramRoot{
			word:     root,
			runes:    []rune(lower),
			trigrams: sortedNGramHashes(lower, 3),
		})
	}
	return roots
}

func (s *MutationSuggester) topNGramRoots(gs *GoSpell, word string) []ngramRootCandidate {
	s.ngramOnce.Do(func() {
		s.ngramRoots = buildMutationNGramRoots(gs)
	})
	roots := make([]ngramRootCandidate, 0, s.opts.NGramRootCap)
	queryLower := strings.ToLower(word)
	queryRunes := []rune(queryLower)
	queryTrigrams := sortedNGramHashes(queryLower, 3)
	add := func(candidate ngramRootCandidate) {
		if candidate.score <= 0 {
			return
		}
		pos := sort.Search(len(roots), func(i int) bool {
			if roots[i].score != candidate.score {
				return roots[i].score < candidate.score
			}
			return roots[i].word > candidate.word
		})
		if pos == len(roots) {
			if len(roots) < s.opts.NGramRootCap {
				roots = append(roots, candidate)
			}
			return
		}
		roots = append(roots, ngramRootCandidate{})
		copy(roots[pos+1:], roots[pos:])
		roots[pos] = candidate
		if len(roots) > s.opts.NGramRootCap {
			roots = roots[:s.opts.NGramRootCap]
		}
	}
	for _, root := range s.ngramRoots {
		if root.word == word {
			continue
		}
		add(ngramRootCandidate{
			word:  root.word,
			score: ngramRootScorePrepared(queryRunes, queryTrigrams, root),
		})
	}
	return roots
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
	swapBuf := append([]rune(nil), runes...)
	for i := 0; i+1 < len(runes); i++ {
		if runes[i] == runes[i+1] {
			continue
		}
		copy(swapBuf, runes)
		swapBuf[i], swapBuf[i+1] = swapBuf[i+1], swapBuf[i]
		if !emit(string(swapBuf), 0) {
			return
		}
	}

	// 2. Keyboard-biased substitutions (penalty 0) then generic alphabet (penalty 1).
	subBuf := append([]rune(nil), runes...)
	for i, r := range runes {
		neighbors := qwertyNeighborsForRune(r)
		for _, repl := range neighbors {
			copy(subBuf, runes)
			subBuf[i] = preserveRuneCase(r, repl)
			if !emit(string(subBuf), 0) {
				return
			}
		}
		for _, repl := range s.replacementRunes() {
			if unicode.ToLower(r) == repl {
				continue
			}
			copy(subBuf, runes)
			subBuf[i] = preserveRuneCase(r, repl)
			if !emit(string(subBuf), 1) {
				return
			}
		}
	}

	// 3. Keyboard-biased insertions (penalty 0) then generic alphabet (penalty 1).
	insertBuf := make([]rune, len(runes)+1)
	for i := 0; i <= len(runes); i++ {
		neighbors := qwertyInsertionNeighbors(runes, i)
		for _, repl := range neighbors {
			candidate := insertRuneInto(insertBuf, runes, i, repl)
			if !emit(candidate, 0) {
				return
			}
		}
		for _, repl := range s.replacementRunes() {
			candidate := insertRuneInto(insertBuf, runes, i, preserveInsertionCase(runes, i, repl))
			if !emit(candidate, 1) {
				return
			}
		}
	}

	// 4. Deletions last (penalty 2 — least likely to be the intended correction).
	if len(runes) <= 1 {
		return
	}
	deleteBuf := make([]rune, len(runes)-1)
	for i := range runes {
		copy(deleteBuf, runes[:i])
		copy(deleteBuf[i:], runes[i+1:])
		candidate := string(deleteBuf)
		if !emit(candidate, 2) {
			return
		}
	}
}

// dedupCaseVariants removes case-equivalent duplicates from candidates.
// For each group of candidates that differ only in case, it keeps the one
// whose case style best matches the query's case style (e.g. for an allLower
// query, "guidance" beats "Guidance" beats "GUIDANCE").
func dedupCaseVariants(query string, candidates map[string]int) map[string]int {
	queryStyle := caseStyle(query)
	type entry struct {
		word    string
		penalty int
		pri     int
	}
	deduped := make(map[string]entry, len(candidates))
	for candidate, penalty := range candidates {
		key := strings.ToLower(candidate)
		pri := casePriority(caseStyle(candidate), queryStyle)
		if existing, ok := deduped[key]; !ok || pri < existing.pri || (pri == existing.pri && penalty < existing.penalty) {
			deduped[key] = entry{candidate, penalty, pri}
		}
	}
	out := make(map[string]int, len(deduped))
	for _, e := range deduped {
		out[e.word] = e.penalty
	}
	return out
}

// casePriority returns how well candidateStyle matches queryStyle.
// Lower is better; 0 means exact match.
func casePriority(candidateStyle, queryStyle wordCase) int {
	if candidateStyle == queryStyle {
		return 0
	}
	switch queryStyle {
	case allLower:
		switch candidateStyle {
		case titleCase:
			return 1
		case allUpper:
			return 2
		default:
			return 3
		}
	case titleCase:
		switch candidateStyle {
		case allLower:
			return 1
		case allUpper:
			return 2
		default:
			return 3
		}
	case allUpper:
		switch candidateStyle {
		case titleCase:
			return 1
		case allLower:
			return 2
		default:
			return 3
		}
	default:
		return 3
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
		return repls
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

func insertRuneInto(out []rune, runes []rune, pos int, repl rune) string {
	out = out[:len(runes)+1]
	copy(out, runes[:pos])
	out[pos] = repl
	copy(out[pos+1:], runes[pos:])
	return string(out)
}

func (s *MutationSuggester) replacementRunes() []rune {
	if len(s.tryChars) > 0 {
		return s.tryChars
	}
	return englishAlphabetRunes
}

func uniqueLowerRunes(chars string) []rune {
	seen := make(map[rune]struct{}, len(chars))
	out := make([]rune, 0, len(chars))
	for _, r := range chars {
		r = unicode.ToLower(r)
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func anyEntrySuggestable(entries []dictionaryEntry, affix *dictConfig) bool {
	for _, entry := range entries {
		if entrySuggestable(entry.rawFlags, affix) {
			return true
		}
	}
	return false
}

func entrySuggestable(flags []string, affix *dictConfig) bool {
	if affix == nil {
		return true
	}
	return !hasFlagToken(flags, affix.ForbiddenWordFlag) &&
		!hasFlagToken(flags, affix.NoSuggestFlag) &&
		!hasFlagToken(flags, affix.CompoundOnly)
}

func ngramRootScorePrepared(queryRunes []rune, queryTrigrams []uint64, root mutationNGramRoot) int {
	if absIntLocal(len(queryRunes)-len(root.runes)) > 5 {
		return 0
	}
	score := ngramOverlapHashes(queryTrigrams, root.trigrams)*4 + leftCommonRunes(queryRunes, root.runes)*2
	score -= absIntLocal(len(queryRunes) - len(root.runes))
	return score
}

// ngramQuery holds pre-computed query data for ngramSuggestionScore so the
// query n-gram maps are built once per Suggest call rather than per candidate.
type ngramQuery struct {
	lower    string
	runes    []rune
	trigrams map[string]int
	bigrams  map[string]int
}

func newNgramQuery(query string) ngramQuery {
	lower := strings.ToLower(query)
	return ngramQuery{
		lower:    lower,
		runes:    []rune(lower),
		trigrams: ngramCounts(lower, 3),
		bigrams:  ngramCounts(lower, 2),
	}
}

func ngramSuggestionScore(q ngramQuery, candidate string) int {
	candidateLower := strings.ToLower(candidate)
	cr := []rune(candidateLower)
	score := ngramOverlapScorePrepared(q.trigrams, candidateLower, 3) * 5
	score += ngramOverlapScorePrepared(q.bigrams, candidateLower, 2) * 2
	score += leftCommonRunes(q.runes, cr) * 2
	score += lcsRunes(q.runes, cr) * 2
	score -= absIntLocal(len(q.runes) - len(cr))
	return score
}

// ngramOverlapScorePrepared is like ngramOverlapScore but accepts a
// pre-computed n-gram map for the query side.
func ngramOverlapScorePrepared(queryNgrams map[string]int, candidate string, n int) int {
	if n <= 0 || len(queryNgrams) == 0 {
		return 0
	}
	score := 0
	for gram, count := range ngramCounts(candidate, n) {
		if ac := queryNgrams[gram]; ac > 0 {
			if count < ac {
				score += count
			} else {
				score += ac
			}
		}
	}
	return score
}

func sortedNGramHashes(word string, n int) []uint64 {
	if n <= 0 {
		return nil
	}
	runes := []rune(word)
	if len(runes) == 0 {
		return nil
	}
	padded := make([]rune, 0, len(runes)+2)
	padded = append(padded, '^')
	padded = append(padded, runes...)
	padded = append(padded, '$')
	if len(padded) < n {
		return []uint64{hashRunes(padded)}
	}
	out := make([]uint64, 0, len(padded)-n+1)
	for i := 0; i+n <= len(padded); i++ {
		out = append(out, hashRunes(padded[i:i+n]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func ngramOverlapHashes(a, b []uint64) int {
	score := 0
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			score++
			i++
			j++
		}
	}
	return score
}

func hashRunes(runes []rune) uint64 {
	var h uint64 = 1469598103934665603
	for _, r := range runes {
		h ^= uint64(r)
		h *= 1099511628211
	}
	return h
}

func ngramCounts(word string, n int) map[string]int {
	runes := []rune(word)
	if len(runes) == 0 {
		return nil
	}
	padded := make([]rune, 0, len(runes)+2)
	padded = append(padded, '^')
	padded = append(padded, runes...)
	padded = append(padded, '$')
	if len(padded) < n {
		return map[string]int{string(padded): 1}
	}
	out := make(map[string]int, len(padded)-n+1)
	for i := 0; i+n <= len(padded); i++ {
		out[string(padded[i:i+n])]++
	}
	return out
}

func leftCommonRunes(a, b []rune) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func lcsRunes(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				curr[j] = prev[j-1] + 1
			} else if prev[j] > curr[j-1] {
				curr[j] = prev[j]
			} else {
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
		clear(curr)
	}
	return prev[len(b)]
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
