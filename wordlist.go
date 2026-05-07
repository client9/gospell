package gospell

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// WordList is a small overlay of allowed and forbidden words used as a
// per-context supplement to a base GoSpell dictionary. Multiple WordLists can
// be active simultaneously; see Checker. Loading policy — global or
// per-document — is the caller's responsibility.
//
// Format (one entry per line):
//
//	# comment
//	word          → allowed
//	*word         → forbidden
type WordList struct {
	allowed   map[string]struct{}
	forbidden map[string]struct{}
}

// NewWordList parses a WordList from r.
func NewWordList(r io.Reader) (*WordList, error) {
	wl := &WordList{
		allowed:   make(map[string]struct{}),
		forbidden: make(map[string]struct{}),
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "*") {
			word := strings.TrimSpace(line[1:])
			if word != "" {
				wl.forbidden[word] = struct{}{}
			}
			continue
		}
		wl.allowed[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return wl, nil
}

// NewWordListFile parses a WordList from a file.
func NewWordListFile(name string) (*WordList, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return NewWordList(f)
}

// Add adds word to the allowed set.
func (wl *WordList) Add(word string) {
	if wl.allowed == nil {
		wl.allowed = make(map[string]struct{})
	}
	wl.allowed[word] = struct{}{}
}

// Forbid adds word to the forbidden set.
func (wl *WordList) Forbid(word string) {
	if wl.forbidden == nil {
		wl.forbidden = make(map[string]struct{})
	}
	wl.forbidden[word] = struct{}{}
}

// HasWord reports whether word is in the allowed set.
func (wl *WordList) HasWord(word string) bool {
	if wl == nil {
		return false
	}
	_, ok := wl.allowed[word]
	return ok
}

// IsForbidden reports whether word is in the forbidden set.
func (wl *WordList) IsForbidden(word string) bool {
	if wl == nil {
		return false
	}
	_, ok := wl.forbidden[word]
	return ok
}
