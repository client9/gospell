package gospell

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type flagMode int

const (
	flagASCII flagMode = iota
	flagLong
	flagNum
	flagUTF8
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
	Flag      string
	Permit    string
	Forbid    string
	Only      string
	NeedAffix string
}

type expandedWord struct {
	word         string
	flags        string
	mask         compoundMask
	state        affixState
	forbid       bool
	compoundOnly bool
	needsAffix   bool // true if this form has an unresolved NEEDAFFIX
}

type compoundMask uint8

type affixState uint8

type compoundPatternRule struct {
	left          string
	right         string
	leftFlag      string
	rightFlag     string
	mod           string
	leftStemOnly  bool // true when parsed from "0/flag" — only match root dic entries
	rightStemOnly bool
}

const (
	compoundBegin compoundMask = 1 << iota
	compoundMiddle
	compoundEnd
)

const (
	statePrefix affixState = 1 << iota
	stateSuffix
)

func (a affix) expand(word, flags string, state affixState, c compoundRules, mode flagMode, incomingNeedsAffix bool, out []expandedWord) []expandedWord {
	for _, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}
		mask := compoundMask(0)
		forbid := false
		if flagContains(r.OutFlags, c.Forbid, mode) {
			mask = 0
			forbid = true
		} else if flagContains(r.OutFlags, c.Flag, mode) || flagContains(r.OutFlags, c.Permit, mode) {
			mask = compoundBegin | compoundMiddle | compoundEnd
			if flagContains(r.OutFlags, c.Only, mode) {
				mask &^= compoundEnd
			}
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
		if outState == statePrefix|stateSuffix && !flagContains(r.OutFlags, c.Flag, mode) && !flagContains(r.OutFlags, c.Permit, mode) {
			mask = 0
		}
		// Compute NEEDAFFIX status for the generated form.
		//   Suffix: determined solely by whether this rule's output has the flag.
		//   Prefix applied after a suffix: X only persists if BOTH this rule
		//     and the incoming suffix form have X (each needs the other type).
		//   Prefix applied to root (no prior suffix): X persists if this rule has it.
		ruleHasX := c.NeedAffix != "" && flagContains(r.OutFlags, c.NeedAffix, mode)
		var needsAffix bool
		if a.Type == suffix {
			needsAffix = ruleHasX
		} else { // prefix
			if state&stateSuffix != 0 {
				needsAffix = ruleHasX && incomingNeedsAffix
			} else {
				needsAffix = ruleHasX
			}
		}
		if a.Type == prefix {
			stripWord := word
			if r.Strip != "" && strings.HasPrefix(word, r.Strip) {
				stripWord = word[len(r.Strip):]
			}
			out = append(out, expandedWord{
				word:       r.AffixText + stripWord,
				flags:      appendFlags(flags, r.OutFlags, mode),
				mask:       mask,
				state:      outState,
				forbid:     forbid,
				needsAffix: needsAffix,
			})
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, expandedWord{
				word:       stripWord + r.AffixText,
				flags:      appendFlags(flags, r.OutFlags, mode),
				mask:       mask,
				state:      outState,
				forbid:     forbid,
				needsAffix: needsAffix,
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
	Flag                string
	flagMode            flagMode
	TryChars            string
	WordChars           string
	NoSuggestFlag       string
	NeedAffixFlag       string
	ForbiddenWordFlag   string
	ForceUcaseFlag      string
	CompoundBeginFlag   string
	CompoundMiddleFlag  string
	CompoundEndFlag     string
	CompoundFlag        string
	CompoundPermitFlag  string
	CompoundForbidFlag  string
	IconvReplacements   []string
	Replacements        [][2]string
	CheckCompoundCase   bool
	CheckCompoundDup    bool
	CheckCompoundTriple bool
	SimplifiedTriple    bool
	CheckCompoundRep    bool
	BreakEnabled        bool // false only when BREAK 0 is set
	// AffixMap stores pointers so appending rules in newDictConfig never
	// requires a map write-back after each rule line.
	AffixMap               map[string]*affix
	CompoundMin            int
	CompoundOnly           string
	CompoundRule           []string
	checkCompoundPatterns  []compoundPatternRule
	compoundMap            map[string][]string
	compoundBeginWords     map[string]struct{}
	compoundMiddleWords    map[string]struct{}
	compoundEndWords       map[string]struct{}
	compoundForbiddenWords map[string]struct{}
	compoundOnlyWords      map[string]struct{}
	forceUcaseWords        map[string]struct{}
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
	records, err := a.expandRecords(wordAffix)
	if err != nil {
		return nil, err
	}
	out = out[:0]
	for _, rec := range records {
		out = append(out, rec.word)
	}
	return out, nil
}

func (a *dictConfig) expandRecords(wordAffix string) ([]expandedWord, error) {
	idx := strings.Index(wordAffix, "/")

	if idx == -1 {
		return []expandedWord{{word: wordAffix}}, nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return nil, fmt.Errorf("slash char found in first or last position")
	}
	word, keyString := wordAffix[:idx], wordAffix[idx+1:]
	keys, err := a.splitFlags(a.normalizeFlags(keyString))
	if err != nil {
		return nil, err
	}
	rootMask, _ := a.maskForFlags(keys)
	rootOnly := false
	rootForbid := false
	for _, key := range keys {
		if a.isCompoundOnlyFlag(key) {
			rootOnly = true
		}
		if key == string(a.CompoundForbidFlag) {
			rootForbid = true
		}
	}
	// The root dic entry is a virtual stem if it carries the NEEDAFFIX flag.
	rootNeedsAffix := a.NeedAffixFlag != "" && flagContains(a.normalizeFlags(keyString), a.NeedAffixFlag, a.flagMode)
	added := make(map[string]int)
	seen := make(map[string]struct{})
	var out []expandedWord
	if err := a.expandStateRecords(word, keyString, rootOnly, rootMask, 0, rootForbid, rootNeedsAffix, added, map[string]struct{}{}, seen, 0, 0, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *dictConfig) expandStateRecords(word, flags string, compoundOnly bool, currentMask compoundMask, currentState affixState, explicitForbid bool, needsAffix bool, added map[string]int, used map[string]struct{}, seen map[string]struct{}, prefixCount, suffixCount int, out *[]expandedWord) error {
	flags = a.normalizeFlags(flags)
	keys, err := a.splitFlags(flags)
	if err != nil {
		return err
	}
	stateKey := a.expandStateKey(word, keys, compoundOnly, currentMask, currentState, explicitForbid)
	if _, ok := seen[stateKey]; ok {
		return nil
	}
	seen[stateKey] = struct{}{}
	for _, key := range keys {
		if _, ok := a.compoundMap[key]; ok {
			a.compoundMap[key] = append(a.compoundMap[key], word)
		}
		if key == a.ForceUcaseFlag {
			if a.forceUcaseWords == nil {
				a.forceUcaseWords = make(map[string]struct{})
			}
			a.forceUcaseWords[word] = struct{}{}
		}
		if a.isCompoundOnlyFlag(key) {
			compoundOnly = true
			continue
		}
	}
	a.markCompoundWord(word, currentMask, compoundOnly, explicitForbid)
	if idx, ok := added[word]; !ok {
		added[word] = len(*out)
		*out = append(*out, expandedWord{
			word:         word,
			flags:        flags,
			mask:         currentMask,
			state:        currentState,
			forbid:       explicitForbid,
			compoundOnly: compoundOnly,
			needsAffix:   needsAffix,
		})
	} else {
		rec := &(*out)[idx]
		rec.mask |= currentMask
		rec.state |= currentState
		rec.forbid = rec.forbid || explicitForbid
		rec.compoundOnly = rec.compoundOnly || compoundOnly
		// false wins: if any path reaches this form without needsAffix, it is valid
		rec.needsAffix = rec.needsAffix && needsAffix
	}
	c := compoundRules{a.CompoundFlag, a.CompoundPermitFlag, a.CompoundForbidFlag, a.CompoundOnly, a.NeedAffixFlag}
	applyKeys := func(keys []string, wantType affixType) error {
		for _, key := range keys {
			if a.isCompoundOnlyFlag(key) || a.isCompoundRuleFlag(key) {
				continue
			}
			nextPrefixCount := prefixCount
			nextSuffixCount := suffixCount
			if wantType == prefix {
				if prefixCount >= 1 {
					continue
				}
				nextPrefixCount++
			} else {
				if suffixCount >= 2 {
					continue
				}
				nextSuffixCount++
			}
			if _, ok := used[key]; ok {
				continue
			}
			af, ok := a.AffixMap[key]
			if !ok || af.Type != wantType {
				continue
			}
			var expanded []expandedWord
			expanded = af.expand(word, flags, currentState, c, a.flagMode, needsAffix, expanded[:0])
			nextUsed := make(map[string]struct{}, len(used)+1)
			for k := range used {
				nextUsed[k] = struct{}{}
			}
			nextUsed[key] = struct{}{}
			for _, ew := range expanded {
				nextOnly := compoundOnly
				a.markCompoundWord(ew.word, ew.mask, nextOnly, ew.forbid)
				if err := a.expandStateRecords(ew.word, ew.flags, nextOnly, ew.mask, ew.state, ew.forbid, ew.needsAffix, added, nextUsed, seen, nextPrefixCount, nextSuffixCount, out); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := applyKeys(keys, suffix); err != nil {
		return err
	}
	if err := applyKeys(keys, prefix); err != nil {
		return err
	}
	return nil
}

func appendFlags(base, extra string, mode flagMode) string {
	return mergeFlags(mode, base, extra)
}

func (a *dictConfig) expandStateKey(word string, flags []string, compoundOnly bool, currentMask compoundMask, currentState affixState, explicitForbid bool) string {
	normalized := append([]string(nil), flags...)
	sort.Strings(normalized)
	return word + "\x00" + strings.Join(normalized, "\x1f") + "\x00" + strconv.FormatBool(compoundOnly) + "\x00" + strconv.Itoa(int(currentMask)) + "\x00" + strconv.Itoa(int(currentState)) + "\x00" + strconv.FormatBool(explicitForbid)
}

func (a *dictConfig) normalizeFlags(flags string) string {
	return mergeFlags(a.flagMode, flags)
}

func mergeFlags(mode flagMode, parts ...string) string {
	seen := make(map[string]struct{})
	var tokens []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		var items []string
		switch mode {
		case flagNum:
			items = strings.Split(part, ",")
		case flagLong:
			items = make([]string, 0, len(part)/2)
			for i := 0; i+1 < len(part); i += 2 {
				items = append(items, part[i:i+2])
			}
		default:
			items = make([]string, 0, len([]rune(part)))
			for _, r := range part {
				items = append(items, string(r))
			}
		}
		for _, item := range items {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			tokens = append(tokens, item)
		}
	}
	sort.Strings(tokens)
	switch mode {
	case flagNum:
		return strings.Join(tokens, ",")
	case flagLong:
		return strings.Join(tokens, "")
	default:
		return strings.Join(tokens, "")
	}
}

func (a *dictConfig) compoundRuleTokens(rule string) []string {
	var tokens []string
	switch a.flagMode {
	case flagLong:
		for i := 0; i < len(rule); {
			switch rule[i] {
			case '(', ')', '+', '?', '*':
				i++
			default:
				if i+1 >= len(rule) {
					return tokens
				}
				tokens = append(tokens, rule[i:i+2])
				i += 2
			}
		}
	case flagNum:
		for i := 0; i < len(rule); {
			switch rule[i] {
			case '(', ')', '+', '?', '*':
				i++
			default:
				j := i
				for j < len(rule) {
					switch rule[j] {
					case '(', ')', '+', '?', '*':
						goto doneNum
					default:
						j++
					}
				}
			doneNum:
				tokens = append(tokens, rule[i:j])
				i = j
			}
		}
	default:
		for _, r := range rule {
			switch r {
			case '(', ')', '+', '?', '*':
				continue
			default:
				tokens = append(tokens, string(r))
			}
		}
	}
	return tokens
}

func (a *dictConfig) splitFlags(flags string) ([]string, error) {
	switch a.flagMode {
	case flagASCII, flagUTF8:
		out := make([]string, 0, len(flags))
		for _, r := range flags {
			out = append(out, string(r))
		}
		return out, nil
	case flagLong:
		if len(flags)%2 != 0 {
			return nil, fmt.Errorf("FLAG long requires an even number of bytes in %q", flags)
		}
		out := make([]string, 0, len(flags)/2)
		for i := 0; i < len(flags); i += 2 {
			out = append(out, flags[i:i+2])
		}
		return out, nil
	case flagNum:
		return strings.Split(flags, ","), nil
	default:
		return nil, fmt.Errorf("unsupported FLAG mode %q", a.Flag)
	}
}

// splitPatternFlag parses an endchars[/flag] or beginchars[/flag] token from a
// CHECKCOMPOUNDPATTERN directive.  The special endchars value "0" means
// stem-only: the rule fires only on unmodified root dic entries.
func splitPatternFlag(token string) (pattern, flag string, stemOnly bool) {
	var found bool
	pattern, flag, found = strings.Cut(token, "/")
	if !found {
		return token, "", false
	}
	if pattern == "0" {
		pattern = ""
		stemOnly = true
	}
	return pattern, flag, stemOnly
}

func flagContains(flags, want string, mode flagMode) bool {
	if want == "" || flags == "" {
		return false
	}
	switch mode {
	case flagNum:
		for _, part := range strings.Split(flags, ",") {
			if part == want {
				return true
			}
		}
		return false
	case flagLong:
		if len(want) == 1 {
			return strings.ContainsRune(flags, []rune(want)[0])
		}
		for i := 0; i+1 <= len(flags); i += 2 {
			if flags[i:i+2] == want {
				return true
			}
		}
		return false
	default:
		return strings.Contains(flags, want)
	}
}

func singleRune(s string) (rune, bool) {
	r := []rune(s)
	if len(r) != 1 {
		return 0, false
	}
	return r[0], true
}

func (a *dictConfig) isCompoundOnlyFlag(flag string) bool {
	if a.CompoundOnly == "" {
		return false
	}
	return flag == a.CompoundOnly
}

func (a *dictConfig) isCompoundRuleFlag(flag string) bool {
	return flag == a.CompoundFlag || flag == a.CompoundPermitFlag || flag == a.CompoundForbidFlag
}

func (a *dictConfig) maskForFlags(flags []string) (compoundMask, bool) {
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
		Flag:                   "ASCII",
		flagMode:               flagASCII,
		AffixMap:               make(map[string]*affix),
		compoundMap:            make(map[string][]string),
		compoundBeginWords:     make(map[string]struct{}),
		compoundMiddleWords:    make(map[string]struct{}),
		compoundEndWords:       make(map[string]struct{}),
		compoundForbiddenWords: make(map[string]struct{}),
		compoundOnlyWords:      make(map[string]struct{}),
		forceUcaseWords:        make(map[string]struct{}),
		CompoundMin:            3,
		BreakEnabled:           true,
	}
	// Many affix rules share the same condition pattern (e.g. ".", "e",
	// "[^aeiou]y"). Cache parsed matchers so each unique (pattern, type) pair
	// is only allocated once during loading.
	matcherCache := make(map[matcherCacheKey]*affixMatcher)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
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
			aff.CompoundFlag = parts[1]
		case "COMPOUNDPERMITFLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDPERMITFLAG stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundPermitFlag = parts[1]
		case "COMPOUNDFORBIDFLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDFORBIDFLAG stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundForbidFlag = parts[1]
		case "COMPOUNDRULE":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDRULE stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				aff.CompoundRule = make([]string, 0, val)
			} else {
				aff.CompoundRule = append(aff.CompoundRule, parts[1])
				for _, token := range aff.compoundRuleTokens(parts[1]) {
					if _, ok := aff.compoundMap[token]; !ok {
						aff.compoundMap[token] = []string{}
					}
				}
			}
		case "NOSUGGEST":
			if len(parts) != 2 {
				return nil, fmt.Errorf("NOSUGGEST stanza had %d fields, expected 2", len(parts))
			}
			aff.NoSuggestFlag = parts[1]
		case "FORCEUCASE":
			if len(parts) != 2 {
				return nil, fmt.Errorf("FORCEUCASE stanza had %d fields, expected 2", len(parts))
			}
			aff.ForceUcaseFlag = parts[1]
		case "COMPOUNDBEGIN":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDBEGIN stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundBeginFlag = parts[1]
		case "COMPOUNDMIDDLE":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDMIDDLE stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundMiddleFlag = parts[1]
		case "COMPOUNDEND":
			if len(parts) != 2 {
				return nil, fmt.Errorf("COMPOUNDEND stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundEndFlag = parts[1]
		case "CHECKCOMPOUNDPATTERN":
			if len(parts) == 2 {
				continue
			}
			if len(parts) < 3 {
				return nil, fmt.Errorf("CHECKCOMPOUNDPATTERN stanza had %d fields, expected at least 2", len(parts))
			}
			rule := compoundPatternRule{}
			rule.left, rule.leftFlag, rule.leftStemOnly = splitPatternFlag(parts[1])
			rule.right, rule.rightFlag, rule.rightStemOnly = splitPatternFlag(parts[2])
			if len(parts) > 3 {
				rule.mod = parts[3]
			}
			aff.checkCompoundPatterns = append(aff.checkCompoundPatterns, rule)
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
			switch parts[1] {
			case "ASCII":
				aff.flagMode = flagASCII
			case "long":
				aff.flagMode = flagLong
			case "num":
				aff.flagMode = flagNum
			case "UTF-8":
				aff.flagMode = flagUTF8
			default:
				return nil, fmt.Errorf("unsupported FLAG mode %q", parts[1])
			}
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
				flag := parts[1]
				// Store a pointer so subsequent rule appends don't need a write-back.
				aff.AffixMap[flag] = &affix{
					Type:         atype,
					CrossProduct: cross,
				}
			case 5:
				flag := parts[1]
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
				if affText == "0" {
					affText = ""
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
		case "CHECKCOMPOUNDCASE":
			aff.CheckCompoundCase = true
		case "CHECKCOMPOUNDDUP":
			aff.CheckCompoundDup = true
		case "CHECKCOMPOUNDTRIPLE":
			aff.CheckCompoundTriple = true
		case "SIMPLIFIEDTRIPLE":
			aff.SimplifiedTriple = true
		case "CHECKCOMPOUNDREP":
			aff.CheckCompoundRep = true
		case "BREAK":
			// BREAK 0 disables the default hyphen-splitting behavior.
			if len(parts) == 2 && parts[1] == "0" {
				aff.BreakEnabled = false
			}
			// Other BREAK patterns are accepted but not stored; we only
			// implement the default "-" split.
		case "NEEDAFFIX", "PSEUDOROOT":
			if len(parts) != 2 {
				return nil, fmt.Errorf("NEEDAFFIX stanza had %d fields, expected 2", len(parts))
			}
			aff.NeedAffixFlag = parts[1]
		case "FORBIDDENWORD":
			if len(parts) != 2 {
				return nil, fmt.Errorf("FORBIDDENWORD stanza had %d fields, expected 2", len(parts))
			}
			aff.ForbiddenWordFlag = parts[1]
		case "NOSPLITSUGS":
			// suggestion-only option; no effect on spell checking
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
	set := ""
	flagUTF8 := false
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
		switch fields[0] {
		case "SET":
			if len(fields) != 2 {
				return "", fmt.Errorf("SET stanza had %d fields, expected 2", len(fields))
			}
			set = fields[1]
		case "FLAG":
			if len(fields) == 2 && fields[1] == "UTF-8" {
				flagUTF8 = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	// FLAG UTF-8 implies the file itself is UTF-8 encoded when no SET is given.
	if set == "" && flagUTF8 {
		return "UTF-8", nil
	}
	return set, nil
}
