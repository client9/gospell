package gospell

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var numericTokenRegexp = regexp.MustCompile("^([0-9]+[.,-]?)+$")

// GoSpell is main struct
type GoSpell struct {
	dict         map[string]struct{}
	compoundOnly map[string]struct{}
	maxWordLen   int
	ireplacer    *strings.Replacer
	compounds    []*regexp.Regexp
	suggester    Suggestions
}

// InputConversion does any character substitution before checking
// based on the ICONV stanza in the AFF file.
func (s *GoSpell) InputConversion(raw []byte) string {
	sraw := string(raw)
	if s.ireplacer == nil {
		return sraw
	}
	return s.ireplacer.Replace(sraw)
}

// AddWordRaw adds a single word to the internal dictionary without modifications.
// Returns true if added, false if already present.
func (s *GoSpell) AddWordRaw(word string) bool {
	if _, ok := s.dict[word]; ok {
		return false
	}
	s.dict[word] = struct{}{}
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
	if _, ok := s.dict[word]; ok {
		return true
	}
	if s.compoundOnly != nil {
		if _, ok := s.compoundOnly[word]; ok {
			return false
		}
	}
	if numericTokenRegexp.MatchString(word) {
		return true
	}
	for _, pat := range s.compounds {
		if pat.MatchString(word) {
			return true
		}
	}
	return false
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
		dict:         make(map[string]struct{}),
		compoundOnly: affix.compoundOnlyWords,
		compounds:    make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
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
			if len(word) > gs.maxWordLen {
				gs.maxWordLen = len(word)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, compoundRule := range affix.CompoundRule {
		pattern := "^"
		for _, key := range compoundRule {
			switch key {
			case '(', ')', '+', '?', '*':
				pattern = pattern + string(key)
			default:
				groups := affix.compoundMap[key]
				pattern = pattern + "(" + strings.Join(groups, "|") + ")"
			}
		}
		pattern = pattern + "$"
		pat, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("unable to compile compound rule %q: %w", pattern, err)
		}
		gs.compounds = append(gs.compounds, pat)
	}

	if len(affix.IconvReplacements) > 0 {
		gs.ireplacer = strings.NewReplacer(affix.IconvReplacements...)
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
