package main

import (
	"flag"
	"io"
	"log"
	"os"

	"github.com/client9/plaintext"
)

func main() {
	extension := flag.String("s", "", "over-ride file suffix to determine parser")
	flag.Parse()
	ext := *extension
	if ext != "" && ext[0] != '.' {
		ext = "." + ext
	}
	args := flag.Args()

	// stdin support
	if len(args) == 0 {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Unable to read Stdin: %s", err)
		}
		md, err := plaintext.ExtractorByFilename("stdin" + ext)
		if err != nil {
			log.Fatalf("Unable to create parser: %s", err)
		}

		raw = plaintext.StripTemplate(raw)
		if _, err := os.Stdout.Write(md.Text(raw)); err != nil {
			log.Fatalf("Unable to write: %s", err)
		}
	}

	for _, arg := range args {
		raw, err := os.ReadFile(arg)
		if err != nil {
			log.Fatalf("Unable to read %q: %s", arg, err)
		}
		md, err := plaintext.ExtractorByFilename(arg + ext)
		if err != nil {
			log.Fatalf("Unable to create parser: %s", err)
		}

		raw = plaintext.StripTemplate(raw)
		if _, err := os.Stdout.Write(md.Text(raw)); err != nil {
			log.Fatalf("Unable to write: %s", err)
		}
		if _, err := os.Stdout.Write([]byte{'\n'}); err != nil {
			log.Fatalf("Unable to write: %s", err)
		}
	}
}
