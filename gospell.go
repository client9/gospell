package gospell

import (
	"fmt"
	"io"
	"os"
)

// GoSpell is main struct
type GoSpell struct {
}

// Spell -- not done :-)
func (s *GoSpell) Spell(word string) bool {
	return true
}

// NewGoSpellReader creates a speller from io.Readers for aff and dic
// Hunspell files
func NewGoSpellReader(aff, dic io.Reader) (*GoSpell, error) {
	_, err := NewAFF(aff)
	if err != nil {
		return nil, err
	}

	return &GoSpell{}, nil
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
