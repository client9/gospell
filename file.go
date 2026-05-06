package gospell

import (
	"strings"
)

// Diff represent a unknown word in a file
type Diff struct {
	Filename string
	Path     string
	Original string
	Line     string
	LineNum  int
}

// SpellFile spell-checks raw bytes treated as plain text.
func SpellFile(gs *GoSpell, raw []byte) []Diff {
	out := []Diff{}

	rawstring := gs.InputConversion(raw)
	s := RemoveURL(rawstring)
	s = RemovePath(s)

	for linenum, line := range strings.Split(s, "\n") {
		words := gs.Split(line)
		for _, word := range words {
			word = strings.Trim(word, "'")
			if known := gs.Spell(word); !known {
				out = append(out, Diff{
					Line:     line,
					LineNum:  linenum + 1,
					Original: word,
				})
			}
		}
	}
	return out
}
