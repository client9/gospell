# gospell

[![Go Reference](https://pkg.go.dev/badge/github.com/client9/gospell.svg)](https://pkg.go.dev/github.com/client9/gospell)
[![Build Status](https://github.com/client9/gospell/actions/workflows/go.yml/badge.svg)](https://github.com/client9/gospell/actions)
[![license](https://img.shields.io/badge/license-MIT-blue.svg?style=flat)](https://raw.githubusercontent.com/client9/gospell/main/LICENSE)

`gospell` is a pure Go spell checker for Hunspell-style dictionaries.

It is designed for:

- loading Hunspell `.aff` and `.dic` files
- exact word checking
- word splitting and compound handling
- per-document or per-session word list overlays
- lazy mutation-based spelling suggestions

The package keeps the core checker small and uses lazy lookup for both spelling and suggestions.

## Features

- Pure Go implementation
- Hunspell dictionary format support
- Exact spell checking with affix and compound handling
- `WordList` overlays — add allowed or forbidden words without touching the base dictionary
- `Checker` — combines a base dictionary with any number of `WordList`s; supports per-document reset at zero cost
- Built-in English/QWERTY mutation suggester with n-gram fallback

## Install

```bash
go get github.com/client9/gospell
```

## Basic Use

```go
package main

import (
	"fmt"
	"log"

	"github.com/client9/gospell"
)

func main() {
	gs, err := gospell.NewGoSpell("hunspell-en_US/en_US.aff", "hunspell-en_US/en_US.dic")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(gs.Spell("silly"))
	fmt.Println(gs.Spell("sillly"))
}
```

## Suggestions

Suggestions use the built-in mutation suggester by default. The suggester
generates common typo mutations first, then falls back to an n-gram root scan
that expands only the best matching roots.

```go
gs, err := gospell.NewGoSpell("hunspell-en_US/en_US.aff", "hunspell-en_US/en_US.dic")
if err != nil {
	log.Fatal(err)
}

sugs, err := gs.Suggest("sillly", 5)
if err != nil {
	log.Fatal(err)
}
for _, sug := range sugs {
	fmt.Println(sug.Word, sug.Score)
}
```

You can still replace the default suggester if you provide your own engine:

```go
if err := gs.SetSuggester(gospell.NewMutationSuggester(
	gospell.MutationOptions{CandidateCap: 256},
)); err != nil {
	log.Fatal(err)
}
```

### Suggestion Engines

`gospell` ships with a small interface so you can swap suggestion strategies without changing the checker:

```go
type Suggestions interface {
	Init(src SuggestionSource) error
	Suggest(word string, limit int) ([]Suggestion, error)
}
```

This makes it easy to experiment with:

- query-time mutation generation
- n-gram fallback scoring
- chained or composite engines

### Built-In Suggesters

`gospell` currently ships with one built-in suggestion engine:

- `NewMutationSuggester` - generates English/QWERTY candidates on demand and falls back to n-gram root matching

Defaults:

- `MutationOptions{CandidateCap: 256, NGramRootCap: 64}`

### Mutation-Based Suggestions

The mutation suggester takes the misspelled word, generates a bounded set of
single-edit candidates, and checks each candidate with the dictionary's lazy
`Spell` method. If no mutation survives, it scans root dictionary entries by
n-gram similarity, expands only the best roots, and reranks those candidates.
It does not materialize all dictionary surfaces.

The current first pass assumes English and a QWERTY keyboard:

- deletions and adjacent transpositions are always generated
- substitutions prefer nearby keyboard keys first, then fall back to the
  English alphabet
- insertions use nearby keyboard keys first, then the English alphabet

That keeps startup cost near zero while still finding common typos such as
missing letters, doubled letters, transpositions, and near-key substitutions.

See [`docs/mutation-suggestions.md`](docs/mutation-suggestions.md)
for a longer explanation of the approach and tradeoffs.

## Multiple Dictionaries

A common pattern is to combine a base dictionary with domain-specific vocabulary (medical, legal, technical) that shares the same affix rules. `AddDictionaryReader` and `AddDictionaryFile` merge an additional `.dic` file into an existing `GoSpell`, applying the same affix expansion as the base — so entries like `widget/S` produce both "widget" and "widgets".

This is the key difference from `OpenSupplement`/`NewWordListFromDic`, which strip affix flags and only recognize bare stems.

```go
gs, err := gospell.NewGoSpell("en_US.aff", "en_US.dic")
if err != nil {
    log.Fatal(err)
}

// Merge a domain-specific dictionary reusing the en_US affix rules.
if err := gs.AddDictionaryFile("medical.dic"); err != nil {
    log.Fatal(err)
}

// Or use AddDic for path-based search:
if err := gospell.AddDic(gs, "medical", searchPaths); err != nil {
    log.Fatal(err)
}
```

These are load-time operations; call them before using `Spell` or `Suggest` from multiple goroutines.

## Word Lists

`WordList` is a lightweight overlay of allowed and forbidden words. It does not require rebuilding the base dictionary and can be attached or detached at any time.

Format: one entry per line. Lines starting with `#` are comments. Lines starting with `*` forbid the word. All other non-blank lines allow the word.

```
# project-specific terms
Kubernetes
gRPC
*irregardless
```

```go
gs, err := gospell.NewGoSpell("en_US.aff", "en_US.dic")
if err != nil {
    log.Fatal(err)
}

checker := gospell.NewChecker(gs)

// Load a global personal word list once.
global, err := gospell.NewWordListFile("personal.txt")
if err != nil {
    log.Fatal(err)
}
checker.AddWordList(global)

// Per-document: add, use, then remove.
doc, _ := gospell.NewWordList(strings.NewReader("ProjectName\n*badterm\n"))
checker.AddWordList(doc)

fmt.Println(checker.Spell("ProjectName")) // true
fmt.Println(checker.Spell("badterm"))     // false

checker.RemoveWordList(doc) // reset for next document
```

`Checker.Suggest` merges base dictionary suggestions with a brute-force scan of all active `WordList`s, so per-document words appear in suggestions automatically.

## API Overview

The main entry points are:

- [`NewGoSpell`](https://pkg.go.dev/github.com/client9/gospell#NewGoSpell) / [`NewGoSpellReader`](https://pkg.go.dev/github.com/client9/gospell#NewGoSpellReader) — load a base dictionary
- [`(*GoSpell).AddDictionaryFile`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.AddDictionaryFile) / [`AddDictionaryReader`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.AddDictionaryReader) — merge additional `.dic` files with full affix expansion
- [`AddDic`](https://pkg.go.dev/github.com/client9/gospell#AddDic) — path-search wrapper for `AddDictionaryFile`
- [`NewChecker`](https://pkg.go.dev/github.com/client9/gospell#NewChecker) — runtime query API wrapping a base dictionary
- [`(*Checker).Spell`](https://pkg.go.dev/github.com/client9/gospell#Checker.Spell) — spell check against base + all active WordLists
- [`(*Checker).Suggest`](https://pkg.go.dev/github.com/client9/gospell#Checker.Suggest) — suggestions from base + WordLists
- [`(*Checker).AddWordList`](https://pkg.go.dev/github.com/client9/gospell#Checker.AddWordList) / [`RemoveWordList`](https://pkg.go.dev/github.com/client9/gospell#Checker.RemoveWordList)
- [`NewWordList`](https://pkg.go.dev/github.com/client9/gospell#NewWordList) / [`NewWordListFile`](https://pkg.go.dev/github.com/client9/gospell#NewWordListFile)
- [`(*GoSpell).Spell`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.Spell) — direct base-only check (no WordLists)
- [`(*GoSpell).Suggest`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.Suggest)
- [`NewMutationSuggester`](https://pkg.go.dev/github.com/client9/gospell#NewMutationSuggester)

## Hunspell Compatibility

This package understands the Hunspell dictionary format and supports:

- affix expansion
- compound rules
- iconv conversions
- case handling
- custom word lists

For a feature checklist and compatibility matrix, see
[`docs/hunspell-compatibility.md`](docs/hunspell-compatibility.md).

## Dictionary Files

The repository includes an English Hunspell dictionary used by tests and benchmarks:

- `hunspell-en_US/en_US.aff`
- `hunspell-en_US/en_US.dic`

## Contributing

This project is still evolving. If you find a bug, a mismatch with Hunspell behavior, or a better suggestion strategy, patches are welcome.

## License

[MIT](/LICENSE)
