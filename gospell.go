package gospell

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// GoSpell is main struct
type GoSpell struct {
	WordChars string              // from AFF file
	ireplacer *strings.Replacer   // input conversion
	Dict      map[string]struct{} // likely will contain some value later
}

// Input conversion does any character substitution before checking
func (s *GoSpell) InputConversion(raw []byte) string {
	sraw := string(raw)
	if s.ireplacer == nil {
		return sraw
	}
	return s.ireplacer.Replace(sraw)
}

// Spell checks to see if a given word is in the internal dictionaries
// TODO: add multiple dictionaries
func (s *GoSpell) Spell(word string) bool {
	//log.Printf("Checking %s", word)
	_, ok := s.Dict[word]
	return ok
}

// NewGoSpellReader creates a speller from io.Readers for aff and dic
// Hunspell files
func NewGoSpellReader(aff, dic io.Reader) (*GoSpell, error) {
	affix, err := NewAFF(aff)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(dic)
	// get first line
	if !scanner.Scan() {
		return nil, scanner.Err()
	}
	line := scanner.Text()
	i, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return nil, err
	}
	gs := GoSpell{
		WordChars: affix.WordChars,
		Dict:      make(map[string]struct{}, i*5),
	}

	words := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		words, err = affix.Expand(line, words)
		if err != nil {
			// Need to support Compound rules
			//return nil, fmt.Errorf("Unable to process %q: %s", line, err)
			continue
		}

		// this is about 100ms faster, than the full case iteration
		// below
		if false {
			for _, word := range words {
				gs.Dict[word] = struct{}{}
			}
		}

		if true {
			style := CaseStyle(words[0])
			for _, word := range words {
				switch style {
				case AllLower:
					gs.Dict[word] = struct{}{}
					gs.Dict[strings.Title(word)] = struct{}{}
					gs.Dict[strings.ToUpper(word)] = struct{}{}
				case AllUpper:
					gs.Dict[strings.ToUpper(word)] = struct{}{}
				case Title:
					gs.Dict[word] = struct{}{}
					gs.Dict[strings.ToUpper(word)] = struct{}{}
				case Mixed:
					gs.Dict[word] = struct{}{}
					gs.Dict[strings.ToUpper(word)] = struct{}{}
				default:
					gs.Dict[word] = struct{}{}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(affix.IconvReplacements) > 0 {
		gs.ireplacer = strings.NewReplacer(affix.IconvReplacements...)
	}
	//	log.Printf("Internal dictionary has %d entries", len(gs.Dict))
	return &gs, nil
}

// NewGoSpell from aff and dic Hunspell filenames
func NewGoSpell(affFile, dicFile string) (*GoSpell, error) {
	aff, err := os.Open(affFile)
	if err != nil {
		return nil, fmt.Errorf("Unable to open aff: %s", err)
	}
	defer aff.Close()
	dic, err := os.Open(dicFile)
	if err != nil {
		return nil, fmt.Errorf("Unable to open dic: %s", err)
	}
	defer dic.Close()
	h, err := NewGoSpellReader(aff, dic)
	return h, err
}
