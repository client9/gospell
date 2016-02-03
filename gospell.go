package gospell

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// GoSpell is main struct
type GoSpell struct {
	Config DictConfig
	Dict   map[string]struct{} // likely will contain some value later

	ireplacer *strings.Replacer // input conversion
	compounds []*regexp.Regexp
	splitter  *Splitter
}

// InputConversion does any character substitution before checking
//  This is based on the ICONV stanza
func (s *GoSpell) InputConversion(raw []byte) string {
	sraw := string(raw)
	if s.ireplacer == nil {
		return sraw
	}
	return s.ireplacer.Replace(sraw)
}

// Split a text into Words
func (s *GoSpell) Split(text string) []string {
	return s.splitter.Split(text)
}

// AddWordRaw adds a single word to the internal dictionary without modifications
// returns true if added
// return false is already exists
func (s *GoSpell) AddWordRaw(word string) bool {
	_, ok := s.Dict[word]
	if ok {
		// hey already exists
		return false
	}
	s.Dict[word] = struct{}{}
	return true
}

// AddWordList adds basic word lists, just one word per line
//  Assumed to be in UTF-8
// TODO: hunspell compatible with "*" prefix for forbidden words
// and affix support
// returns list of duplicated words and/or error
func (s *GoSpell) AddWordList(r io.Reader) ([]string, error) {
	var duplicates []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line == "#" {
			continue
		}
		for _, word := range caseVariations(line) {
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

// Spell checks to see if a given word is in the internal dictionaries
// TODO: add multiple dictionaries
func (s *GoSpell) Spell(word string) bool {
	//log.Printf("Checking %s", word)
	_, ok := s.Dict[word]
	if ok {
		return true
	}
	if isNumber(word) {
		return true
	}
	if isNumberHex(word) {
		return true
	}

	if isNumberBinary(word) {
		return true
	}

	if isHash(word) {
		return true
	}

	// check compounds
	for _, pat := range s.compounds {
		if pat.MatchString(word) {
			return true
		}
	}

	// ok maybe a word with units? e.g. 100GB
	units := isNumberUnits(word)
	if units != "" {
		// dictionary appears to have list of units
		if _, ok = s.Dict[units]; ok {
			return true
		}
	}

	// if camelCase and each word e.g. "camel" "Case" is know
	// then the word is ok!
	if chunks := splitCamelCase(word); len(chunks) > 0 {
		if false {
			for _, chunk := range chunks {
				if _, ok = s.Dict[chunk]; !ok {
					return false
				}
			}
		}
		return true
	}

	return false
}

// NewGoSpellReader creates a speller from io.Readers for aff and dic
// Hunspell files
func NewGoSpellReader(aff, dic io.Reader) (*GoSpell, error) {
	affix, err := NewDictConfig(aff)
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
		Dict:      make(map[string]struct{}, i*5),
		compounds: make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		splitter:  NewSplitter(affix.WordChars),
	}

	words := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		words, err = affix.Expand(line, words)
		if err != nil {
			return nil, fmt.Errorf("Unable to process %q: %s", line, err)
		}

		if len(words) == 0 {
			//log.Printf("No words for %s", line)
			continue
		}

		style := CaseStyle(words[0])
		for _, word := range words {
			switch style {
			case AllLower:
				gs.Dict[word] = struct{}{}
				gs.Dict[strings.ToUpper(word[0:1])+word[1:]] = struct{}{}
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
			log.Printf("REGEXP FAIL= %q %s", pattern, err)
		} else {
			//log.Printf("REGEXP ok %s", pattern)
			gs.compounds = append(gs.compounds, pat)
		}

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
