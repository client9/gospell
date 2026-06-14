package main

import (
	"strings"

	"github.com/client9/gospell"
)

// Diff represents an unknown word found in a file.
type Diff struct {
	Path     string
	Original string
	Line     string
	LineNum  int
}

// SpellFile spell-checks raw bytes treated as plain text.
func SpellFile(gs *gospell.Checker, raw []byte) []Diff {
	out := []Diff{}

	s := removeURL(string(raw))
	s = removePath(s)

	for linenum, line := range strings.Split(s, "\n") {
		words := splitWords(line)
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
