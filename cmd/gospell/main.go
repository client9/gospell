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
	"bytes"
	"flag"
	"github.com/client9/gospell"
	"github.com/client9/plaintext"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

var (
	stdout      *log.Logger // see below in init()
	defaultLog  *template.Template
	defaultWord *template.Template
	defaultLine *template.Template
)

const (
	defaultLogTmpl  = `{{ .Filename }}:{{ .LineNum }}:{{ js .Original }}`
	defaultWordTmpl = `{{ .Original }}`
	defaultLineTmpl = `{{ .Line }}`
)

func init() {
	// we see it so it doesn't use a prefix or include a time stamp.
	stdout = log.New(os.Stdout, "", 0)
	defaultLog = template.Must(template.New("defaultLog").Parse(defaultLogTmpl))
	defaultWord = template.Must(template.New("defaultWord").Parse(defaultWordTmpl))
	defaultLine = template.Must(template.New("defaultLine").Parse(defaultLineTmpl))
}

type diff struct {
	Filename string
	Path     string
	Original string
	Line     string
	LineNum  int
}

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

func process(gs *gospell.GoSpell, fullpath string, raw []byte) {
	md, err := plaintext.ExtractorByFilename(fullpath)
	if err != nil {
		log.Fatalf("Unable to create parser: %s", err)
	}
	// remove any golang templates
	raw = plaintext.StripTemplate(raw)

	// extract plain text
	raw = md.Text(raw)

	// do character conversion "smart quotes" to quotes, etc
	// as specified in the Affix file
	rawstring := gs.InputConversion(raw)

	// zap URLS
	s := removeURL(rawstring)

	for linenum, line := range strings.Split(s, "\n") {
		// now get words
		words := gs.Split(line)
		for _, word := range words {
			// HACK
			word = strings.Trim(word, "'")
			if known := gs.Spell(word); !known {
				var output bytes.Buffer
				defaultLog.Execute(&output, diff{
					Filename: filepath.Base(fullpath),
					Path:     fullpath,
					Line:     line,
					LineNum:  linenum + 1,
					Original: word,
				})
				// goroutine-safe print to os.Stdout
				stdout.Println(output.String())
			}
		}
	}
}

func main() {
	format := flag.String("f", "", "use Golang template for log message")

	// TODO based on OS (windows vs. linux)
	dictPath := flag.String("path", ".:/usr/local/share/hunspell:/usr/share/hunspell", "Search path for dictionaries")

	// TODO based on ENV settings
	dicts := flag.String("d", "en_US", "dictionaries to load")

	listOnly := flag.Bool("l", false, "only print unknown word")
	lineOnly := flag.Bool("L", false, "print line with unknown word")
	flag.Parse()
	args := flag.Args()

	if *listOnly {
		defaultLog = defaultWord
	}

	if *lineOnly {
		defaultLog = defaultLine
	}

	if len(*format) > 0 {
		t, err := template.New("custom").Parse(*format)
		if err != nil {
			log.Fatalf("Unable to compile log format: %s", err)
		}
		defaultLog = t
	}

	affFile := ""
	dicFile := ""
	for _, base := range filepath.SplitList(*dictPath) {
		affFile = filepath.Join(base, *dicts+".aff")
		dicFile = filepath.Join(base, *dicts+".dic")
		//log.Printf("Trying %s", affFile)
		_, err1 := os.Stat(affFile)
		_, err2 := os.Stat(dicFile)
		if err1 == nil && err2 == nil {
			break
		}
		affFile = ""
		dicFile = ""
	}

	if affFile == "" {
		log.Fatalf("Unable to load %s", *dicts)
	}

	log.Printf("Loading %s %s", affFile, dicFile)
	timeStart := time.Now()
	h, err := gospell.NewGoSpell(affFile, dicFile)
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
		process(h, "stdin", raw)
	}
	for _, arg := range args {
		raw, err := ioutil.ReadFile(arg)
		if err != nil {
			log.Fatalf("Unable to read %q: %s", arg, err)
		}
		process(h, arg, raw)
	}
}
