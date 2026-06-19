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

var numericTokenRegexp = regexp.MustCompile(`^([\p{Nd}]+[.,-]?)+$`)

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

	gs.rebuildRuneLenIndex()

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

// AddDictionaryReader merges an additional Hunspell .dic file into this
// GoSpell, applying the same affix rules as the base dictionary. The file
// must be UTF-8 encoded. The word-count header line is optional: if the first
// line cannot be parsed as an integer it is treated as a word entry.
//
// Intended for load-time use; not safe for concurrent calls with Spell or Suggest.
func (g *GoSpell) AddDictionaryReader(dic io.Reader) error {
	dicBytes, err := io.ReadAll(dic)
	if err != nil {
		return fmt.Errorf("read dic: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(dicBytes))
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "\uFEFF")
		fields := strings.Fields(line)
		isCountLine := len(fields) > 0
		if isCountLine {
			_, err := strconv.ParseInt(fields[0], 10, 64)
			isCountLine = err == nil
		}
		if !isCountLine && line != "" && !strings.HasPrefix(line, "#") {
			if err := g.addRootEntry(stripDicMorphFields(line), g.affix); err != nil {
				return fmt.Errorf("unable to process %q: %w", line, err)
			}
		}
	}
	for scanner.Scan() {
		line := stripDicMorphFields(scanner.Text())
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if err := g.addRootEntry(line, g.affix); err != nil {
			return fmt.Errorf("unable to process %q: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	g.rebuildRuneLenIndex()
	return nil
}

// AddDictionaryFile opens dicFile and calls AddDictionaryReader.
func (g *GoSpell) AddDictionaryFile(dicFile string) error {
	f, err := os.Open(dicFile)
	if err != nil {
		return fmt.Errorf("unable to open dic: %w", err)
	}
	defer func() { _ = f.Close() }()
	return g.AddDictionaryReader(f)
}
