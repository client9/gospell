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
	"unicode/utf8"
)

var numericTokenRegexp = regexp.MustCompile("^([0-9]+[.,-]?)+$")

type iconvRule struct {
	old string
	new string
}

// GoSpell is main struct
type GoSpell struct {
	dict             map[string]struct{}
	compoundOnly     map[string]struct{}
	compoundBegin    map[string]struct{}
	compoundMiddle   map[string]struct{}
	compoundEnd      map[string]struct{}
	compoundForbidden map[string]struct{}
	blockedCompound  map[string]struct{}
	compoundMin      int
	maxWordLen       int
	flagMode         flagMode
	iconvRules       []iconvRule
	compounds        []*regexp.Regexp
	suggester        Suggestions
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

// Spell reports whether word is correctly spelled.
func (s *GoSpell) Spell(word string) bool {
	word = s.InputConversion([]byte(word))
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
	if s.compoundOnly != nil {
		if _, ok := s.compoundOnly[word]; ok {
			return false
		}
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
	return false
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
	return s.spellCompoundFromRunes(runes)
}

func (s *GoSpell) spellCompoundFromRunes(runes []rune) bool {
	if len(runes) < s.compoundMin {
		return false
	}
	for i := s.compoundMin; i <= len(runes)-s.compoundMin; i++ {
		prefix := string(runes[:i])
		if !s.compoundStartPart(prefix) {
			continue
		}
		suffix := string(runes[i:])
		if s.compoundFinalPart(suffix) {
			return true
		}
		if s.spellCompoundFromMiddleRunes([]rune(suffix)) {
			return true
		}
	}
	return false
}

func (s *GoSpell) spellCompoundFromMiddleRunes(runes []rune) bool {
	if len(runes) < s.compoundMin {
		return false
	}
	for i := s.compoundMin; i <= len(runes)-s.compoundMin; i++ {
		prefix := string(runes[:i])
		if !s.compoundMiddlePart(prefix) {
			continue
		}
		suffix := string(runes[i:])
		if s.compoundFinalPart(suffix) {
			return true
		}
		if s.spellCompoundFromMiddleRunes([]rune(suffix)) {
			return true
		}
	}
	return false
}

func (s *GoSpell) compoundStartPart(word string) bool {
	if compoundRuneLen(word) < s.compoundMin {
		return false
	}
	if s.compoundBegin != nil {
		if _, ok := s.compoundBegin[word]; ok {
			return true
		}
	}
	if s.compoundOnly != nil {
		if _, ok := s.compoundOnly[word]; ok {
			return true
		}
	}
	return false
}

func (s *GoSpell) compoundMiddlePart(word string) bool {
	if compoundRuneLen(word) < s.compoundMin {
		return false
	}
	if s.compoundMiddle != nil {
		if _, ok := s.compoundMiddle[word]; ok {
			return true
		}
	}
	if s.compoundOnly != nil {
		if _, ok := s.compoundOnly[word]; ok {
			return true
		}
	}
	return false
}

func (s *GoSpell) compoundFinalPart(word string) bool {
	if compoundRuneLen(word) < s.compoundMin {
		return false
	}
	if s.compoundOnly != nil {
		if _, ok := s.compoundOnly[word]; ok {
			return true
		}
	}
	if s.compoundEnd != nil {
		if _, ok := s.compoundEnd[word]; ok {
			return true
		}
	}
	return false
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
		set = "UTF-8"
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
		dict:             make(map[string]struct{}),
		compoundOnly:     affix.compoundOnlyWords,
		compoundBegin:    affix.compoundBeginWords,
		compoundMiddle:   affix.compoundMiddleWords,
		compoundEnd:      affix.compoundEndWords,
		compoundMin:      affix.CompoundMin,
		flagMode:         affix.flagMode,
		compounds:         make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
	}

	words := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		words, err = affix.expand(line, words)
		if err != nil {
			return nil, fmt.Errorf("unable to process %q: %s", line, err)
		}
		for _, word := range words {
			insertWord(gs.dict, word)
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
