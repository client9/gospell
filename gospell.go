package gospell

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var numericTokenRegexp = regexp.MustCompile("^([0-9]+[.,-]?)+$")

type iconvRule struct {
	old string
	new string
}

// GoSpell is main struct
type GoSpell struct {
	dict                map[string]struct{}
	surfaces            map[string][]surfaceEntry
	wordFlags           map[string]map[string]struct{}
	wordEntryCount      map[string]int
	onlyCompoundCount   map[string]int
	compoundOnlyRoot    map[string]struct{}
	compoundOnly        map[string]struct{}
	compoundBegin       map[string]struct{}
	compoundMiddle      map[string]struct{}
	compoundEnd         map[string]struct{}
	compoundForbidden   map[string]struct{}
	forceUcaseWords     map[string]struct{}
	compoundBeginFlag   string
	compoundMiddleFlag  string
	compoundEndFlag     string
	compoundOnlyFlag    string
	compoundPatterns    []compoundPatternRule
	blockedCompound     map[string]struct{}
	compoundMin         int
	maxWordLen          int
	flagMode            flagMode
	iconvRules          []iconvRule
	compounds           []*regexp.Regexp
	suggester           Suggestions
	checkCompoundCase   bool
	checkCompoundDup    bool
	checkCompoundTriple bool
	simplifiedTriple    bool
	checkCompoundRep    bool
	repReplacements     [][2]string
	breakEnabled        bool
}

