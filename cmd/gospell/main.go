package main

// email
// [separator]a-zA-Z0-9+@domain.com[separator]
// http[s]://   [separator]
/*
   } else if (! (is_wordchar(line[actual] + url_head) ||
     (ch == '-') || (ch == '_') || (ch == '\\') ||
     (ch == '.') || (ch == ':') || (ch == '/') ||
     (ch == '~') || (ch == '%') || (ch == '*') ||
     (ch == '$') || (ch == '[') || (ch == ']') ||
     (ch == '?') || (ch == '!') ||
     ((ch >= '0') && (ch <= '9')))) {
*/

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/client9/gospell"
	"github.com/client9/plaintext"
)

var (
	stdout *log.Logger // see below in init()
)

// Needs to be replaced based on AFF file
// enWordChar is true if [a-zA-Z0-9]
func enWordChar(c rune) bool {
	//	return c < 255 && (c == 0x27 || unicode.IsLetter(c))// || unicode.IsNumber(c))
	return c < 255 && unicode.IsLetter(c) // || unicode.IsNumber(c))
}
func enNotWordChar(c rune) bool {
	return !enWordChar(c)
}

// This needs auditing as I believe it is wrong
func enURLChar(c rune) bool {
	return enWordChar(c) ||
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

func removeURL(s string) string {
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

func split(s string) []string {
	s = removeURL(s)
	words := strings.FieldsFunc(s, enNotWordChar)
	if false {
		fmt.Printf("%v\n", words)
	}
	return words
}

func init() {
	// we see it so it doesn't use a prefix or include a time stamp.
	stdout = log.New(os.Stdout, "", 0)
}

func getSuffix(filename string) string {
	idx := strings.LastIndex(filename, ".")
	if idx == -1 || idx+1 == len(filename) {
		return ""
	}
	return filename[idx+1:]
}

func getExtractor(filename string) (plaintext.Extractor, error) {
	var e plaintext.Extractor
	var err error
	switch getSuffix(filename) {
	case "md":
		e, err = plaintext.NewMarkdownText()
	case "html":
		e, err = plaintext.NewHTMLText()
	case "go", "h", "c", "java":
		e, err = plaintext.NewGolangText()
	default:
		e, err = plaintext.NewIdentity()
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

func main() {
	flag.Parse()
	args := flag.Args()

	aff := "/usr/local/share/hunspell/en_US.aff"
	dic := "/usr/local/share/hunspell/en_US.dic"
	timeStart := time.Now()
	h, err := gospell.NewGoSpell(aff, dic)
	timeEnd := time.Now()

	// note: 10x too slow
	log.Printf("Loaded in %v", timeEnd.Sub(timeStart))

	if err != nil {
		log.Fatalf("%s", err)
	}

	// stdin support
	if len(args) == 0 {
		raw, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Unable to read Stdin: %s", err)
		}
		raw = plaintext.StripTemplate(raw)
		md, err := getExtractor("foo.txt")
		if err != nil {
			log.Fatalf("Unable to create parser: %s", err)
		}
		rawstring := string(md.Text(raw))
		words := split(rawstring)
		for _, word := range words {
			if known := h.Spell(word); !known {
				stdout.Printf("%s\n", word)
			}
		}
	}
	for _, arg := range args {
		raw, err := ioutil.ReadFile(arg)
		if err != nil {
			log.Fatalf("Unable to read %q: %s", arg, err)
		}
		raw = plaintext.StripTemplate(raw)
		md, err := getExtractor(arg)
		if err != nil {
			log.Fatalf("Unable to create parser: %s", err)
		}
		rawstring := string(md.Text(raw))
		words := split(rawstring)
		for _, word := range words {
			if known := h.Spell(word); !known {
				stdout.Printf("%s\n", word)
				//stdout.Printf("%s:%d unknown %q", arg, linenum+1, word)
			}
		}
	}
}
