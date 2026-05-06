package gospell

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type affixType int

const (
	prefix affixType = iota
	suffix
)

type affix struct {
	Type         affixType
	CrossProduct bool
	Rules        []rule
}

type compoundRules struct {
	Flag   rune
	Permit rune
	Forbid rune
}

type expandedWord struct {
	word  string
	flags string
	mask  compoundMask
	state affixState
}

type compoundMask uint8

type affixState uint8

const (
	compoundBegin compoundMask = 1 << iota
	compoundMiddle
	compoundEnd
)

const (
	statePrefix affixState = 1 << iota
	stateSuffix
)

func (a affix) expand(word, flags string, state affixState, c compoundRules, out []expandedWord) []expandedWord {
	for _, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}
		mask := compoundMask(0)
		if strings.ContainsRune(r.OutFlags, c.Forbid) {
			mask = 0
		} else if strings.ContainsRune(r.OutFlags, c.Flag) || strings.ContainsRune(r.OutFlags, c.Permit) {
			mask = compoundBegin | compoundMiddle | compoundEnd
		} else if a.Type == prefix {
			mask = compoundBegin
		} else {
			mask = compoundEnd
		}
		var outState affixState
		if a.Type == prefix {
			outState = state | statePrefix
		} else {
			outState = state | stateSuffix
		}
		if outState == statePrefix|stateSuffix && !strings.ContainsRune(r.OutFlags, c.Flag) && !strings.ContainsRune(r.OutFlags, c.Permit) {
			mask = 0
		}
		if a.Type == prefix {
			out = append(out, expandedWord{
				word:  r.AffixText + word,
				flags: flags + r.OutFlags,
				mask:  mask,
				state: outState,
			})
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, expandedWord{
				word:  stripWord + r.AffixText,
				flags: flags + r.OutFlags,
				mask:  mask,
				state: outState,
			})
		}
	}
	return out
}

type rule struct {
	Strip     string
	AffixText string
	OutFlags  string
	matcher   *affixMatcher
}

type dictConfig struct {
	Flag              string
	TryChars          string
	WordChars         string
	NoSuggestFlag     rune
	CompoundFlag      rune
	CompoundPermitFlag rune
	CompoundForbidFlag rune
	IconvReplacements []string
	Replacements      [][2]string
	// AffixMap stores pointers so appending rules in newDictConfig never
	// requires a map write-back after each rule line.
	AffixMap          map[rune]*affix
	CompoundMin       int
	CompoundOnly      string
	CompoundRule      []string
	compoundMap       map[rune][]string
	compoundBeginWords    map[string]struct{}
	compoundMiddleWords   map[string]struct{}
	compoundEndWords      map[string]struct{}
	compoundForbiddenWords map[string]struct{}
	compoundOnlyWords     map[string]struct{}
	// Scratch slices reused across expand calls to avoid per-entry allocations.
	prefixScratch  []*affix
	suffixScratch  []*affix
	prewordScratch []expandedWord
}

// expand takes a raw .dic entry (e.g. "work/AB") and appends all valid
// inflected forms to out, returning the updated slice.
// The pointer receiver lets us reuse prefixScratch/suffixScratch/prewordScratch
// across calls without allocating on every .dic entry.
func (a *dictConfig) expand(wordAffix string, out []string) ([]string, error) {
	out = out[:0]
	idx := strings.Index(wordAffix, "/")

	if idx == -1 {
		out = append(out, wordAffix)
		return out, nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return nil, fmt.Errorf("slash char found in first or last position")
	}
	word, keyString := wordAffix[:idx], wordAffix[idx+1:]

	compoundOnly := false
	for _, key := range keyString {
		if strings.ContainsRune(a.CompoundOnly, key) {
			compoundOnly = true
			continue
		}
		if key == a.CompoundFlag || key == a.CompoundPermitFlag || key == a.CompoundForbidFlag {
			continue
		}
		if _, ok := a.compoundMap[key]; !ok {
			continue
		}
		a.compoundMap[key] = append(a.compoundMap[key], word)
	}

	if compoundOnly {
		a.markCompoundWord(word, compoundBegin|compoundMiddle|compoundEnd, true, false)
	}

	if !compoundOnly {
		out = append(out, word)
	}
	mask, forbid := a.maskForFlags(keyString)
	a.markCompoundWord(word, mask, compoundOnly, forbid)
	// Reuse scratch slices to avoid a heap allocation per .dic entry.
	prefixes := a.prefixScratch[:0]
	suffixes := a.suffixScratch[:0]
	for _, key := range keyString {
		if strings.ContainsRune(a.CompoundOnly, key) {
			continue
		}
		if key == a.CompoundFlag || key == a.CompoundPermitFlag || key == a.CompoundForbidFlag {
			continue
		}
		af, ok := a.AffixMap[key]
		if !ok {
			if _, ok := a.compoundMap[key]; ok {
				continue
			}
			if key == a.NoSuggestFlag {
				continue
			}
			return nil, fmt.Errorf("unable to find affix key %v", key)
		}
		if !af.CrossProduct {
			expanded := af.expand(word, keyString, 0, compoundRules{a.CompoundFlag, a.CompoundPermitFlag, a.CompoundForbidFlag}, nil)
			for _, ew := range expanded {
				a.markCompoundWord(ew.word, ew.mask, compoundOnly, false)
				out = append(out, ew.word)
			}
			continue
		}
		if af.Type == prefix {
			prefixes = append(prefixes, af)
		} else {
			suffixes = append(suffixes, af)
		}
	}
	// Save grown slices so future calls benefit from the capacity.
	a.prefixScratch = prefixes
	a.suffixScratch = suffixes

	for _, suf := range suffixes {
		expanded := suf.expand(word, keyString, 0, compoundRules{a.CompoundFlag, a.CompoundPermitFlag, a.CompoundForbidFlag}, nil)
		for _, ew := range expanded {
			a.markCompoundWord(ew.word, ew.mask, compoundOnly, false)
			out = append(out, ew.word)
		}
	}
	for _, pre := range prefixes {
		// Reuse prewordScratch for the prefix-expanded forms; the inner suffix
		// loop only reads it, so it is safe to reuse the same backing array.
		a.prewordScratch = pre.expand(word, keyString, 0, compoundRules{a.CompoundFlag, a.CompoundPermitFlag, a.CompoundForbidFlag}, a.prewordScratch[:0])
		for _, ew := range a.prewordScratch {
			a.markCompoundWord(ew.word, ew.mask, compoundOnly, false)
			out = append(out, ew.word)
		}
		for _, suf := range suffixes {
			for _, ew := range a.prewordScratch {
				expanded := suf.expand(ew.word, ew.flags, ew.state, compoundRules{a.CompoundFlag, a.CompoundPermitFlag, a.CompoundForbidFlag}, nil)
				for _, sw := range expanded {
					a.markCompoundWord(sw.word, sw.mask, compoundOnly, false)
					out = append(out, sw.word)
				}
			}
		}
	}
	return out, nil
}

