package gospell

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

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
		for i := 0; i <= len(wordRunes)-modLen; i++ {
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
			leftRunes := append(append([]rune(nil), wordRunes[:i]...), leftEnd...)
			rightRunes := append(append([]rune(nil), rightStart...), wordRunes[i+modLen:]...)
			left := string(leftRunes)
			right := string(rightRunes)
			if compoundRuneLen(left) < s.compoundMin || compoundRuneLen(right) < s.compoundMin {
				continue
			}
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
		if first {
			if !s.compoundStartPart(prefix) {
				continue
			}
		} else if !s.compoundMiddlePart(prefix) {
			continue
		}
		suffix := string(runes[i:])
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
		if _, blocked := s.blockedCompound[suffix]; !blocked {
			if rest, ok := s.spellCompoundParts(runes[i:], wholeStyle, false, skipTriple); ok {
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

// repBlocksCompoundParts applies CHECKCOMPOUNDREP on each adjacent pair concatenation.
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
