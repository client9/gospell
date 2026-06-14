package gospell

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const compareDict = "hunspell-en_US/en_US"

// hunspellSuggest runs hunspell -a against the local dictionary and returns
// a map of misspelled word → suggestion list. The test is skipped if hunspell
// is not installed or the local dictionary is missing.
func hunspellSuggest(t *testing.T, words []string) map[string][]string {
	t.Helper()
	bin, err := exec.LookPath("hunspell")
	if err != nil {
		t.Skip("hunspell not in PATH, skipping comparison test")
	}
	if _, err := os.Stat(compareDict + ".aff"); err != nil {
		t.Skipf("local dictionary %s not found", compareDict)
	}

	// Prefix every line with ^ so hunspell treats it as data, not a command.
	var sb strings.Builder
	sb.WriteString("^\n") // flush the version header
	for _, w := range words {
		sb.WriteString("^")
		sb.WriteString(w)
		sb.WriteString("\n")
	}

	cmd := exec.Command(bin, "-a", "-d", compareDict)
	cmd.Stdin = strings.NewReader(sb.String())
	out, _ := cmd.Output() // non-zero exit is normal when words are misspelled

	results := make(map[string][]string, len(words))
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "& ") {
			continue
		}
		// Format: & word count offset: sugg1, sugg2, ...
		rest := line[2:]
		colonIdx := strings.Index(rest, ": ")
		if colonIdx < 0 {
			continue
		}
		word := strings.Fields(rest[:colonIdx])[0]
		var suggs []string
		for _, s := range strings.Split(rest[colonIdx+2:], ", ") {
			if s = strings.TrimSpace(s); s != "" {
				suggs = append(suggs, s)
			}
		}
		results[word] = suggs
	}
	return results
}

func TestSuggestionsMatchHunspell(t *testing.T) {
	testWords := []string{
		"guidence",   // → guidance
		"alot",       // → a lot  (REP rule)
		"sillly",     // → silly
		"thier",      // → their
		"recieve",    // → receive
		"beleive",    // → believe
		"seperate",   // → separate
		"occured",    // → occurred
		"definately", // → definitely
		"accomodate", // → accommodate
		"writeing",   // writing
		"dissapoint",
		"usible",
		"wat",
		"greatful",
		"abcense",
		"aquire",
	}

	hunspellMap := hunspellSuggest(t, testWords)

	gs, err := NewGoSpell(compareDict+".aff", compareDict+".dic")
	if err != nil {
		t.Fatal(err)
	}

	const limit = 10

	// Header
	t.Logf("%-14s  %-16s  %5s  %s", "word", "hunspell #1", "rank", "gospell suggestions")
	t.Logf("%s", strings.Repeat("-", 72))

	for _, word := range testWords {
		hSuggs := hunspellMap[word]
		hsTop := ""
		if len(hSuggs) > 0 {
			hsTop = hSuggs[0]
		}

		gSuggs, err := gs.Suggest(word, limit)
		if err != nil {
			t.Errorf("%s: Suggest: %v", word, err)
			continue
		}
		var gWords []string
		for _, s := range gSuggs {
			gWords = append(gWords, s.Word)
		}

		rank := 0
		for i, s := range gWords {
			if strings.EqualFold(s, hsTop) {
				rank = i + 1
				break
			}
		}

		t.Logf("%-14s  %-16s  %5d  %s", word, hsTop, rank, strings.Join(gWords, ", "))

		if hsTop != "" && rank == 0 {
			t.Errorf("%s: hunspell top suggestion %q not found in gospell top %d", word, hsTop, limit)
		}
	}
}
