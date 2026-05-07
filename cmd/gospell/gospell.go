package main

import (
	"bytes"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/client9/gospell"
)

var (
	stdout      *log.Logger
	defaultLog  *template.Template
	defaultWord *template.Template
	defaultLine *template.Template
)

const (
	defaultLogTmpl  = `{{ .Path }}:{{ .LineNum }}:{{ js .Original }}`
	defaultWordTmpl = `{{ .Original }}`
	defaultLineTmpl = `{{ .Line }}`
)

func init() {
	stdout = log.New(os.Stdout, "", 0)
	defaultLog = template.Must(template.New("defaultLog").Parse(defaultLogTmpl))
	defaultWord = template.Must(template.New("defaultWord").Parse(defaultWordTmpl))
	defaultLine = template.Must(template.New("defaultLine").Parse(defaultLineTmpl))
}

func main() {
	format := flag.String("f", "", "use Golang template for log message")
	listOnly := flag.Bool("l", false, "only print unknown word")
	lineOnly := flag.Bool("L", false, "print line with unknown word")

	// for testing load time
	exitOnly := flag.Bool("e", false, "load dictionary and exit")

	dictPath := flag.String("path", ".:/usr/local/share/hunspell:/usr/share/hunspell", "Search path for dictionaries")
	dicts := flag.String("d", "en_US", "dictionaries to load")
	personalDict := flag.String("p", "", "personal wordlist file")

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
	if err != nil {
		log.Fatalf("%s", err)
	}
	log.Printf("Loaded in %v", time.Since(timeStart))

	if *exitOnly {
		return
	}

	checker := gospell.NewChecker(h)
	if *personalDict != "" {
		wl, err := gospell.NewWordListFile(*personalDict)
		if err != nil {
			log.Fatalf("Unable to load personal dictionary %s: %s", *personalDict, err)
		}
		checker.AddWordList(wl)
	}

	printDiffs := func(diffs []Diff) {
		for _, diff := range diffs {
			buf := bytes.Buffer{}
			if err := defaultLog.Execute(&buf, diff); err != nil {
				log.Printf("template error: %s", err)
			}
			stdout.Println(buf.String())
		}
	}

	if len(args) == 0 {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Unable to read stdin: %s", err)
		}
		out := SpellFile(checker, raw)
		for i := range out {
			out[i].Path = "stdin"
		}
		printDiffs(out)
		return
	}

	for _, arg := range args {
		if f, err := os.Stat(arg); err != nil || f.IsDir() {
			continue
		}
		raw, err := os.ReadFile(arg)
		if err != nil {
			log.Fatalf("Unable to read %q: %s", arg, err)
		}
		out := SpellFile(checker, raw)
		for i := range out {
			out[i].Path = arg
		}
		printDiffs(out)
	}
}
