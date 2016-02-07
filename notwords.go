package gospell

import (
	"strings"
)

// Functions to remove non-words such as URLs, file paths, etc.


// This needs auditing as I believe it is wrong
func enURLChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' ||
		c == '_' ||
		c == '\\' ||
		c == '.' ||
		c == ':' ||
		c == ';' ||
		c == '/' ||
		c == '~' ||
		c == '%' ||
		c == '*' ||
		c == '$' ||
		c == '[' ||
		c == ']' ||
		c == '?' ||
		c == '#' ||
		c == '!'
}
func enNotURLChar(c rune) bool {
	return !enURLChar(c)
}

// RemoveURL attempts to strip away obvious URLs
//
func RemoveURL(s string) string {
	var idx int

	for {
		if idx = strings.Index(s, "http"); idx == -1 {
			return s
		}

		news := s[:idx]
		endx := strings.IndexFunc(s[idx:], enNotURLChar)
		if endx != -1 {
			news = news + " " + s[idx+endx:]
		}
		s = news
	}
}

