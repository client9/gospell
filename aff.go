package gospell

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	// "log"
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

func (a affix) expand(word string, out []string) []string {
	for _, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}
		if a.Type == prefix {
			out = append(out, r.AffixText+word)
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, stripWord+r.AffixText)
		}
	}
	return out
}

type rule struct {
	Strip     string
	AffixText string
	Pattern   string
	matcher   *regexp.Regexp
}

type dictConfig struct {
	Flag              string
	TryChars          string
	WordChars         string
	NoSuggestFlag     rune
	IconvReplacements []string
	Replacements      [][2]string
	AffixMap          map[rune]affix
	CompoundMin       int
	CompoundOnly      string
	CompoundRule      []string
	compoundMap       map[rune][]string
}

func (a dictConfig) expand(wordAffix string, out []string) ([]string, error) {
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
		if _, ok := a.compoundMap[key]; !ok {
			continue
		}
		a.compoundMap[key] = append(a.compoundMap[key], word)
	}

	if compoundOnly {
		return out, nil
	}

	out = append(out, word)
	prefixes := make([]affix, 0, 5)
	suffixes := make([]affix, 0, 5)
	for _, key := range keyString {
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
			out = af.expand(word, out)
			continue
		}
		if af.Type == prefix {
			prefixes = append(prefixes, af)
		} else {
			suffixes = append(suffixes, af)
		}
	}

	for _, suf := range suffixes {
		out = suf.expand(word, out)
	}
	for _, pre := range prefixes {
		prewords := pre.expand(word, nil)
		out = append(out, prewords...)
		for _, suf := range suffixes {
			for _, w := range prewords {
				out = suf.expand(w, out)
			}
		}
	}
	return out, nil
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

func newDictConfig(file io.Reader) (*dictConfig, error) {
	aff := dictConfig{
		Flag:        "ASCII",
		AffixMap:    make(map[rune]affix),
		compoundMap: make(map[rune][]string),
		CompoundMin: 3,
	}
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
			if parts[1] != "UTF-8" {
				return nil, fmt.Errorf("SET had non-UTF-8 character encoding of %q -- not supported", parts[1])
			}
			// UTF-8 - nothing to do
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
				a := affix{
					Type:         atype,
					CrossProduct: cross,
				}
				flag := rune(parts[1][0])
				aff.AffixMap[flag] = a
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
				var matcher *regexp.Regexp
				var err error
				pat := parts[4]
				if pat != "." {
					if a.Type == prefix {
						pat = "^" + pat
					} else {
						pat = pat + "$"
					}
					matcher, err = regexp.Compile(pat)
					if err != nil {
						return nil, fmt.Errorf("unable to compile %s", pat)
					}
					//log.Printf("compiled regexp %q", pat)
				}
				a.Rules = append(a.Rules, rule{
					Strip:     strip,
					AffixText: parts[3],
					Pattern:   parts[4],
					matcher:   matcher,
				})
				aff.AffixMap[flag] = a
			default:
				return nil, fmt.Errorf("%s stanza had %d fields, expected 4 or 5", parts[0], len(parts))
			}
		default:
			return nil, fmt.Errorf("unknown command %v", parts)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &aff, nil
}
