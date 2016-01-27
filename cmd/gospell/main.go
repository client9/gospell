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
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/client9/gospell"
	"github.com/client9/plaintext"
)

var (
	stdout *log.Logger // see below in init()
)

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

func init() {
	// we see it so it doesn't use a prefix or include a time stamp.
	stdout = log.New(os.Stdout, "", 0)
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

	splitter := gospell.NewSplitter(h.WordChars)

	// stdin support
	if len(args) == 0 {
		raw, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Unable to read Stdin: %s", err)
		}
		raw = plaintext.StripTemplate(raw)
		md, err := plaintext.ExtractorByFilename("stdin")
		if err != nil {
			log.Fatalf("Unable to create parser: %s", err)
		}
		rawstring := string(md.Text(raw))
		s := removeURL(rawstring)
		words := splitter.Split(s)
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
		md, err := plaintext.ExtractorByFilename(arg)
		if err != nil {
			log.Fatalf("Unable to create parser: %s", err)
		}
		raw = plaintext.StripTemplate(raw)
		rawstring := string(md.Text(raw))
		s := removeURL(rawstring)
		words := splitter.Split(s)
		for _, word := range words {
			if known := h.Spell(word); !known {
				stdout.Printf("%s\n", word)
				//stdout.Printf("%s:%d unknown %q", arg, linenum+1, word)
			}
		}
	}
}