// InputConversion does any character substitution before checking
// based on the ICONV stanza in the AFF file.
func (s *GoSpell) InputConversion(raw []byte) string {
	sraw := string(raw)
	if len(s.iconvRules) == 0 {
		return sraw
	}
	var b strings.Builder
	for i := 0; i < len(sraw); {
		best := -1
		bestLen := 0
		for idx, rule := range s.iconvRules {
			if strings.HasPrefix(sraw[i:], rule.old) && len(rule.old) > bestLen {
				best = idx
				bestLen = len(rule.old)
			}
		}
		if best >= 0 {
			b.WriteString(s.iconvRules[best].new)
			i += len(s.iconvRules[best].old)
			continue
		}
		r, size := utf8.DecodeRuneInString(sraw[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// AddWordRaw adds a single word to the internal dictionary without modifications.
// Returns true if added, false if already present.
func (s *GoSpell) AddWordRaw(word string) bool {
	if _, ok := s.dict[word]; ok {
		return false
	}
	s.dict[word] = struct{}{}
	if s.surfaces == nil {
		s.surfaces = make(map[string][]surfaceEntry)
	}
	s.surfaces[word] = append(s.surfaces[word], surfaceEntry{
		Word:              word,
		StandaloneAllowed: true,
	})
	if strings.ContainsRune(word, ' ') {
		if s.blockedCompound == nil {
			s.blockedCompound = make(map[string]struct{})
		}
		s.blockedCompound[strings.ReplaceAll(word, " ", "")] = struct{}{}
	}
	if len(word) > s.maxWordLen {
		s.maxWordLen = len(word)
	}
	return true
}

// ForEachWord calls fn for every word in the dictionary.
// Iteration stops early if fn returns false.
func (s *GoSpell) ForEachWord(fn func(word string) bool) {
	for word := range s.dict {
		if !fn(word) {
			return
		}
	}
}

// HasWord reports whether word exists in the dictionary as an exact entry.
func (s *GoSpell) HasWord(word string) bool {
	_, ok := s.dict[word]
	return ok
}

// WordCount returns the number of dictionary entries currently loaded.
func (s *GoSpell) WordCount() int {
	return len(s.dict)
}

// MaxWordLen returns the longest loaded dictionary word length.
func (s *GoSpell) MaxWordLen() int {
	return s.maxWordLen
}

// SetSuggester configures the suggestion engine and initializes it from the
// current dictionary contents.
func (s *GoSpell) SetSuggester(suggester Suggestions) error {
	if suggester == nil {
		s.suggester = nil
		return nil
	}
	if err := suggester.Init(s); err != nil {
		return err
	}
	s.suggester = suggester
	return nil
}

// Suggest returns spelling suggestions using the configured suggester.
func (s *GoSpell) Suggest(word string, limit int) ([]Suggestion, error) {
	if s.suggester == nil {
		return nil, fmt.Errorf("no suggester configured")
	}
	return s.suggester.Suggest(word, limit)
}

// AddWordListFile reads a word list file, one word per line.
func (s *GoSpell) AddWordListFile(name string) ([]string, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fd.Close() }()
	return s.AddWordList(fd)
}

// AddWordList adds words from a reader, one word per line (UTF-8).
// Returns a list of duplicate words and any read error.
func (s *GoSpell) AddWordList(r io.Reader) ([]string, error) {
	var duplicates []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line == "#" {
			continue
		}
		for _, word := range caseVariations(line, caseStyle(line)) {
			if !s.AddWordRaw(word) {
				duplicates = append(duplicates, word)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return duplicates, err
	}
	return duplicates, nil
}

// isWordForbidden returns true when the word is blocked by FORBIDDENWORD.
// A non-forbidden root dic entry (IsRoot && !ForbiddenWord) acts as a homonym
// and rescues the word.  Derived forms (affix expansions) do not rescue a word
// that has an explicit FORBIDDENWORD dic entry.
func (s *GoSpell) isWordForbidden(word string) bool {
	entries := s.surfaces[word]
	if len(entries) == 0 {
		return false
	}
	hasForbidden := false
	for _, entry := range entries {
		if entry.ForbiddenWord {
			hasForbidden = true
		} else if entry.IsRoot {
			return false
		}
	}
	return hasForbidden
}

// Spell reports whether word is correctly spelled.
func (s *GoSpell) Spell(word string) bool {
	word = s.InputConversion([]byte(word))
	// FORBIDDENWORD entries must block the word even when a case-folded form
	// would otherwise be accepted (e.g. "Kg" forbidden while "kg" is valid).
	if s.isWordForbidden(word) {
		return false
	}
	if s.spellExact(word) {
		return true
	}

	// Case-folded fallbacks, matching hunspell semantics.
	// Words are stored in their dic-file form (one entry each), so we try
	// progressively simpler case forms only when an exact match fails:
	//
	//   titleCase query "Junkyard" → try lowercase "junkyard"
	//   allUpper  query "JUNKYARD" → try lowercase "junkyard",
	//                                then title "Junkyard" (for proper nouns like "LONDON")
	//
	// allLower and mixedCase queries get no fallback — hunspell rejects
	// e.g. "london" even when "London" is in the dictionary.
	switch caseStyle(word) {
	case titleCase:
		return s.spellExact(strings.ToLower(word))
	case allUpper:
		lower := strings.ToLower(word)
		if s.spellExact(lower) {
			return true
		}
		// title form: uppercase the first byte, lowercase the rest
		if len(lower) > 0 {
			return s.spellExact(strings.ToUpper(lower[:1]) + lower[1:])
		}
	}
	return false
}

func (s *GoSpell) spellExact(word string) bool {
	if ok, found := s.surfaceAllowsStandalone(word); found {
		return ok
	}
	if _, ok := s.dict[word]; ok {
		return true
	}
	if numericTokenRegexp.MatchString(word) {
		return true
	}
	for _, pat := range s.compounds {
		if pat.MatchString(word) {
			return true
		}
	}
	if s.blockedCompound != nil {
		if _, ok := s.blockedCompound[word]; ok {
			return false
		}
	}
	if s.spellCompound(word) {
		return true
	}
	if s.spellSandhiCompound(word) {
		return true
	}
	if s.simplifiedTriple && s.spellSimplifiedTriple(word) {
		return true
	}
	// Default BREAK behavior: split at hyphens and accept if each part is valid.
	if s.breakEnabled && strings.ContainsRune(word, '-') {
		parts := strings.Split(word, "-")
		allOK := true
		for _, p := range parts {
			if p == "" {
				continue
			}
			if !s.spellExact(p) {
				allOK = false
				break
			}
		}
		if allOK {
			return true
		}
	}
	return false
}

func (s *GoSpell) surfaceAllowsStandalone(word string) (bool, bool) {
	if len(s.surfaces) == 0 {
		return false, false
	}
	entries, ok := s.surfaces[word]
	if !ok {
		return false, false
	}
	for _, entry := range entries {
		if entry.allowsStandalone() {
			return true, true
		}
	}
	return false, true
}

func compileCompoundRulePattern(rule string, groups map[string][]string, mode flagMode) (string, error) {
	var b strings.Builder
	b.WriteString("^")
	switch mode {
	case flagLong:
		for i := 0; i < len(rule); {
			switch rule[i] {
			case '(', ')', '+', '?', '*':
				b.WriteByte(rule[i])
				i++
			default:
				if i+1 >= len(rule) {
					return "", fmt.Errorf("compound rule %q has truncated long flag", rule)
				}
				token := rule[i : i+2]
				b.WriteString("(")
				b.WriteString(strings.Join(groups[token], "|"))
				b.WriteString(")")
				i += 2
			}
		}
	case flagNum:
		for i := 0; i < len(rule); {
			switch rule[i] {
			case '(', ')', '+', '?', '*':
				b.WriteByte(rule[i])
				i++
			default:
				j := i
				for j < len(rule) {
					switch rule[j] {
					case '(', ')', '+', '?', '*':
						goto numTokenDone
					default:
						j++
					}
				}
			numTokenDone:
				token := rule[i:j]
				b.WriteString("(")
				b.WriteString(strings.Join(groups[token], "|"))
				b.WriteString(")")
				i = j
			}
		}
	default:
		for _, r := range rule {
			switch r {
			case '(', ')', '+', '?', '*':
				b.WriteRune(r)
			default:
				token := string(r)
				b.WriteString("(")
				b.WriteString(strings.Join(groups[token], "|"))
				b.WriteString(")")
			}
		}
	}
	b.WriteString("$")
	return b.String(), nil
}

func (s *GoSpell) spellCompound(word string) bool {
	runes := []rune(word)
	if len(runes) < 2*s.compoundMin {
		return false
	}
	parts, ok := s.spellCompoundParts(runes, caseStyle(word), true)
	if !ok {
		return false
	}
	if len(parts) >= 3 && caseStyle(word) == allLower && s.compoundTypoMatchesDict(word) {
		return false
	}
	if s.compoundPatternBlocks(parts) {
		return false
	}
	if s.checkCompoundRep && s.repBlocksCompoundParts(parts) {
		return false
	}
	return true
}

// spellSandhiCompound checks whether word is the modified (sandhi) form of a
// compound governed by a 3-argument CHECKCOMPOUNDPATTERN rule.
//
// Rule: CHECKCOMPOUNDPATTERN endchars[/leftFlag] beginchars[/rightFlag] mod
//
// The sandhi form is produced by replacing the junction:
//
//	left_base + mod + right_base
//	  where left = left_base + endchars, right = beginchars + right_base
//
// We reverse-engineer this: find every occurrence of mod in word, reconstruct
// the candidate left and right, verify flags and compound eligibility.
func (s *GoSpell) spellSandhiCompound(word string) bool {
	if len(s.compoundPatterns) == 0 {
		return false
	}
	wholeStyle := caseStyle(word)
	wordRunes := []rune(word)
	for _, rule := range s.compoundPatterns {
		if rule.mod == "" {
			continue
		}
		modRunes := []rune(rule.mod)
		leftEnd := []rune(rule.left)
		rightStart := []rune(rule.right)
		modLen := len(modRunes)
		// Scan every position in word where mod could sit.
		for i := 0; i <= len(wordRunes)-modLen; i++ {
			// Check mod matches at position i.
			match := true
			for j, r := range modRunes {
				if wordRunes[i+j] != r {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			// Reconstruct left = word[:i] + endchars, right = beginchars + word[i+modLen:]
			leftRunes := append(append([]rune(nil), wordRunes[:i]...), leftEnd...)
			rightRunes := append(append([]rune(nil), rightStart...), wordRunes[i+modLen:]...)
			left := string(leftRunes)
			right := string(rightRunes)
			if compoundRuneLen(left) < s.compoundMin || compoundRuneLen(right) < s.compoundMin {
				continue
			}
			// Verify flag constraints from the rule.
			if rule.leftFlag != "" && !s.wordHasFlag(left, rule.leftFlag) {
				continue
			}
			if rule.rightFlag != "" && !s.wordHasFlag(right, rule.rightFlag) {
				continue
			}
			if s.compoundStartPart(left) && s.compoundFinalPart(right, wholeStyle) {
				return true
			}
		}
	}
	return false
}

func (s *GoSpell) spellCompoundParts(runes []rune, wholeStyle wordCase, first bool) ([]string, bool) {
	if len(runes) < s.compoundMin {
		return nil, false
	}
	for i := s.compoundMin; i <= len(runes)-s.compoundMin; i++ {
		prefix := string(runes[:i])
		suffix := string(runes[i:])
		if first {
			if !s.compoundStartPart(prefix) {
				continue
			}
		} else if !s.compoundMiddlePart(prefix) {
			continue
		}
		// Per-boundary checks: compare prefix against the raw suffix string
		// (not its decomposed parts) so that CHECKCOMPOUNDDUP and CHECKCOMPOUNDCASE
		// apply at each recursion level independently.
		if s.checkCompoundDup && prefix == suffix {
			continue
		}
		if s.checkCompoundCase && compoundBoundaryHasCase(prefix, suffix) {
			continue
		}
		if s.checkCompoundTriple && compoundBoundaryHasTriple(prefix, suffix) {
			continue
		}
		if s.compoundFinalPart(suffix, wholeStyle) {
			return []string{prefix, suffix}, true
		}
		if rest, ok := s.spellCompoundParts([]rune(suffix), wholeStyle, false); ok {
			return append([]string{prefix}, rest...), true
		}
	}
	return nil, false
}

func (s *GoSpell) compoundStartPart(word string) bool {
	if compoundRuneLen(word) < s.compoundMin {
		return false
	}
	if ok, found := s.surfaceAllowsCompound(word, compoundPositionStart); found {
		return ok
	}
	return s.compoundSetContains(s.compoundBegin, word) ||
		s.compoundSetContains(s.compoundOnly, word) ||
		s.wordHasFlag(word, s.compoundBeginFlag)
}

func (s *GoSpell) compoundMiddlePart(word string) bool {
	if compoundRuneLen(word) < s.compoundMin {
		return false
	}
	if ok, found := s.surfaceAllowsCompound(word, compoundPositionMiddle); found {
		return ok
	}
	return s.compoundSetContains(s.compoundMiddle, word) ||
		s.compoundSetContains(s.compoundOnly, word) ||
		s.wordHasFlag(word, s.compoundMiddleFlag)
}

func (s *GoSpell) compoundFinalPart(word string, wholeStyle wordCase) bool {
	if compoundRuneLen(word) < s.compoundMin {
		return false
	}
	if ok, found := s.surfaceAllowsCompound(word, compoundPositionEnd); found {
		if !ok {
			return false
		}
		if s.forceUcaseWords != nil {
			if _, ok := s.forceUcaseWords[word]; ok && wholeStyle == allLower {
				return false
			}
		}
		return true
	}
	if s.forceUcaseWords != nil {
		if _, ok := s.forceUcaseWords[word]; ok && wholeStyle == allLower {
			return false
		}
	}
	return s.compoundSetContains(s.compoundEnd, word) ||
		(s.compoundOnlyRoot != nil && func() bool {
			_, ok := s.compoundOnlyRoot[word]
			return ok
		}()) ||
		s.wordHasFlag(word, s.compoundEndFlag)
}

func (s *GoSpell) wordHasFlag(word, flag string) bool {
	if flag == "" {
		return false
	}
	flags := s.wordFlags[word]
	if len(flags) == 0 {
		return false
	}
	_, ok := flags[flag]
	return ok
}

// wordOrRootHasFlag checks whether word has flag either via its own wordFlags
// (root dic entries, state==0) or via the RawFlags carried in its surface
// entries (which record the original dic entry's flags for derived forms).
func (s *GoSpell) wordOrRootHasFlag(word, flag string) bool {
	if flag == "" {
		return false
	}
	if s.wordHasFlag(word, flag) {
		return true
	}
	for _, entry := range s.surfaces[word] {
		for _, f := range entry.RawFlags {
			if f == flag {
				return true
			}
		}
	}
	return false
}

func (s *GoSpell) compoundPatternBlocks(parts []string) bool {
	if len(parts) < 2 || len(s.compoundPatterns) == 0 {
		return false
	}
	for i := 0; i+1 < len(parts); i++ {
		left := parts[i]
		right := parts[i+1]
		for _, rule := range s.compoundPatterns {
			if rule.leftFlag != "" {
				if rule.leftStemOnly {
					if !s.wordHasFlag(left, rule.leftFlag) {
						continue
					}
				} else {
					if !s.wordOrRootHasFlag(left, rule.leftFlag) {
						continue
					}
				}
			}
			if rule.rightFlag != "" {
				if rule.rightStemOnly {
					if !s.wordHasFlag(right, rule.rightFlag) {
						continue
					}
				} else {
					if !s.wordOrRootHasFlag(right, rule.rightFlag) {
						continue
					}
				}
			}
			if rule.left != "" && !strings.HasSuffix(left, rule.left) {
				continue
			}
			if rule.right != "" && !strings.HasPrefix(right, rule.right) {
				continue
			}
			return true
		}
	}
	return false
}

func (s *GoSpell) compoundSetContains(set map[string]struct{}, word string) bool {
	if len(set) == 0 {
		return false
	}
	if _, ok := set[word]; ok {
		return true
	}
	lower := strings.ToLower(word)
	if lower != word {
		if _, ok := set[lower]; ok {
			return true
		}
	}
	return false
}

// compoundBoundaryHasCase reports whether the left|right compound boundary
// violates CHECKCOMPOUNDCASE.  The check fires only when both boundary
// characters are letters; a non-letter (e.g. '-') resets the boundary so
// hyphen-connected forms like "BAZ-foo" are not blocked.
func compoundBoundaryHasCase(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	lastR, _ := utf8.DecodeLastRuneInString(left)
	firstR, _ := utf8.DecodeRuneInString(right)
	if !unicode.IsLetter(lastR) || !unicode.IsLetter(firstR) {
		return false
	}
	return unicode.IsUpper(lastR) || unicode.IsUpper(firstR)
}

// compoundBoundaryHasTriple reports whether the left|right boundary creates a
// run of three identical characters (CHECKCOMPOUNDTRIPLE).
func compoundBoundaryHasTriple(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftR := []rune(left)
	rightR := []rune(right)
	last := leftR[len(leftR)-1]
	first := rightR[0]
	if last != first {
		return false
	}
	if len(leftR) >= 2 && leftR[len(leftR)-2] == last {
		return true
	}
	if len(rightR) >= 2 && rightR[1] == first {
		return true
	}
	return false
}

// repBlocksCompoundParts is called with the parts list found by spellCompoundParts
// and applies CHECKCOMPOUNDREP on each adjacent pair concatenation.
func (s *GoSpell) repBlocksCompoundParts(parts []string) bool {
	if len(s.repReplacements) == 0 || len(parts) < 2 {
		return false
	}
	for i := 0; i+1 < len(parts); i++ {
		pair := parts[i] + parts[i+1]
		if s.repMatchesDict(pair) {
			return true
		}
	}
	return false
}

// repMatchesDict applies all REP rules at every rune position in word and
// returns true if any result is a standalone dictionary word.
func (s *GoSpell) repMatchesDict(word string) bool {
	runes := []rune(word)
	for _, rep := range s.repReplacements {
		from := []rune(rep[0])
		to := []rune(rep[1])
		fromLen := len(from)
		if fromLen == 0 {
			continue
		}
		for i := 0; i <= len(runes)-fromLen; i++ {
			match := true
			for j, r := range from {
				if runes[i+j] != r {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			result := make([]rune, 0, len(runes)-fromLen+len(to))
			result = append(result, runes[:i]...)
			result = append(result, to...)
			result = append(result, runes[i+fromLen:]...)
			if s.isStandaloneWord(string(result)) {
				return true
			}
		}
	}
	return false
}

// isStandaloneWord checks whether word is in the dictionary as a standalone
// (non-compound) entry, without recursing into compound checking.
func (s *GoSpell) isStandaloneWord(word string) bool {
	if ok, found := s.surfaceAllowsStandalone(word); found {
		return ok
	}
	_, ok := s.dict[word]
	return ok
}

// spellSimplifiedTriple accepts a word that is the simplified form of a
// CHECKCOMPOUNDTRIPLE-blocked compound (SIMPLIFIEDTRIPLE).  It tries inserting
// one copy of each character at every position and checks whether the extended
// form is a valid compound that would otherwise be blocked by the triple rule.
func (s *GoSpell) spellSimplifiedTriple(word string) bool {
	if !s.checkCompoundTriple {
		return false
	}
	runes := []rune(word)
	s.checkCompoundTriple = false
	defer func() { s.checkCompoundTriple = true }()
	for i := 1; i <= len(runes); i++ {
		c := runes[i-1]
		extended := make([]rune, len(runes)+1)
		copy(extended[:i], runes[:i])
		extended[i] = c
		copy(extended[i+1:], runes[i:])
		if s.spellCompound(string(extended)) {
			return true
		}
	}
	return false
}

func (s *GoSpell) compoundTypoMatchesDict(word string) bool {
	for dictWord := range s.dict {
		if compoundRuneLen(dictWord) != compoundRuneLen(word) {
			continue
		}
		if oneEditAway(word, dictWord) {
			return true
		}
	}
	return false
}

func oneEditAway(a, b string) bool {
	ar := []rune(a)
	br := []rune(b)
	if absIntLocal(len(ar)-len(br)) > 1 {
		return false
	}
	if len(ar) == len(br) {
		diff := 0
		for i := range ar {
			if ar[i] != br[i] {
				diff++
				if diff > 1 {
					return false
				}
			}
		}
		return diff == 1
	}
	if len(ar) > len(br) {
		ar, br = br, ar
	}
	i, j, edits := 0, 0, 0
	for i < len(ar) && j < len(br) {
		if ar[i] == br[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		j++
	}
	return true
}

func compoundRuneLen(word string) int {
	return utf8.RuneCountInString(word)
}

// insertWord stores word in dict using its canonical (dic-file) form.
// Case variants are NOT pre-generated; Spell's fallback logic handles them at
// lookup time, matching hunspell's behaviour:
//   - allLower "junkyard"  → accepts junkyard / Junkyard / JUNKYARD
//   - titleCase "London"   → accepts London / LONDON  (rejects london)
//   - allUpper "NASA"      → accepts NASA only
//   - mixedCase "McDonald" → accepts McDonald / MCDONALD
//
// mixedCase is the one exception: its all-caps form is stored explicitly because
// ToLower("MCDONALD") = "mcdonald", which can't be mapped back to "McDonald"
// without the original form.
func insertWord(dict map[string]struct{}, word string) {
	dict[word] = struct{}{}
	if caseStyle(word) == mixedCase {
		dict[strings.ToUpper(word)] = struct{}{}
	}
}

func (s *GoSpell) surfaceAllowsCompound(word string, pos compoundPosition) (bool, bool) {
	if len(s.surfaces) == 0 {
		return false, false
	}
	entries, ok := s.surfaces[word]
	if !ok {
		return false, false
	}
	// An explicit CompoundForbidden entry blocks compound use regardless of
	// other surface forms for the same spelling.
	for _, entry := range entries {
		if entry.CompoundForbidden {
			return false, true
		}
	}
	for _, entry := range entries {
		if entry.allowsCompound(pos) {
			return true, true
		}
	}
	return false, true
}

func compoundEntryFlags(line string, affix *dictConfig) []string {
	idx := strings.Index(line, "/")
	if idx == -1 {
		return nil
	}
	_, flags, found := strings.Cut(line[idx+1:], "/")
	if !found {
		flags = line[idx+1:]
	}
	flags = affix.normalizeFlags(flags)
	tokens, err := affix.splitFlags(flags)
	if err != nil {
		return nil
	}
	return tokens
}

func hasFlagToken(flags []string, want string) bool {
	if want == "" {
		return false
	}
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}
	return false
}

func buildSurfaceEntry(word string, rawFlags []string, affix *dictConfig, rec expandedWord) surfaceEntry {
	entry := surfaceEntry{
		Word:              word,
		StandaloneAllowed: !rec.needsAffix,
		IsRoot:            rec.state == 0,
		RawFlags:          append([]string(nil), rawFlags...),
	}
	if affix == nil {
		return entry
	}
	if (rec.forbid && rec.state == 0) || hasFlagToken(rawFlags, affix.CompoundForbidFlag) {
		entry.CompoundForbidden = true
	}
	if affix.ForbiddenWordFlag != "" && hasFlagToken(rawFlags, affix.ForbiddenWordFlag) {
		entry.StandaloneAllowed = false
		entry.CompoundForbidden = true
		entry.ForbiddenWord = true
	}
	if rec.compoundOnly || hasFlagToken(rawFlags, affix.CompoundOnly) {
		entry.OnlyInCompound = true
		entry.StandaloneAllowed = false
		entry.CompoundStartAllowed = true
		entry.CompoundMiddleAllowed = true
		entry.CompoundEndAllowed = false
	}
	if rec.mask&compoundBegin != 0 || hasFlagToken(rawFlags, affix.CompoundBeginFlag) {
		entry.CompoundStartAllowed = true
	}
	if rec.mask&compoundMiddle != 0 || hasFlagToken(rawFlags, affix.CompoundMiddleFlag) {
		entry.CompoundMiddleAllowed = true
	}
	if rec.mask&compoundEnd != 0 || hasFlagToken(rawFlags, affix.CompoundEndFlag) {
		entry.CompoundEndAllowed = true
	}
	if entry.CompoundForbidden {
		// Keep the surface record for diagnostics, but let compound position
		// checks decide using the positional metadata.
	}
	return entry
}

// NewGoSpellReader creates a GoSpell from io.Readers for Hunspell AFF and DIC data.
func NewGoSpellReader(aff, dic io.Reader) (*GoSpell, error) {
	affBytes, err := io.ReadAll(aff)
	if err != nil {
		return nil, fmt.Errorf("read aff: %w", err)
	}
	dicBytes, err := io.ReadAll(dic)
	if err != nil {
		return nil, fmt.Errorf("read dic: %w", err)
	}

	set, err := detectSET(affBytes)
	if err != nil {
		return nil, err
	}
	if set == "" {
		set = "ISO8859-1"
	}
	enc, ok := encodingForSET(set)
	if !ok {
		return nil, fmt.Errorf("SET had non-UTF-8 character encoding of %q -- not supported", set)
	}
	affBytes, err = decodeWithEncoding(enc, affBytes)
	if err != nil {
		return nil, fmt.Errorf("decode aff: %w", err)
	}
	dicBytes, err = decodeWithEncoding(enc, dicBytes)
	if err != nil {
		return nil, fmt.Errorf("decode dic: %w", err)
	}
	affBytes = bytes.TrimPrefix(affBytes, []byte("\uFEFF"))

	affix, err := newDictConfig(bytes.NewReader(affBytes))
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(dicBytes))
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("dic file is empty")
	}
	line := strings.TrimSpace(scanner.Text())
	line = strings.TrimPrefix(line, "\uFEFF")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, fmt.Errorf("dic file header is empty")
	}
	_, err = strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, err
	}

	gs := GoSpell{
		dict:                make(map[string]struct{}),
		surfaces:            make(map[string][]surfaceEntry),
		wordFlags:           make(map[string]map[string]struct{}),
		wordEntryCount:      make(map[string]int),
		onlyCompoundCount:   make(map[string]int),
		compoundOnlyRoot:    make(map[string]struct{}),
		compoundOnly:        affix.compoundOnlyWords,
		compoundBegin:       affix.compoundBeginWords,
		compoundMiddle:      affix.compoundMiddleWords,
		compoundEnd:         affix.compoundEndWords,
		forceUcaseWords:     affix.forceUcaseWords,
		compoundBeginFlag:   affix.CompoundBeginFlag,
		compoundMiddleFlag:  affix.CompoundMiddleFlag,
		compoundEndFlag:     affix.CompoundEndFlag,
		compoundOnlyFlag:    affix.CompoundOnly,
		compoundPatterns:    affix.checkCompoundPatterns,
		compoundMin:         affix.CompoundMin,
		flagMode:            affix.flagMode,
		compounds:           make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		checkCompoundCase:   affix.CheckCompoundCase,
		checkCompoundDup:    affix.CheckCompoundDup,
		checkCompoundTriple: affix.CheckCompoundTriple,
		simplifiedTriple:    affix.SimplifiedTriple,
		checkCompoundRep:    affix.CheckCompoundRep,
		repReplacements:     affix.Replacements,
		breakEnabled:        affix.BreakEnabled,
	}

	words := []expandedWord{}
	for scanner.Scan() {
		line := scanner.Text()
		rawFlags := compoundEntryFlags(line, affix)
		compoundOnlyEntry := false
		for _, flag := range rawFlags {
			if flag == affix.CompoundOnly {
				compoundOnlyEntry = true
				break
			}
		}
		expanded, err := affix.expandRecords(line)
		if err != nil {
			return nil, fmt.Errorf("unable to process %q: %s", line, err)
		}
		words = expanded
		base := line
		if idx := strings.Index(base, "/"); idx >= 0 {
			base = base[:idx]
		}
		base = strings.TrimSpace(base)
		// wordFlags stores the root dic-entry flags for the base word.
		// expandStateRecords merges same-spelling records (OR-ing state), so we
		// cannot rely on rec.state==0 to identify the root entry.  Instead, store
		// rawFlags (the DIC line's flags) directly for the base word.  Derived
		// forms (SFX/PFX) get their own surface entries and never appear as base.
		if base != "" && len(rawFlags) > 0 {
			if gs.wordFlags[base] == nil {
				gs.wordFlags[base] = make(map[string]struct{}, len(rawFlags))
			}
			for _, flag := range rawFlags {
				gs.wordFlags[base][flag] = struct{}{}
			}
		}
		seen := make(map[string]struct{}, len(words))
		for _, rec := range words {
			word := rec.word
			if _, ok := seen[word]; ok {
				continue
			}
			seen[word] = struct{}{}
			insertWord(gs.dict, word)
			gs.surfaces[word] = append(gs.surfaces[word], buildSurfaceEntry(word, rawFlags, affix, rec))
			gs.wordEntryCount[word]++
			if strings.ContainsRune(word, ' ') {
				if gs.blockedCompound == nil {
					gs.blockedCompound = make(map[string]struct{})
				}
				gs.blockedCompound[strings.ReplaceAll(word, " ", "")] = struct{}{}
			}
			if len(word) > gs.maxWordLen {
				gs.maxWordLen = len(word)
			}
		}
		if compoundOnlyEntry {
			if base != "" {
				gs.onlyCompoundCount[base]++
				gs.compoundOnlyRoot[base] = struct{}{}
			}
			for _, rec := range words {
				gs.compoundOnlyRoot[rec.word] = struct{}{}
			}
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, compoundRule := range affix.CompoundRule {
		pattern, err := compileCompoundRulePattern(compoundRule, affix.compoundMap, affix.flagMode)
		if err != nil {
			return nil, err
		}
		pat, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("unable to compile compound rule %q: %w", pattern, err)
		}
		gs.compounds = append(gs.compounds, pat)
	}

	if len(affix.IconvReplacements) > 0 {
		gs.iconvRules = make([]iconvRule, 0, len(affix.IconvReplacements)/2)
		for i := 0; i+1 < len(affix.IconvReplacements); i += 2 {
			gs.iconvRules = append(gs.iconvRules, iconvRule{
				old: affix.IconvReplacements[i],
				new: affix.IconvReplacements[i+1],
			})
		}
		sort.SliceStable(gs.iconvRules, func(i, j int) bool {
			if len(gs.iconvRules[i].old) == len(gs.iconvRules[j].old) {
				return i < j
			}
			return len(gs.iconvRules[i].old) > len(gs.iconvRules[j].old)
		})
	}
	return &gs, nil
}

// NewGoSpell creates a GoSpell from Hunspell AFF and DIC file paths.
func NewGoSpell(affFile, dicFile string) (*GoSpell, error) {
	aff, err := os.Open(affFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open aff: %s", err)
	}
	defer func() { _ = aff.Close() }()
	dic, err := os.Open(dicFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open dic: %s", err)
	}
	defer func() { _ = dic.Close() }()
	return NewGoSpellReader(aff, dic)
}