func (a *dictConfig) maskForFlags(flags string) (compoundMask, bool) {
	var mask compoundMask
	for _, key := range flags {
		switch key {
		case a.CompoundFlag, a.CompoundPermitFlag:
			mask |= compoundBegin | compoundMiddle | compoundEnd
		case a.CompoundForbidFlag:
			return 0, true
		}
	}
	return mask, false
}

func (a *dictConfig) markCompoundWord(word string, mask compoundMask, compoundOnly bool, explicitForbid bool) {
	if compoundOnly {
		if a.compoundOnlyWords == nil {
			a.compoundOnlyWords = make(map[string]struct{})
		}
		a.compoundOnlyWords[word] = struct{}{}
		return
	}
	if explicitForbid {
		delete(a.compoundBeginWords, word)
		delete(a.compoundMiddleWords, word)
		delete(a.compoundEndWords, word)
		return
	}
	if mask&compoundBegin != 0 {
		if a.compoundBeginWords == nil {
			a.compoundBeginWords = make(map[string]struct{})
		}
		a.compoundBeginWords[word] = struct{}{}
	}
	if mask&compoundMiddle != 0 {
		if a.compoundMiddleWords == nil {
			a.compoundMiddleWords = make(map[string]struct{})
		}
		a.compoundMiddleWords[word] = struct{}{}
	}
	if mask&compoundEnd != 0 {
		if a.compoundEndWords == nil {
			a.compoundEndWords = make(map[string]struct{})
		}
		a.compoundEndWords[word] = struct{}{}
	}
}

func isCrossProduct(val string) (bool, error) {
	switch val {
	case "Y":
		return true, nil
	case "N":
		return false, nil
	}
	return false, fmt.Errorf("CrossProduct is not Y or N: got %q", val)
}

// matcherCacheKey identifies a unique affixMatcher by its pattern string and
// whether it is used as a prefix or suffix matcher.
type matcherCacheKey struct {
	pat      string
	isPrefix bool
}

