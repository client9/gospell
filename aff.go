package gospell

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Affix is a rule for affix (adding prefixes or suffixes)
type Affix struct {
	Type         string // either PFX or SFX
	Flag         string
	CrossProduct bool
	Rules        []Rule
}

// Expand provides all variations of a given word based on this affix rule
func (a Affix) Expand(word string) []string {
	out := []string{}
	for _, r := range a.Rules {
		if !r.Matcher.MatchString(word) {
			continue
		}
		if a.Type == "PFX" {
			out = append(out, r.AffixText+word)
			// TODO is does Strip apply to prefixes too?
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

// Rule is a Affix rule
type Rule struct {
	Strip     string
	AffixText string         // suffix or prefix
	Condition string         // original regex pattern
	Matcher   *regexp.Regexp // converted into
}

// AFFFile is a partial representation of a Hunspell AFF file.
type AFFFile struct {
	Flag              string
	TryChars          string
	WordChars         string
	IconvReplacements [][2]string
	AffixMap          map[string]Affix
	Replacements      [][2]string
}

// Expand expands a word/affix
func (a AFFFile) Expand(wordAffix string) ([]string, error) {
	out := []string{}
	idx := strings.Index(wordAffix, "/")

	// not found
	if idx == -1 {
		out = append(out, wordAffix)
		return out, nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return nil, fmt.Errorf("Slash char found in first or last position")
	}
	// safe
	word, keyString := wordAffix[:idx], wordAffix[idx+1:]
	out = append(out, word)
	prefixes := []Affix{}
	suffixes := []Affix{}
	for i := 0; i < len(keyString); i++ {
		// no.. this is a rune
		//for _, key := range keys {
		// want keyString to []?something?
		// then iterate over that
		key := string(keyString[i])
		af, ok := a.AffixMap[key]
		if !ok {
			return nil, fmt.Errorf("unable to find affix key %s", key)
		}
		if !af.CrossProduct {
			out = append(out, af.Expand(word)...)
			continue
		}
		if af.Type == "PFX" {
			prefixes = append(prefixes, af)
		} else {
			suffixes = append(suffixes, af)
		}
	}

	// expand all suffixes with out any prefixes
	for _, suf := range suffixes {
		out = append(out, suf.Expand(word)...)
	}
	for _, pre := range prefixes {
		// expand without suffix
		prewords := pre.Expand(word)
		out = append(out, prewords...)

		// now do cross product
		for _, suf := range suffixes {
			for _, w := range prewords {
				out = append(out, suf.Expand(w)...)
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

// NewAFF reads an Hunspell AFF file
func NewAFF(file io.Reader) (*AFFFile, error) {
	aff := AFFFile{
		Flag:     "ASCII",
		AffixMap: make(map[string]Affix),
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
		case "ICONV":
			// if only 2 fields, then its the first stanza that just provides a count
			//  we don't care, as we dynamically allocate
			if len(parts) == 2 {
				continue
			}
			if len(parts) != 3 {
				return nil, fmt.Errorf("ICONV stanza had %d fields, expected 2", len(parts))
			}
			// we have 3
			aff.IconvReplacements = append(aff.IconvReplacements, [2]string{parts[1], parts[2]})
		case "REP":
			// if only 2 fields, then its the first stanza that just provides a count
			//  we don't care, as we dynamically allocate
			if len(parts) == 2 {
				continue
			}
			if len(parts) != 3 {
				return nil, fmt.Errorf("REP stanza had %d fields, expected 2", len(parts))
			}
			// we have 3
			aff.Replacements = append(aff.Replacements, [2]string{parts[1], parts[2]})
		case "WORDCHARS":
			if len(parts) != 2 {
				return nil, fmt.Errorf("WORDCHAR stanza had %d fields, expected 2", len(parts))
			}
			aff.WordChars = parts[1]
		case "FLAG":
			if len(parts) != 2 {
				return nil, fmt.Errorf("FLAG stanze had %d, expected 1", len(parts))
			}
			aff.Flag = parts[1]
			return nil, fmt.Errorf("FLAG stanza not yet supported")
		case "PFX", "SFX":
			switch len(parts) {
			case 4:
				cross, err := isCrossProduct(parts[2])
				if err != nil {
					return nil, err
				}
				// this is a new Affix!
				a := Affix{
					Type:         parts[0],
					Flag:         parts[1],
					CrossProduct: cross,
				}
				aff.AffixMap[a.Flag] = a
			case 5:
				// does this need to be split out into suffix and prefix?
				flag := parts[1]
				a, ok := aff.AffixMap[flag]
				if !ok {
					return nil, fmt.Errorf("Got rules for flag %q but no definition", flag)
				}
				pat := parts[4]
				if a.Type == "PFX" {
					pat = "^" + pat
				} else {
					pat = pat + "$"
				}
				matcher, err := regexp.Compile(pat)
				if err != nil {
					return nil, fmt.Errorf("Unable to compile %s", pat)
				}
				strip := parts[2]
				if strip == "0" {
					strip = ""
				}
				a.Rules = append(a.Rules, Rule{
					Strip:     strip,
					AffixText: parts[3],
					Condition: parts[4],
					Matcher:   matcher,
				})
				aff.AffixMap[flag] = a
			default:
				return nil, fmt.Errorf("%s stanza had %d fields, expected 4 or 5", parts[0], len(parts))
			}
		default:
			// nothing
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &aff, nil
}
