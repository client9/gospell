package gospell

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

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

// rebuildRuneLenIndex rebuilds dictByRuneLen from the current dict contents.
// Called after bulk additions to keep the compound typo guard in sync.
func (s *GoSpell) rebuildRuneLenIndex() {
	s.dictByRuneLen = make(map[int][]string, s.maxWordLen)
	for w := range s.dict {
		rl := utf8.RuneCountInString(w)
		s.dictByRuneLen[rl] = append(s.dictByRuneLen[rl], w)
	}
}
