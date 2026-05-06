package gospell

import "testing"

func TestParseAffixPatternDot(t *testing.T) {
	// "." is the hunspell "always apply" sentinel — must return nil, not an error.
	m, err := parseAffixPattern(".", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil matcher for \".\", got %v", m)
	}
}

func TestParseAffixPatternBadClass(t *testing.T) {
	_, err := parseAffixPattern("[abc", false)
	if err == nil {
		t.Fatal("expected error for unclosed '[', got nil")
	}
}

func TestAffixMatcherSuffix(t *testing.T) {
	cases := []struct {
		pat  string
		word string
		want bool
	}{
		// literal suffix
		{"e", "love", true},
		{"e", "run", false},
		{"ed", "worked", true},
		{"ed", "work", false},

		// any-char suffix: ".e" matches any single char followed by 'e'
		{".e", "love", true}, // ends in "ve": 'v' matches '.', 'e' matches 'e'
		{".e", "the", true},  // ends in "he"
		{".e", "e", false},   // word shorter than pattern

		// negated class [^aeiou]y — must end in a consonant then 'y'
		// 'l' in "fly" is a consonant → match
		{"[^aeiou]y", "fly", true},
		// 'a' in "day" is a vowel → no match
		{"[^aeiou]y", "day", false},
		// 'a' in "play" is a vowel → no match
		{"[^aeiou]y", "play", false},
		// 'o' in "boy" is a vowel → no match
		{"[^aeiou]y", "boy", false},

		// inclusive class [aeiou]y — must end in a vowel then 'y'
		{"[aeiou]y", "boy", true},  // ends in "oy"
		{"[aeiou]y", "day", true},  // ends in "ay"
		{"[aeiou]y", "fly", false}, // ends in "ly", 'l' is not a vowel

		// word shorter than pattern
		{"ed", "e", false},
	}

	for _, tt := range cases {
		m, err := parseAffixPattern(tt.pat, false /* suffix */)
		if err != nil {
			t.Errorf("parse(%q): unexpected error: %v", tt.pat, err)
			continue
		}
		if m == nil {
			t.Errorf("parse(%q): unexpected nil matcher", tt.pat)
			continue
		}
		if got := m.MatchString(tt.word); got != tt.want {
			t.Errorf("suffix pattern %q against %q: got %v, want %v", tt.pat, tt.word, got, tt.want)
		}
	}
}

func TestAffixMatcherPrefix(t *testing.T) {
	cases := []struct {
		pat  string
		word string
		want bool
	}{
		// literal prefix
		{"re", "redefine", true},
		{"re", "define", false},
		{"un", "unhappy", true},
		{"un", "happy", false},

		// any-char prefix: ".n" — any char then 'n'
		{".n", "unable", true},  // starts with "un": 'u' matches '.', 'n' matches 'n'
		{".n", "orange", false}, // starts with "or": 'r' ≠ 'n'

		// inclusive class at word start
		{"[aeiou]", "apple", true}, // starts with 'a', a vowel
		{"[aeiou]", "fly", false},  // starts with 'f', a consonant

		// negated class at word start
		{"[^aeiou]", "fly", true},    // starts with 'f', a consonant
		{"[^aeiou]", "apple", false}, // starts with 'a', a vowel
	}

	for _, tt := range cases {
		m, err := parseAffixPattern(tt.pat, true /* prefix */)
		if err != nil {
			t.Errorf("parse(%q): unexpected error: %v", tt.pat, err)
			continue
		}
		if m == nil {
			t.Errorf("parse(%q): unexpected nil matcher", tt.pat)
			continue
		}
		if got := m.MatchString(tt.word); got != tt.want {
			t.Errorf("prefix pattern %q against %q: got %v, want %v", tt.pat, tt.word, got, tt.want)
		}
	}
}
