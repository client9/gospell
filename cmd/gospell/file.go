package main

import (
	"strings"

	"github.com/client9/gospell"
)

// Diff represents an unknown word found in a file.
type Diff struct {
	Path            string
	Original        string
	Line            string
	LineNum         int
	Suggestions     []string
	SuggestionsText string
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
				suggestions, _ := gs.Suggest(word, 5)
				out = append(out, Diff{
					Line:            line,
					LineNum:         linenum + 1,
					Original:        word,
					Suggestions:     suggestionWords(suggestions),
					SuggestionsText: suggestionText(suggestions),
				})
			}
		}
	}
	return out
}

func suggestionWords(suggestions []gospell.Suggestion) []string {
	if len(suggestions) == 0 {
		return nil
	}
	out := make([]string, len(suggestions))
	for i, suggestion := range suggestions {
		out[i] = suggestion.Word
	}
	return out
}

func suggestionText(suggestions []gospell.Suggestion) string {
	return strings.Join(suggestionWords(suggestions), ", ")
}
