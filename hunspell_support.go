package gospell

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type supportStatus string

const (
	supportSupported   supportStatus = "supported"
	supportPartial     supportStatus = "partial"
	supportUnsupported supportStatus = "unsupported"
)

type hunspellSupportEntry struct {
	Directive string
	Status    supportStatus
	Notes     string
}

var hunspellSupportMatrix = []hunspellSupportEntry{
	{Directive: "SET", Status: supportPartial, Notes: "UTF-8 plus ISO8859-1/2/15 are supported"},
	{Directive: "TRY", Status: supportSupported, Notes: "used by suggestions"},
	{Directive: "ICONV", Status: supportSupported, Notes: "input conversion supported"},
	{Directive: "OCONV", Status: supportUnsupported, Notes: "output conversion not implemented"},
	{Directive: "REP", Status: supportSupported, Notes: "used by suggestions"},
	{Directive: "MAXNGRAMSUGS", Status: supportSupported, Notes: "suggester option; ignored by core parser"},
	{Directive: "COMPOUNDMIN", Status: supportSupported, Notes: "compound checks supported"},
	{Directive: "ONLYINCOMPOUND", Status: supportSupported, Notes: "compound checks supported"},
	{Directive: "COMPOUNDRULE", Status: supportSupported, Notes: "compound checks supported"},
	{Directive: "COMPOUNDFLAG", Status: supportPartial, Notes: "compound flags supported for simple segmenting"},
	{Directive: "COMPOUNDPERMITFLAG", Status: supportPartial, Notes: "compound affix permit flags supported for simple segmenting"},
	{Directive: "COMPOUNDFORBIDFLAG", Status: supportPartial, Notes: "compound affix forbid flags supported for simple segmenting"},
	{Directive: "COMPOUNDBEGIN", Status: supportPartial, Notes: "compound begin flag supported for boundary checks"},
	{Directive: "COMPOUNDMIDDLE", Status: supportPartial, Notes: "compound middle flag supported; CHECKCOMPOUNDPATTERN flag checks use root dic flags for derived forms"},
	{Directive: "COMPOUNDEND", Status: supportPartial, Notes: "compound end flag supported for boundary checks"},
	{Directive: "CHECKCOMPOUNDCASE", Status: supportSupported, Notes: "uppercase-at-boundary checks supported; hyphenated forms rely on default BREAK splitting"},
	{Directive: "CHECKCOMPOUNDDUP", Status: supportSupported, Notes: "duplicate adjacent compound parts blocked"},
	{Directive: "CHECKCOMPOUNDPATTERN", Status: supportPartial, Notes: "simple compound boundary checks supported"},
	{Directive: "CHECKCOMPOUNDTRIPLE", Status: supportSupported, Notes: "triple-letter boundary checks supported (ASCII and Unicode)"},
	{Directive: "SIMPLIFIEDTRIPLE", Status: supportSupported, Notes: "simplified two-letter compound forms accepted"},
	{Directive: "CHECKSHARPS", Status: supportUnsupported, Notes: "sharp-s handling not implemented"},
	{Directive: "CIRCUMFIX", Status: supportUnsupported, Notes: "circumfix support not implemented"},
	{Directive: "COMPLEXPREFIXES", Status: supportUnsupported, Notes: "complex prefix handling not implemented"},
	{Directive: "FLAG", Status: supportPartial, Notes: "ASCII-style single-byte flags supported"},
	{Directive: "FORCEUCASE", Status: supportPartial, Notes: "compound-final capitalization supported"},
	{Directive: "FORBIDDENWORD", Status: supportSupported, Notes: "forbidden word and its affixed forms are blocked; homonym without flag rescues standalone use"},
	{Directive: "FULLSTRIP", Status: supportUnsupported, Notes: "full strip not implemented"},
	{Directive: "IGNORE", Status: supportUnsupported, Notes: "ignore-map handling not implemented"},
	{Directive: "KEEPCASE", Status: supportUnsupported, Notes: "keepcase handling not implemented"},
	{Directive: "LANG", Status: supportUnsupported, Notes: "language-specific rules not implemented"},
	{Directive: "MAP", Status: supportUnsupported, Notes: "character map not implemented"},
	{Directive: "NEEDAFFIX", Status: supportSupported, Notes: "virtual-stem flag supported for dic entries and affix outputs including prefix+suffix combinations"},
	{Directive: "CHECKCOMPOUNDREP", Status: supportSupported, Notes: "REP-based compound rejection supported"},
	{Directive: "NOSPLITSUGS", Status: supportPartial, Notes: "suggestion engine option; no effect on spell checking"},
	{Directive: "BREAK", Status: supportPartial, Notes: "default hyphen splitting supported; custom BREAK patterns accepted but not applied"},
	{Directive: "PFX", Status: supportSupported, Notes: "prefix rules supported"},
	{Directive: "SFX", Status: supportSupported, Notes: "suffix rules supported"},
	{Directive: "PHONE", Status: supportUnsupported, Notes: "phonetic suggestions not implemented"},
	{Directive: "PSEUDOROOT", Status: supportSupported, Notes: "deprecated alias of NEEDAFFIX"},
	{Directive: "AM", Status: supportUnsupported, Notes: "morph alias tables not implemented"},
	{Directive: "AF", Status: supportUnsupported, Notes: "affix flag alias tables not implemented"},
	{Directive: "NOSUGGEST", Status: supportSupported, Notes: "honored in checker/suggester"},
	{Directive: "WORDCHARS", Status: supportPartial, Notes: "used by CLI tokenization only"},
}

var hunspellSupportByDirective = func() map[string]hunspellSupportEntry {
	out := make(map[string]hunspellSupportEntry, len(hunspellSupportMatrix))
	for _, entry := range hunspellSupportMatrix {
		out[entry.Directive] = entry
	}
	return out
}()

func unsupportedHunspellFeatures(path string) ([]hunspellSupportEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var unsupported []hunspellSupportEntry
	scanner := bufio.NewScanner(f)
	firstLine := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstLine = false
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		directive := fields[0]
		entry, ok := hunspellSupportByDirective[directive]
		if !ok {
			continue
		}
		if entry.Status == supportUnsupported {
			unsupported = append(unsupported, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return unsupported, nil
}
