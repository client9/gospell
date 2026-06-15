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
	suggester Suggestions
	dict      map[string]struct{}
	// dictByRuneLen groups every dict key by its rune count. It is built once
	// at load time and used by compoundTypoMatchesDict to avoid a full dict
	// scan: only words with the same rune count can be one edit away, so the
	// index shrinks the candidate set by roughly 1/maxWordLen on average.
	dictByRuneLen       map[int][]string
	surfaces            map[string][]surfaceEntry
	wordFlags           map[string]map[string]struct{}
	compoundOnly        map[string]struct{}
	compoundBegin       map[string]struct{}
	compoundMiddle      map[string]struct{}
	compoundEnd         map[string]struct{}
	forceUcaseWords     map[string]struct{}
	blockedCompound     map[string]struct{}
	affix               *dictConfig
	entriesByRoot       map[string][]dictionaryEntry
	lazySurfaceChecked  map[string]struct{}
	compoundBeginFlag   string
	compoundMiddleFlag  string
	compoundEndFlag     string
	compoundOnlyFlag    string
	compoundPatterns    []compoundPatternRule
	iconvRules          []iconvRule
	compounds           []*regexp.Regexp
	repReplacements     [][2]string
	breakPatterns       []string
	compoundMin         int
	maxWordLen          int
	flagMode            flagMode
	checkCompoundCase   bool
	checkCompoundDup    bool
	checkCompoundTriple bool
	simplifiedTriple    bool
	checkCompoundRep    bool
}

type dictionaryEntry struct {
	line     string
	base     string
	rawFlags []string
}

// InputConversion does any character substitution before checking
// based on the ICONV stanza in the AFF file.
func (s *GoSpell) InputConversion(raw []byte) string {
	return s.inputConversionString(string(raw))
}

