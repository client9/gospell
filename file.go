package gospell

import (
	"path/filepath"
	"strings"

	"github.com/client9/plaintext"
)

// Diff represent a unknown word in a file
type Diff struct {
	Filename string
	Path     string
	Original string
	Line     string
	LineNum  int
}

// SpellFile is attempts to spell-check a file.  This interface is not
// very good so expect changes.
func SpellFile(gs *GoSpell, fullpath string, raw []byte) []Diff {
	out := []Diff{}
	md, err := plaintext.ExtractorByFilename(fullpath)
	if err != nil {
		return nil
	}
	// remove any golang templates
	raw = plaintext.StripTemplate(raw)

	// extract plain text
	raw = md.Text(raw)

	// do character conversion "smart quotes" to quotes, etc
	// as specified in the Affix file
	rawstring := gs.InputConversion(raw)

	// zap URLS
	s := RemoveURL(rawstring)

	for linenum, line := range strings.Split(s, "\n") {
		// now get words
		words := gs.Split(line)
		for _, word := range words {
			// HACK
			word = strings.Trim(word, "'")
			if known := gs.Spell(word); !known {
				out = append(out, Diff{
					Filename: filepath.Base(fullpath),
					Path:     fullpath,
					Line:     line,
					LineNum:  linenum + 1,
					Original: word,
				})
			}
		}
	}
	return out
}
