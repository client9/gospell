package gospell

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHunspellFixtures is an opt-in integration test for external Hunspell
// fixture directories.
//
// Set GOSPELL_HUNSPELL_FIXTURES to the root of a Hunspell tests directory that
// contains matching .aff/.dic pairs and optional .good/.wrong files.
func TestHunspellFixtures(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("GOSPELL_HUNSPELL_FIXTURES"))
	if root == "" {
		t.Skip("set GOSPELL_HUNSPELL_FIXTURES to run Hunspell fixture tests")
	}

	var affFiles []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".aff") {
			affFiles = append(affFiles, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk fixture root: %v", err)
	}

	if len(affFiles) == 0 {
		t.Fatalf("no .aff files found in %q", root)
	}

	for _, affPath := range affFiles {
		base := strings.TrimSuffix(affPath, ".aff")
		dicPath := base + ".dic"
		goodPath := base + ".good"
		wrongPath := base + ".wrong"
		if _, err := os.Stat(goodPath); os.IsNotExist(err) {
			if _, err := os.Stat(wrongPath); os.IsNotExist(err) {
				continue
			}
		}

		t.Run(filepath.Base(base), func(t *testing.T) {
			if unsupported, err := unsupportedHunspellFeatures(affPath); err != nil {
				t.Fatalf("scan aff directives: %v", err)
			} else if len(unsupported) > 0 {
				var parts []string
				for _, entry := range unsupported {
					parts = append(parts, fmt.Sprintf("%s (%s)", entry.Directive, entry.Notes))
				}
				t.Skipf("unsupported Hunspell directives: %s", strings.Join(parts, ", "))
			}

			affF, err := os.Open(affPath)
			if err != nil {
				t.Fatalf("open aff %q: %v", affPath, err)
			}
			defer func() { _ = affF.Close() }()

			dicF, err := os.Open(dicPath)
			if err != nil {
				t.Fatalf("open dic %q: %v", dicPath, err)
			}
			defer func() { _ = dicF.Close() }()

			gs, err := NewGoSpellReader(affF, dicF)
			if err != nil {
				t.Fatalf("load dictionary: %v", err)
			}

			if err := checkFixtureWords(t, gs, goodPath, true); err != nil {
				t.Fatal(err)
			}
			if err := checkFixtureWords(t, gs, wrongPath, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func checkFixtureWords(t *testing.T, gs *GoSpell, path string, want bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open fixture list %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		word := strings.TrimSpace(scanner.Text())
		if word == "" || strings.HasPrefix(word, "#") {
			continue
		}
		if got := gs.Spell(word); got != want {
			return fmt.Errorf("%s:%d: %q got %v want %v", path, lineNum, word, got, want)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan fixture list %q: %w", path, err)
	}
	return nil
}