// inputConversionString is the string-input variant of InputConversion,
// used internally to avoid a string→[]byte→string round-trip.
func (s *GoSpell) inputConversionString(word string) string {
	if len(s.iconvRules) == 0 {
		return word
	}
	var b strings.Builder
	for i := 0; i < len(word); {
		best := -1
		bestLen := 0
		for idx, rule := range s.iconvRules {
			if strings.HasPrefix(word[i:], rule.old) && len(rule.old) > bestLen {
				best = idx
				bestLen = len(rule.old)
			}
		}
		if best >= 0 {
			b.WriteString(s.iconvRules[best].new)
			i += len(s.iconvRules[best].old)
			continue
		}
		r, size := utf8.DecodeRuneInString(word[i:])
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
		IsRoot:            true,
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
	if s.dictByRuneLen != nil {
		// Keep the rune-length index in sync when words are added after load.
		rl := utf8.RuneCountInString(word)
		s.dictByRuneLen[rl] = append(s.dictByRuneLen[rl], word)
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

// isWordForbidden returns true when the word is blocked by FORBIDDENWORD.
// A non-forbidden root dic entry (IsRoot && !ForbiddenWord) acts as a homonym
// and rescues the word.  Derived forms (affix expansions) do not rescue a word
// that has an explicit FORBIDDENWORD dic entry.
func (s *GoSpell) isWordForbidden(word string) bool {
	s.ensureLazySurface(word)
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
	word = s.inputConversionString(word)
	return s.spellConverted(word)
}

// spellConverted is the inner spell check after ICONV has already been applied.
// Checker.Spell calls this directly so ICONV is not applied a second time.
func (s *GoSpell) spellConverted(word string) bool {
	// FORBIDDENWORD entries must block the word even when a case-folded form
	// would otherwise be accepted (e.g. "Kg" forbidden while "kg" is valid).
	if s.isWordForbidden(word) {
		return false
	}
	// Hunspell validates space-separated phrases by checking each token.
	if strings.ContainsRune(word, ' ') {
		parts := strings.Fields(word)
		for _, p := range parts {
			if !s.spellConverted(p) {
				return false
			}
		}
		return len(parts) > 0
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
		lower := strings.ToLower(word)
		// Only accept the lowercase form if its title-case reconstruction
		// matches the query. This rejects "İmply" when "imply" is in the dict
		// because ToTitle("imply") = "Imply" ≠ "İmply".
		r, size := utf8.DecodeRuneInString(lower)
		if string(unicode.ToUpper(r))+lower[size:] == word && s.spellExact(lower) {
			return true
		}
	case allUpper:
		lower := strings.ToLower(word)
		// Guard: only accept if ToUpper(lower)==word so "İMPLY" (wrong caps)
		// isn't accepted just because "imply" is in the dict.
		if strings.ToUpper(lower) == word && s.spellExact(lower) {
			return true
		}
		if idx := strings.LastIndexByte(word, '\''); idx > 0 && idx+1 < len(word) {
			possessive := word[:idx] + strings.ToLower(word[idx:])
			if possessive != word && s.spellExact(possessive) {
				return true
			}
			rest := strings.ToLower(word[idx+1:])
			if rest != "" {
				r, size := utf8.DecodeRuneInString(rest)
				prefixed := word[:idx+1] + string(unicode.ToUpper(r)) + rest[size:]
				if prefixed != word && s.spellExact(prefixed) {
					return true
				}
			}
		}
		// Title form: original first rune (already uppercase) + lowercase rest.
		// Using the original rune preserves diacritics like İ (U+0130) that
		// would be lost by round-tripping through ToLower then ToUpper.
		if _, size := utf8.DecodeRuneInString(word); size > 0 {
			if s.spellExact(word[:size] + strings.ToLower(word[size:])) {
				return true
			}
		}
	}

	// Hunspell strips one trailing period and retries (abbreviation handling).
	if strings.HasSuffix(word, ".") {
		return s.spellConverted(strings.TrimSuffix(word, "."))
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
	if s.blockedCompound != nil {
		if _, ok := s.blockedCompound[word]; ok {
			return false
		}
	}
	for _, pat := range s.compounds {
		if pat.MatchString(word) {
			return true
		}
	}
	if s.spellCompound(word, false) {
		return true
	}
	if s.spellSandhiCompound(word) {
		return true
	}
	if s.simplifiedTriple && s.spellSimplifiedTriple(word) {
		return true
	}
	if len(s.breakPatterns) > 0 && s.spellBreak(word, 0) {
		return true
	}
	return false
}

// spellBreak recursively tries all BREAK patterns against word.
// Middle patterns (e.g. "-") try every occurrence; "^X" strips a leading X;
// "X$" strips a trailing X.  Each resulting sub-word is checked with
// spellExact (which will recurse back into spellBreak as needed).
func (s *GoSpell) spellBreak(word string, depth int) bool {
	if depth > 10 {
		return false
	}
	for _, pat := range s.breakPatterns {
		switch {
		case strings.HasPrefix(pat, "^"):
			pfx := pat[1:]
			// Byte-length comparison is safe: HasPrefix guarantees pfx's bytes
			// exactly match word's start, so word[len(pfx):] is always at a
			// valid UTF-8 rune boundary.
			// Skip bare "^" (empty pfx): stripping nothing produces the same
			// word and causes infinite recursion.
			if len(pfx) > 0 && len(word) > len(pfx) && strings.HasPrefix(word, pfx) {
				rest := word[len(pfx):]
				if s.spellExact(rest) || s.spellBreak(rest, depth+1) {
					return true
				}
			}
		case strings.HasSuffix(pat, "$"):
			sfx := pat[:len(pat)-1]
			// Same reasoning as above: HasSuffix guarantees alignment.
			// Skip bare "$" (empty sfx): same infinite-recursion hazard as "^".
			if len(sfx) > 0 && len(word) > len(sfx) && strings.HasSuffix(word, sfx) {
				prefix := word[:len(word)-len(sfx)]
				if s.spellExact(prefix) || s.spellBreak(prefix, depth+1) {
					return true
				}
			}
		default:
			// Try every non-edge occurrence so e.g. "e-mail-foo" splits
			// at the second hyphen ("e-mail" | "foo") not just the first.
			offset := 0
			for offset < len(word) {
				idx := strings.Index(word[offset:], pat)
				if idx < 0 {
					break
				}
				absIdx := offset + idx
				rightStart := absIdx + len(pat)
				// Both sides must be non-empty.
				if absIdx == 0 || rightStart >= len(word) {
					offset = absIdx + len(pat)
					continue
				}
				left := word[:absIdx]
				right := word[rightStart:]
				if (s.spellExact(left) || s.spellBreak(left, depth+1)) &&
					(s.spellExact(right) || s.spellBreak(right, depth+1)) {
					return true
				}
				offset = absIdx + len(pat)
			}
		}
	}
	return false
}

// stripDicMorphFields removes hunspell morphological data fields from a raw
// .dic entry line.
//
// Modern hunspell dic files use a TAB to separate the word+flags from any
// morphological data (e.g. "drink/S\tpo:noun\tal:drank").  If a TAB is
// present, everything from the first TAB onward is dropped.
//
// Older / space-only files (e.g. "foo ph:bar ph:baz") are handled by
// scanning for the first field whose first three bytes match the
// two-lowercase-letter-plus-colon pattern used by morph tags (ph:, st:,
// po:, is:, …).  Remaining tokens are rejoined preserving multi-word
// entries like "foo bar".
func stripDicMorphFields(line string) string {
	if idx := strings.IndexByte(line, '\t'); idx >= 0 {
		return line[:idx]
	}
	fields := strings.Fields(line)
	for i, f := range fields {
		if len(f) >= 3 && f[0] >= 'a' && f[0] <= 'z' && f[1] >= 'a' && f[1] <= 'z' && f[2] == ':' {
			return strings.Join(fields[:i], " ")
		}
	}
	return line
}

// computeBreakPatterns returns the effective BREAK patterns for a dictConfig.
// When BREAK 0 is set, returns nil (disabled).
// When explicit patterns are given they replace the hunspell defaults.
// Otherwise the defaults ("-", "^-", "-$") are used.
func computeBreakPatterns(aff *dictConfig) []string {
	if !aff.BreakEnabled {
		return nil
	}
	if len(aff.BreakPatterns) > 0 {
		return aff.BreakPatterns
	}
	return []string{"-", "^-", "-$"}
}

func (s *GoSpell) surfaceAllowsStandalone(word string) (bool, bool) {
	s.ensureLazySurface(word)
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

func (s *GoSpell) spellCompound(word string, skipTriple bool) bool {
	runes := []rune(word)
	if len(runes) < 2*s.compoundMin {
		return false
	}
	parts, ok := s.spellCompoundParts(runes, caseStyle(word), true, skipTriple)
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
			// Use wordOrRootHasFlag for non-stemOnly rules so affix-derived
			// forms are checked too, matching compoundPatternBlocks behaviour.
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
			if s.compoundStartPart(left) && s.compoundFinalPart(right, wholeStyle) {
				return true
			}
		}
	}
	return false
}

func (s *GoSpell) spellCompoundParts(runes []rune, wholeStyle wordCase, first bool, skipTriple bool) ([]string, bool) {
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
		if s.checkCompoundTriple && !skipTriple && compoundBoundaryHasTriple(prefix, suffix) {
			continue
		}
		if s.compoundFinalPart(suffix, wholeStyle) {
			return []string{prefix, suffix}, true
		}
		// Don't recurse into a suffix that is itself a blocked compound
		// (e.g. "forbiddenroot" blocked by the "forbidden root" dic entry).
		if _, blocked := s.blockedCompound[suffix]; !blocked {
			if rest, ok := s.spellCompoundParts([]rune(suffix), wholeStyle, false, skipTriple); ok {
				return append([]string{prefix}, rest...), true
			}
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
	if s.forceUcaseWords != nil {
		if _, ok := s.forceUcaseWords[word]; ok && wholeStyle == allLower {
			return false
		}
	}
	if ok, found := s.surfaceAllowsCompound(word, compoundPositionEnd); found {
		return ok
	}
	return s.compoundSetContains(s.compoundEnd, word) ||
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
	s.ensureLazySurface(word)
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
					if !s.wordHasFlag(left, rule.leftFlag) && !s.derivedOnlyInCompoundHasRootFlag(right, rule.leftFlag) {
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

func (s *GoSpell) derivedOnlyInCompoundHasRootFlag(word, flag string) bool {
	if flag == "" {
		return false
	}
	s.ensureLazySurface(word)
	for _, entry := range s.surfaces[word] {
		if entry.IsRoot || !entry.OnlyInCompound {
			continue
		}
		for _, rawFlag := range entry.RawFlags {
			if rawFlag == flag {
				return true
			}
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
	for i := 1; i <= len(runes); i++ {
		c := runes[i-1]
		extended := make([]rune, len(runes)+1)
		copy(extended[:i], runes[:i])
		extended[i] = c
		copy(extended[i+1:], runes[i:])
		if s.spellCompound(string(extended), true) {
			return true
		}
	}
	return false
}

// compoundTypoMatchesDict reports whether word is one edit away from any
// standalone dictionary entry. It is called from spellCompound to suppress
// false-positive splits: when a 3+-part compound parse succeeds but the whole
// word is itself just a misspelling of a single real word (e.g. "teh" splits
// as "t"+"e"+"h" but is really a typo of "the"), this check rejects the
// compound interpretation.
//
// oneEditAway accepts pairs whose rune counts differ by at most 1, so we must
// check the rl-1, rl, and rl+1 buckets to catch insertion and deletion edits.
// dictByRuneLen lets us skip the ~97% of the dictionary outside those three
// buckets. Reading a nil map is safe in Go, so this works correctly even for
// GoSpell values constructed directly (without NewGoSpellReader).
func (s *GoSpell) compoundTypoMatchesDict(word string) bool {
	rl := compoundRuneLen(word)
	for _, dictWord := range s.dictByRuneLen[rl-1] {
		if oneEditAway(word, dictWord) {
			return true
		}
	}
	for _, dictWord := range s.dictByRuneLen[rl] {
		if oneEditAway(word, dictWord) {
			return true
		}
	}
	for _, dictWord := range s.dictByRuneLen[rl+1] {
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
		first := -1
		for i := range ar {
			if ar[i] != br[i] {
				diff++
				if first < 0 {
					first = i
				}
				if diff > 2 {
					return false
				}
			}
		}
		if diff == 1 {
			return true
		}
		// Adjacent transposition: two positions differ and swapping them matches.
		return diff == 2 && first+1 < len(ar) &&
			ar[first] == br[first+1] && ar[first+1] == br[first]
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
	s.ensureLazySurface(word)
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
	_, flags, hasFlags := dicWordSplit(line)
	if !hasFlags {
		return nil
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
		// ONLYINCOMPOUND without explicit position flags is valid at all positions.
		entry.CompoundEndAllowed = !hasExplicitCompoundPosition(rec.flags, affix)
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
	return entry
}

func hasExplicitCompoundPosition(flags string, affix *dictConfig) bool {
	return affix != nil &&
		(flagContains(flags, affix.CompoundBeginFlag, affix.flagMode) ||
			flagContains(flags, affix.CompoundMiddleFlag, affix.flagMode) ||
			flagContains(flags, affix.CompoundEndFlag, affix.flagMode))
}

func rootExpandedRecord(base string, rawFlags []string, affix *dictConfig) expandedWord {
	rec := expandedWord{word: base}
	if affix == nil || len(rawFlags) == 0 {
		return rec
	}
	flags := mergeFlags(affix.flagMode, rawFlags...)
	rec.flags = flags
	rec.mask, rec.forbid = affix.maskForFlags(rawFlags)
	for _, flag := range rawFlags {
		if affix.isCompoundOnlyFlag(flag) {
			rec.compoundOnly = true
		}
	}
	rec.needsAffix = affix.NeedAffixFlag != "" && hasFlagToken(rawFlags, affix.NeedAffixFlag)
	return rec
}

func (s *GoSpell) addRootEntry(line string, affix *dictConfig) error {
	rawFlags := compoundEntryFlags(line, affix)
	base, _, _ := dicWordSplit(line)
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	entry := dictionaryEntry{
		line:     line,
		base:     base,
		rawFlags: append([]string(nil), rawFlags...),
	}
	s.entriesByRoot[base] = append(s.entriesByRoot[base], entry)
	insertWord(s.dict, base)
	rec := rootExpandedRecord(base, rawFlags, affix)
	if s.surfaces == nil {
		s.surfaces = make(map[string][]surfaceEntry)
	}
	s.surfaces[base] = append(s.surfaces[base], buildSurfaceEntry(base, rawFlags, affix, rec))
	if len(rawFlags) > 0 {
		if s.wordFlags[base] == nil {
			s.wordFlags[base] = make(map[string]struct{}, len(rawFlags))
		}
		for _, flag := range rawFlags {
			s.wordFlags[base][flag] = struct{}{}
			if _, ok := affix.compoundMap[flag]; ok {
				affix.compoundMap[flag] = append(affix.compoundMap[flag], base)
			}
			if flag == affix.ForceUcaseFlag {
				if s.forceUcaseWords == nil {
					s.forceUcaseWords = make(map[string]struct{})
				}
				s.forceUcaseWords[base] = struct{}{}
			}
		}
	}
	if rec.compoundOnly {
		if s.compoundOnly == nil {
			s.compoundOnly = make(map[string]struct{})
		}
		s.compoundOnly[base] = struct{}{}
	} else if !rec.forbid {
		if rec.mask&compoundBegin != 0 {
			s.compoundBegin[base] = struct{}{}
		}
		if rec.mask&compoundMiddle != 0 {
			s.compoundMiddle[base] = struct{}{}
		}
		if rec.mask&compoundEnd != 0 {
			s.compoundEnd[base] = struct{}{}
		}
	}
	if strings.ContainsRune(base, ' ') {
		if s.blockedCompound == nil {
			s.blockedCompound = make(map[string]struct{})
		}
		s.blockedCompound[strings.ReplaceAll(base, " ", "")] = struct{}{}
		records, err := affix.expandRecords(line)
		if err != nil {
			return err
		}
		for _, rec := range records {
			if strings.ContainsRune(rec.word, ' ') {
				s.blockedCompound[strings.ReplaceAll(rec.word, " ", "")] = struct{}{}
			}
		}
	}
	if len(base) > s.maxWordLen {
		s.maxWordLen = len(base)
	}
	return nil
}

func (s *GoSpell) ensureLazySurface(word string) {
	if s == nil || s.affix == nil || word == "" {
		return
	}
	if s.lazySurfaceChecked == nil {
		s.lazySurfaceChecked = make(map[string]struct{})
	}
	if _, ok := s.lazySurfaceChecked[word]; ok {
		return
	}
	s.lazySurfaceChecked[word] = struct{}{}
	if s.surfaces == nil {
		s.surfaces = make(map[string][]surfaceEntry)
	}

	candidates := s.lazyRootCandidates(word)
	seenSurface := make(map[string]struct{})
	for _, entry := range candidates {
		records, err := s.affix.expandRecords(entry.line)
		if err != nil {
			continue
		}
		for _, rec := range records {
			if rec.word != word {
				continue
			}
			key := lazySurfaceKey(entry.line, rec)
			if _, ok := seenSurface[key]; ok {
				continue
			}
			seenSurface[key] = struct{}{}
			s.surfaces[word] = append(s.surfaces[word], buildSurfaceEntry(word, entry.rawFlags, s.affix, rec))
			if strings.ContainsRune(word, ' ') {
				if s.blockedCompound == nil {
					s.blockedCompound = make(map[string]struct{})
				}
				s.blockedCompound[strings.ReplaceAll(word, " ", "")] = struct{}{}
			}
			if len(word) > s.maxWordLen {
				s.maxWordLen = len(word)
			}
		}
	}
}

func lazySurfaceKey(line string, rec expandedWord) string {
	return line + "\x00" + rec.word + "\x00" + rec.flags + "\x00" + strconv.Itoa(int(rec.state)) + "\x00" + strconv.Itoa(int(rec.mask))
}

func (s *GoSpell) lazyRootCandidates(word string) []dictionaryEntry {
	seenRoots := make(map[string]struct{})
	stems := s.reverseAffixStems(word)
	out := make([]dictionaryEntry, 0, len(stems))
	for _, stem := range stems {
		if _, done := seenRoots[stem]; done {
			continue
		}
		seenRoots[stem] = struct{}{}
		out = append(out, s.entriesByRoot[stem]...)
	}
	return out
}

func (s *GoSpell) reverseAffixStems(word string) []string {
	if s.affix == nil {
		return []string{word}
	}
	type state struct {
		word   string
		prefix int
		suffix int
	}
	queue := []state{{word: word}}
	seen := map[state]struct{}{{word: word}: {}}
	stems := []string{word}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.prefix+cur.suffix >= 3 {
			continue
		}
		for _, af := range s.affix.AffixMap {
			if af.Type == prefix && cur.prefix >= 1 {
				continue
			}
			if af.Type == suffix && cur.suffix >= 2 {
				continue
			}
			for _, r := range af.Rules {
				stem, ok := reverseAffixRule(cur.word, af.Type, r)
				if !ok {
					continue
				}
				next := state{word: stem, prefix: cur.prefix, suffix: cur.suffix}
				if af.Type == prefix {
					next.prefix++
				} else {
					next.suffix++
				}
				if _, ok := seen[next]; ok {
					continue
				}
				seen[next] = struct{}{}
				stems = append(stems, stem)
				queue = append(queue, next)
			}
		}
	}
	return stems
}

func reverseAffixRule(word string, typ affixType, r rule) (string, bool) {
	var stem string
	if typ == prefix {
		if !strings.HasPrefix(word, r.AffixText) {
			return "", false
		}
		stem = r.Strip + word[len(r.AffixText):]
	} else {
		if !strings.HasSuffix(word, r.AffixText) {
			return "", false
		}
		stem = word[:len(word)-len(r.AffixText)] + r.Strip
	}
	if stem == word {
		return "", false
	}
	if r.matcher != nil && !r.matcher.MatchString(stem) {
		return "", false
	}
	return stem, true
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
		compoundOnly:        affix.compoundOnlyWords,
		compoundBegin:       affix.compoundBeginWords,
		compoundMiddle:      affix.compoundMiddleWords,
		compoundEnd:         affix.compoundEndWords,
		forceUcaseWords:     affix.forceUcaseWords,
		affix:               affix,
		entriesByRoot:       make(map[string][]dictionaryEntry),
		lazySurfaceChecked:  make(map[string]struct{}),
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
		breakPatterns:       computeBreakPatterns(affix),
	}

	for scanner.Scan() {
		line := stripDicMorphFields(scanner.Text())
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if err := gs.addRootEntry(line, affix); err != nil {
			return nil, fmt.Errorf("unable to process %q: %s", line, err)
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

	// Build the rune-length index used by compoundTypoMatchesDict.
	// oneEditAway can only return true for words whose rune counts differ by at
	// most 1, so grouping by rune count lets the typo guard skip the vast
	// majority of the dictionary. We pre-size the outer map with maxWordLen as
	// a rough upper bound on the number of distinct lengths.
	gs.dictByRuneLen = make(map[int][]string, gs.maxWordLen)
	for w := range gs.dict {
		rl := utf8.RuneCountInString(w)
		gs.dictByRuneLen[rl] = append(gs.dictByRuneLen[rl], w)
	}

	if err := gs.SetSuggester(NewMutationSuggester(MutationOptions{})); err != nil {
		return nil, err
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
