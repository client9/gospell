package gospell

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
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
	repRules   [][2]string
	ngramRoots []mutationNGramRoot
	opts       MutationOptions
	ngramOnce  sync.Once
}

var _ Suggestions = (*MutationSuggester)(nil)

// NewMutationSuggester creates a mutation-based suggester.
func NewMutationSuggester(opts MutationOptions) *MutationSuggester {
	if opts.CandidateCap <= 0 {
		opts.CandidateCap = 256
	}
	if opts.NGramRootCap <= 0 {
		opts.NGramRootCap = 100
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
	if gs, ok := src.(*GoSpell); ok && gs.affix != nil {
		if gs.affix.TryChars != "" {
			s.tryChars = uniqueLowerRunes(gs.affix.TryChars)
		} else {
			s.tryChars = nil
		}
		if len(gs.repReplacements) > 0 {
			s.repRules = append([][2]string(nil), gs.repReplacements...)
		} else {
			s.repRules = nil
		}
	} else {
		s.tryChars = nil
		s.repRules = nil
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

	// penalty: 0=swap/REP, 1=keyboard-sub, 2=delete, 3=insert, 4=generic-sub.
	best := make(map[string]int, 32)

	// REP rules are highest priority (matches hunspell's SPELL_BEST_SUG behavior):
	// apply them first, and skip the character-mutation pass if any hit is found.
	for _, rule := range s.repRules {
		from, to := rule[0], rule[1]
		if from == "" || to == "" {
			continue
		}
		start := 0
		for start < len(word) {
			idx := strings.Index(word[start:], from)
			if idx < 0 {
				break
			}
			abs := start + idx
			candidate := word[:abs] + to + word[abs+len(from):]
			if candidate != word && s.spell(candidate) {
				if existing, ok := best[candidate]; !ok || existing > 0 {
					best[candidate] = 0
				}
			}
			start = abs + 1
		}
	}

	if len(best) == 0 {
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
		word       string
		dist       int
		penalty    int
		leftCommon int
	}

	queryRunes := []rune(strings.ToLower(word))
	results := make([]scored, 0, len(best))
	for candidate, penalty := range best {
		results = append(results, scored{
			word:       candidate,
			dist:       osaDistance(word, candidate),
			penalty:    penalty,
			leftCommon: leftCommonRunes(queryRunes, []rune(strings.ToLower(candidate))),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].dist != results[j].dist {
			return results[i].dist < results[j].dist
		}
		if results[i].penalty != results[j].penalty {
			return results[i].penalty < results[j].penalty
		}
		if results[i].leftCommon != results[j].leftCommon {
			return results[i].leftCommon > results[j].leftCommon
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

	queryLower := strings.ToLower(word)
	queryRunes := []rune(queryLower)
	queryBigrams := sortedNGramHashes(queryLower, 2)
	queryTrigrams := sortedNGramHashes(queryLower, 3)

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
				candidateRoot := makeMutationNGramRoot(candidate, strings.ToLower(candidate))
				score := ngramRootScorePrepared(queryRunes, queryBigrams, queryTrigrams, candidateRoot)
				best[candidate] = scored{
					word:  candidate,
					dist:  osaDistance(word, candidate),
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
	bigrams  []uint64
	trigrams []uint64
}

func makeMutationNGramRoot(word, lower string) mutationNGramRoot {
	return mutationNGramRoot{
		word:     word,
		runes:    []rune(lower),
		bigrams:  sortedNGramHashes(lower, 2),
		trigrams: sortedNGramHashes(lower, 3),
	}
}

func buildMutationNGramRoots(gs *GoSpell) []mutationNGramRoot {
	roots := make([]mutationNGramRoot, 0, len(gs.entriesByRoot))
	seen := make(map[string]struct{}, len(gs.entriesByRoot))

	for root, entries := range gs.entriesByRoot {
		if !anyEntrySuggestable(entries, gs.affix) {
			continue
		}
		lower := strings.ToLower(root)
		seen[lower] = struct{}{}
		roots = append(roots, makeMutationNGramRoot(root, lower))
	}

	// Also index prefix-derived surface forms as virtual roots.
	// Words like "disappoint" are not roots — they're derived from root "appoint"
	// via a prefix rule (flag E → "dis"). Without this, the n-gram index only
	// contains "appoint", which scores poorly against "dissapoint" because the
	// leading characters differ. By indexing the surface form "disappoint"
	// directly (pointing back to root "appoint" for expansion), the n-gram pass
	// can find it with a high score.
	if gs.affix != nil {
		for flagKey, af := range gs.affix.AffixMap {
			if af.Type != prefix {
				continue
			}
			for root, entries := range gs.entriesByRoot {
				if !anyEntrySuggestable(entries, gs.affix) {
					continue
				}
				hasFlag := false
				for _, entry := range entries {
					if hasFlagToken(entry.rawFlags, flagKey) {
						hasFlag = true
						break
					}
				}
				if !hasFlag {
					continue
				}
				for _, r := range af.Rules {
					if r.AffixText == "" {
						continue
					}
					if r.matcher != nil && !r.matcher.MatchString(root) {
						continue
					}
					stemBase := root
					if r.Strip != "" {
						if !strings.HasPrefix(root, r.Strip) {
							continue
						}
						stemBase = root[len(r.Strip):]
					}
					surface := r.AffixText + stemBase
					lower := strings.ToLower(surface)
					if _, ok := seen[lower]; ok {
						continue
					}
					seen[lower] = struct{}{}
					roots = append(roots, makeMutationNGramRoot(root, lower))
				}
			}
		}
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
	queryBigrams := sortedNGramHashes(queryLower, 2)
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
			score: ngramRootScorePrepared(queryRunes, queryBigrams, queryTrigrams, root),
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

	// 1. swapchar: adjacent transpositions (penalty 0).
	// Lowest penalty — a transposition is the most likely single-keystroke error.
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

	// 1b. movechars: rotate a substring [p,q+1) to move one character to another
	// position (hunspell's movechar). Covers transpositions beyond adjacent pairs
	// (e.g. "greatful" → "grateful" by rotating "eat"→"ate"). Skip q-p==1 for
	// words ≥ 6 runes to avoid duplicating swapchar.
	if len(runes) > 2 {
		moveBuf := make([]rune, len(runes))
		for p := 0; p < len(runes); p++ {
			for q := p + 1; q < len(runes); q++ {
				if len(runes) >= 6 && q-p == 1 {
					continue
				}
				// Move runes[p] to position q (left-rotate [p..q]).
				copy(moveBuf, runes)
				r := moveBuf[p]
				copy(moveBuf[p:], moveBuf[p+1:q+1])
				moveBuf[q] = r
				if !emit(string(moveBuf), 3) {
					return
				}
				// Move runes[q] to position p (right-rotate [p..q]).
				copy(moveBuf, runes)
				r = moveBuf[q]
				copy(moveBuf[p+1:q+1], moveBuf[p:q])
				moveBuf[p] = r
				if !emit(string(moveBuf), 3) {
					return
				}
			}
		}
	}

	// 2. badcharkey: keyboard-neighbor substitutions (penalty 1).
	subBuf := append([]rune(nil), runes...)
	for i, r := range runes {
		for _, repl := range qwertyNeighborsForRune(r) {
			copy(subBuf, runes)
			subBuf[i] = preserveRuneCase(r, repl)
			if !emit(string(subBuf), 1) {
				return
			}
		}
	}

	// 3. extrachar: deletions (penalty 2).
	// Hunspell ranks extrachar (#7) above forgotchar (#8) and badchar (#10),
	// so deletions are more likely to be the intended correction than insertions
	// or generic substitutions.
	if len(runes) > 1 {
		deleteBuf := make([]rune, len(runes)-1)
		for i := range runes {
			copy(deleteBuf, runes[:i])
			copy(deleteBuf[i:], runes[i+1:])
			if !emit(string(deleteBuf), 2) {
				return
			}
		}
	}

	// 4. forgotchar: insertions — keyboard-biased (penalty 3) then generic alphabet (penalty 3).
	insertBuf := make([]rune, len(runes)+1)
	for i := 0; i <= len(runes); i++ {
		for _, repl := range qwertyInsertionNeighbors(runes, i) {
			candidate := insertRuneInto(insertBuf, runes, i, repl)
			if !emit(candidate, 3) {
				return
			}
		}
		for _, repl := range s.replacementRunes() {
			candidate := insertRuneInto(insertBuf, runes, i, preserveInsertionCase(runes, i, repl))
			if !emit(candidate, 3) {
				return
			}
		}
	}

	// 5. badchar: generic alphabet substitutions (penalty 4).
	for i, r := range runes {
		for _, repl := range s.replacementRunes() {
			if unicode.ToLower(r) == repl {
				continue
			}
			copy(subBuf, runes)
			subBuf[i] = preserveRuneCase(r, repl)
			if !emit(string(subBuf), 4) {
				return
			}
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

// ngramRootScorePrepared scores a root candidate against a query using the same
// formula as hunspell's root-selection phase: sum of 1-gram, 2-gram, and 3-gram
// overlaps plus the length of the left-common substring. This mirrors hunspell's
// ngram(3, query, candidate, NGRAM_LONGER_WORSE) + leftcommonsubstring call.
//
// The 1-gram term counts query positions where the character appears anywhere in
// the root, giving anagram-like misspellings ("abcense"→"absence") a fair score
// even when no trigrams align. The length-difference penalty matches NGRAM_LONGER_WORSE.
func ngramRootScorePrepared(queryRunes []rune, queryBigrams, queryTrigrams []uint64, root mutationNGramRoot) int {
	if absIntLocal(len(queryRunes)-len(root.runes)) > 5 {
		return 0
	}
	// j=1: count query positions whose rune appears anywhere in the root.
	uniScore := 0
	for _, qr := range queryRunes {
		for _, rr := range root.runes {
			if qr == rr {
				uniScore++
				break
			}
		}
	}
	// j=2 and j=3: n-gram overlap using precomputed sorted hash arrays.
	biScore := ngramOverlapHashes(queryBigrams, root.bigrams)
	triScore := ngramOverlapHashes(queryTrigrams, root.trigrams)

	// NGRAM_LONGER_WORSE: penalise candidates longer than the query.
	penalty := len(root.runes) - len(queryRunes) - 2
	if penalty < 0 {
		penalty = 0
	}
	return uniScore + biScore + triScore + leftCommonRunes(queryRunes, root.runes) - penalty
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

// osaDistance computes the Optimal String Alignment distance between a and b.
// Unlike standard Levenshtein, adjacent transpositions cost 1, so
// osaDistance("thier", "their") == 1 instead of 2.
func osaDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	la, lb := len(ar), len(br)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// d[i][j] = OSA distance between ar[:i] and br[:j]
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 1; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			d[i][j] = d[i-1][j-1] + cost
			if d[i-1][j]+1 < d[i][j] {
				d[i][j] = d[i-1][j] + 1
			}
			if d[i][j-1]+1 < d[i][j] {
				d[i][j] = d[i][j-1] + 1
			}
			if i > 1 && j > 1 && ar[i-1] == br[j-2] && ar[i-2] == br[j-1] {
				if d[i-2][j-2]+1 < d[i][j] {
					d[i][j] = d[i-2][j-2] + 1
				}
			}
		}
	}
	return d[la][lb]
}

func hashRunes(runes []rune) uint64 {
	var h uint64 = 1469598103934665603
	for _, r := range runes {
		h ^= uint64(r)
		h *= 1099511628211
	}
	return h
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
