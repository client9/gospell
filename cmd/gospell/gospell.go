package main

import (
	"bytes"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	exitOnly := flag.Bool("e", false, "load dictionary and exit")

	// -path: colon-separated extra directories prepended to the standard
	// search path (DICPATH env var + system defaults). Leave empty to rely
	// solely on the standard path.
	dictPathFlag := flag.String("path", "", "extra colon-separated directories to search for dictionaries (prepended to DICPATH and system defaults)")

	// -d: comma-separated list of dictionary names, mirroring hunspell's -d.
	// First entry must be a base dictionary (.aff + .dic). Subsequent entries
	// may be base dictionaries (words merged as a WordList) or .dic-only
	// supplements.
	dicts := flag.String("d", "en_US", "comma-separated list of dictionaries to load (base first, then supplements)")

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

	// Build the ordered search path: explicit -path flag first, then
	// DICPATH + system defaults via gospell.SearchPaths().
	var searchPaths []string
	if *dictPathFlag != "" {
		searchPaths = append(searchPaths, filepath.SplitList(*dictPathFlag)...)
	}
	searchPaths = append(searchPaths, gospell.SearchPaths()...)

	dictNames := strings.Split(*dicts, ",")

	log.Printf("Loading %s", dictNames[0])
	timeStart := time.Now()
	base, err := gospell.Open(dictNames[0], searchPaths)
	if err != nil {
		log.Fatalf("Unable to load dictionary: %s", err)
	}
	log.Printf("Loaded in %v", time.Since(timeStart))

	if *exitOnly {
		return
	}

	checker := gospell.NewChecker(base)

	// Load supplemental dictionaries (index 1..n).
	for _, name := range dictNames[1:] {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		// Try as a full base dictionary first; fall back to .dic-only supplement.
		if extra, err := gospell.Open(name, searchPaths); err == nil {
			wl := &gospell.WordList{}
			extra.ForEachWord(func(w string) bool {
				wl.Add(w)
				return true
			})
			checker.AddWordList(wl)
		} else if wl, err := gospell.OpenSupplement(name, searchPaths); err == nil {
			checker.AddWordList(wl)
		} else {
			log.Fatalf("Unable to load supplemental dictionary %q: not found as base or supplement", name)
		}
	}

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