func newDictConfig(file io.Reader) (*dictConfig, error) {
	aff := dictConfig{
		Flag:                  "ASCII",
		AffixMap:              make(map[rune]*affix),
		compoundMap:           make(map[rune][]string),
		compoundBeginWords:    make(map[string]struct{}),
		compoundMiddleWords:   make(map[string]struct{}),
		compoundEndWords:      make(map[string]struct{}),
		compoundForbiddenWords: make(map[string]struct{}),
		compoundOnlyWords:      make(map[string]struct{}),
		CompoundMin:            3,
	}
	// Many affix rules share the same condition pattern (e.g. ".", "e",
	// "[^aeiou]y"). Cache parsed matchers so each unique (pattern, type) pair
	// is only allocated once during loading.
	matcherCache := make(map[matcherCacheKey]*affixMatcher)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "#":
			continue
		case "TRY":
			if len(parts) != 2 {
				return nil, fmt.Errorf("TRY stanza had %d fields, expected 2", len(parts))
			}
			aff.TryChars = parts[1]
		case "SET":
			if len(parts) != 2 {
				return nil, fmt.Errorf("SET stanza had %d fields, expected 2", len(parts))
			}
			// Encoding is handled before parsing in NewGoSpellReader.
		case "ICONV":
			if len(parts) == 2 {
				continue
			}
			if len(parts) != 3 {
				return nil, fmt.Errorf("ICONV stanza had %d fields, expected 2", len(parts))
			}
			aff.IconvReplacements = append(aff.IconvReplacements, parts[1], parts[2])
		case "REP":
			if len(parts) == 2 {
				continue
			}
			if len(parts) != 3 {
				return nil, fmt.Errorf("REP stanza had %d fields, expected 2", len(parts))
			}
			aff.Replacements = append(aff.Replacements, [2]string{parts[1], parts[2]})
		case "COMPOUNDMIN":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %q expected number", parts[1])
			}
			aff.CompoundMin = int(val)
		case "ONLYINCOMPOUND":
			if len(parts) != 2 {
				return nil, fmt.Errorf("ONLYINCOMPOUND stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundOnly = parts[1]
		case "COMPOUNDFLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDFLAG stanza had %d fields, expected 2", len(parts))
			}
			r := []rune(parts[1])
			if len(r) != 1 {
				return nil, fmt.Errorf("COMPOUNDFLAG stanza had more than one flag: %q", parts[1])
			}
			aff.CompoundFlag = r[0]
		case "COMPOUNDPERMITFLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDPERMITFLAG stanza had %d fields, expected 2", len(parts))
			}
			r := []rune(parts[1])
			if len(r) != 1 {
				return nil, fmt.Errorf("COMPOUNDPERMITFLAG stanza had more than one flag: %q", parts[1])
			}
			aff.CompoundPermitFlag = r[0]
		case "COMPOUNDFORBIDFLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDFORBIDFLAG stanza had %d fields, expected 2", len(parts))
			}
			r := []rune(parts[1])
			if len(r) != 1 {
				return nil, fmt.Errorf("COMPOUNDFORBIDFLAG stanza had more than one flag: %q", parts[1])
			}
			aff.CompoundForbidFlag = r[0]
		case "COMPOUNDRULE":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDRULE stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				aff.CompoundRule = make([]string, 0, val)
			} else {
				aff.CompoundRule = append(aff.CompoundRule, parts[1])
				for _, char := range parts[1] {
					if _, ok := aff.compoundMap[char]; !ok {
						aff.compoundMap[char] = []string{}
					}
				}
			}
		case "NOSUGGEST":
			if len(parts) != 2 {
				return nil, fmt.Errorf("NOSUGGEST stanza had %d fields, expected 2", len(parts))
			}
			chars := []rune(parts[1])
			if len(chars) != 1 {
				return nil, fmt.Errorf("NOSUGGEST stanza had more than one flag: %q", parts[1])
			}
			aff.NoSuggestFlag = chars[0]
		case "WORDCHARS":
			if len(parts) != 2 {
				return nil, fmt.Errorf("WORDCHAR stanza had %d fields, expected 2", len(parts))
			}
			aff.WordChars = parts[1]
		case "FLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("FLAG stanza had %d, expected 1", len(parts))
			}
			aff.Flag = parts[1]
			return nil, fmt.Errorf("FLAG stanza not yet supported")
		case "PFX", "SFX":
			atype := prefix
			if parts[0] == "SFX" {
				atype = suffix
			}
			switch len(parts) {
			case 4:
				cross, err := isCrossProduct(parts[2])
				if err != nil {
					return nil, err
				}
				flag := rune(parts[1][0])
				// Store a pointer so subsequent rule appends don't need a write-back.
				aff.AffixMap[flag] = &affix{
					Type:         atype,
					CrossProduct: cross,
				}
			case 5:
				flag := rune(parts[1][0])
				a, ok := aff.AffixMap[flag]
				if !ok {
					return nil, fmt.Errorf("got rules for flag %q but no definition", flag)
				}
				strip := ""
				if parts[2] != "0" {
					strip = parts[2]
				}
				pat := parts[4]
				isPrefix := a.Type == prefix
				cacheKey := matcherCacheKey{pat, isPrefix}
				matcher, cached := matcherCache[cacheKey]
				if !cached {
					var err error
					matcher, err = parseAffixPattern(pat, isPrefix)
					if err != nil {
						return nil, fmt.Errorf("unable to parse affix pattern %q: %w", pat, err)
					}
					matcherCache[cacheKey] = matcher
				}
				affText, outFlags, found := strings.Cut(parts[3], "/")
				if !found {
					affText = parts[3]
				}
				// Append directly to the pointed-to affix; no map write-back needed.
				a.Rules = append(a.Rules, rule{
					Strip:     strip,
					AffixText: affText,
					OutFlags:  outFlags,
					matcher:   matcher,
				})
			default:
				return nil, fmt.Errorf("%s stanza had %d fields, expected 4 or 5", parts[0], len(parts))
			}
		case "MAXNGRAMSUGS":
			// not supported
		default:
			return nil, fmt.Errorf("unknown command %v", parts)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &aff, nil
}

func detectSET(raw []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstLine = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "SET" {
			continue
		}
		if len(fields) != 2 {
			return "", fmt.Errorf("SET stanza had %d fields, expected 2", len(fields))
		}
		return fields[1], nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}
