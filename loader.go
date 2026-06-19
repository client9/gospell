package gospell

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SearchPaths returns the ordered list of directories to search for named
// dictionaries. DICPATH (colon-separated, hunspell convention) is read first;
// standard system locations are appended as fallback. Callers may prepend
// their own tool-specific paths before passing the result to Open or
// OpenSupplement.
func SearchPaths() []string {
	var paths []string
	if p := os.Getenv("DICPATH"); p != "" {
		paths = append(paths, filepath.SplitList(p)...)
	}
	paths = append(paths, "/usr/share/hunspell", "/usr/local/share/hunspell")
	return paths
}

// Open finds and loads a named base dictionary (.aff + .dic pair) by searching
// paths in order. name is a bare dictionary name ("en_US"), an absolute path,
// or a relative path containing a separator. The ".aff" or ".dic" extension
// is stripped if present. Returns a descriptive error if no matching pair is
// found in any of the provided paths.
func Open(name string, paths []string) (*GoSpell, error) {
	affPath, dicPath, err := findBase(name, paths)
	if err != nil {
		return nil, err
	}
	return NewGoSpell(affPath, dicPath)
}

// AddDic finds a .dic file by searching paths and merges it into gs using
// AddDictionaryFile. Affix rules from gs are reused, so inflected forms
// (e.g. "colors" from "color/S") are recognized. The .dic file must be UTF-8.
func AddDic(gs *GoSpell, name string, paths []string) error {
	dicPath, err := findSupplement(name, paths)
	if err != nil {
		return err
	}
	return gs.AddDictionaryFile(dicPath)
}

// OpenSupplement finds and loads a .dic-only domain supplement (no .aff file)
// by searching paths in order. The result is a WordList ready to attach to a
// Checker. Supplement dictionaries extend the vocabulary of a base dictionary
// with domain-specific terms (medical, legal, technical) without affix
// processing. Encoding is assumed UTF-8; for non-UTF-8 supplements use a full
// base dictionary (.aff + .dic) as the first entry instead.
func OpenSupplement(name string, paths []string) (*WordList, error) {
	dicPath, err := findSupplement(name, paths)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(dicPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return NewWordListFromDic(f)
}

// NewWordListFromDic parses a Hunspell .dic file from r and returns the word
// stems as an allowed WordList. The word-count header line is skipped; affix
// flags (the part after '/') are stripped from each entry.
//
// This is distinct from NewWordList, which parses the personal word list
// format (plain words or *word for forbidden entries). Use NewWordListFromDic
// when loading a raw Hunspell .dic file as a vocabulary supplement.
func NewWordListFromDic(r io.Reader) (*WordList, error) {
	wl := &WordList{
		allowed:   make(map[string]struct{}),
		forbidden: make(map[string]struct{}),
	}
	scanner := bufio.NewScanner(r)
	// First line is the word count — skip it.
	scanner.Scan()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip affix flags using dicWordSplit so escaped slashes (\/) in the
		// word (e.g. TCP\/IP) are handled correctly.
		word, _, _ := dicWordSplit(line)
		line = strings.TrimSpace(word)
		if line != "" {
			wl.allowed[line] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return wl, nil
}

// findBase locates a base dictionary pair (.aff + .dic) among the candidates
// derived from name and paths.
func findBase(name string, paths []string) (affPath, dicPath string, err error) {
	base := stripDictExt(name)
	for _, candidate := range dictCandidates(base, paths) {
		aff := candidate + ".aff"
		dic := candidate + ".dic"
		if _, err := os.Stat(aff); err != nil {
			continue
		}
		if _, err := os.Stat(dic); err != nil {
			continue
		}
		return aff, dic, nil
	}
	return "", "", fmt.Errorf("dictionary %q not found (searched %s)", name, strings.Join(paths, string(filepath.ListSeparator)))
}

// findSupplement locates a .dic-only supplement file among the candidates
// derived from name and paths.
func findSupplement(name string, paths []string) (string, error) {
	base := stripDictExt(name)
	for _, candidate := range dictCandidates(base, paths) {
		dic := candidate + ".dic"
		if _, err := os.Stat(dic); err == nil {
			return dic, nil
		}
		// Accept the candidate as-is when the caller passed a full filename.
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("supplement dictionary %q not found (searched %s)", name, strings.Join(paths, string(filepath.ListSeparator)))
}

// dictCandidates returns the base paths (without extension) to probe for a
// dictionary name. Absolute paths and names containing a path separator are
// treated as a single explicit candidate. Bare names are joined with each
// search directory.
func dictCandidates(name string, paths []string) []string {
	if filepath.IsAbs(name) || strings.ContainsRune(name, filepath.Separator) {
		return []string{name}
	}
	candidates := make([]string, len(paths))
	for i, dir := range paths {
		candidates[i] = filepath.Join(dir, name)
	}
	return candidates
}

// stripDictExt removes a trailing ".aff" or ".dic" extension so callers can
// pass either "en_US", "en_US.aff", or "en_US.dic" interchangeably.
func stripDictExt(name string) string {
	if strings.HasSuffix(name, ".aff") || strings.HasSuffix(name, ".dic") {
		return name[:len(name)-4]
	}
	return name
}
