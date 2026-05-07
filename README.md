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
- pluggable suggestion engines

The package keeps the core checker small and lets you choose the suggestion strategy that fits your workload.

## Features

- Pure Go implementation
- Hunspell dictionary format support
- Exact spell checking with affix and compound handling
- `WordList` overlays — add allowed or forbidden words without touching the base dictionary
- `Checker` — combines a base dictionary with any number of `WordList`s; supports per-document reset at zero cost
- Pluggable suggestion engines
- Built-in reference engines:
  - brute-force Levenshtein
  - English/QWERTY mutation suggester
  - SymSpell-style delete index
  - hashed trigram index with Levenshtein rerank

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
	gs, err := gospell.NewGoSpell("hunspell-en_US-2026/en_US.aff", "hunspell-en_US-2026/en_US.dic")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(gs.Spell("silly"))
	fmt.Println(gs.Spell("sillly"))
}
```

## Suggestions

Suggestions are provided by a pluggable engine.

```go
gs, err := gospell.NewGoSpell("hunspell-en_US-2026/en_US.aff", "hunspell-en_US-2026/en_US.dic")
if err != nil {
	log.Fatal(err)
}

if err := gs.SetSuggester(gospell.NewLevenshteinSuggester(
	gospell.LevenshteinOptions{MaxDistance: 2},
)); err != nil {
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

For a faster indexed strategy:

```go
if err := gs.SetSuggester(gospell.NewTrigramSuggester(
	gospell.TrigramOptions{
		RerankLimit:   32,
		MaxLengthDiff: 4,
	},
)); err != nil {
	log.Fatal(err)
}
```

For a zero-index strategy that generates English/QWERTY mutations on the fly:

```go
if err := gs.SetSuggester(gospell.NewMutationSuggester(
	gospell.MutationOptions{CandidateCap: 256},
)); err != nil {
	log.Fatal(err)
}
```

For a delete-based strategy that usually sits between brute force and the
trigram index in cost:

```go
if err := gs.SetSuggester(gospell.NewSymSpellSuggester(
	gospell.SymSpellOptions{
		MaxDistance:  2,
		PrefixLength: 7,
	},
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

- brute-force distance scoring
- query-time mutation generation
- delete-index candidate generation
- n-gram or trigram indexing
- chained or composite engines

### Built-In Suggesters

`gospell` currently ships with four reference suggestion engines:

- `NewLevenshteinSuggester` - scans every dictionary word and ranks by edit distance
- `NewMutationSuggester` - generates English/QWERTY one-edit candidates on demand
- `NewSymSpellSuggester` - delete-index engine with expensive build and fast lookup
- `NewTrigramSuggester` - trigram overlap filter followed by Levenshtein rerank

Defaults:

- `LevenshteinOptions{MaxDistance: 2}` when nonzero; `0` disables the cap
- `MutationOptions{CandidateCap: 256}`
- `SymSpellOptions{MaxDistance: 2, PrefixLength: 7}`
- `TrigramOptions{RerankLimit: 32, MaxLengthDiff: 4}`

### Mutation-Based Suggestions

The mutation suggester takes the misspelled word, generates a bounded set of
single-edit candidates, and checks each candidate against the dictionary. It
does not build an index up front.

The current first pass assumes English and a QWERTY keyboard:

- deletions and adjacent transpositions are always generated
- substitutions prefer nearby keyboard keys first, then fall back to the
  English alphabet
- insertions use nearby keyboard keys first, then the English alphabet

That keeps startup cost near zero while still finding common typos such as
missing letters, doubled letters, transpositions, and near-key substitutions.

See [`docs/mutation-suggestions.md`](/Users/nickg/projects/gospell/docs/mutation-suggestions.md)
for a longer explanation of the approach and tradeoffs.

### SymSpell-Style Suggestions

The delete-index engine stores each dictionary word under the strings that can
be formed by deleting up to `MaxDistance` runes from the word prefix. A query
word generates the same family of deletes, and matching delete strings recover
a much smaller candidate set than scanning the full dictionary.

That works because nearby misspellings tend to share at least one delete form.
For example, if `sillly` should be `silly`, both words share the delete form
`silly`. Once the candidate set is small, the engine reranks with a true
Levenshtein distance so the final ordering is easy to interpret.

See [`docs/symspell-suggestions.md`](/Users/nickg/projects/gospell/docs/symspell-suggestions.md)
for a longer explanation of the data structure and tradeoffs.

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
- [`NewChecker`](https://pkg.go.dev/github.com/client9/gospell#NewChecker) — runtime query API wrapping a base dictionary
- [`(*Checker).Spell`](https://pkg.go.dev/github.com/client9/gospell#Checker.Spell) — spell check against base + all active WordLists
- [`(*Checker).Suggest`](https://pkg.go.dev/github.com/client9/gospell#Checker.Suggest) — suggestions from base + WordLists
- [`(*Checker).AddWordList`](https://pkg.go.dev/github.com/client9/gospell#Checker.AddWordList) / [`RemoveWordList`](https://pkg.go.dev/github.com/client9/gospell#Checker.RemoveWordList)
- [`NewWordList`](https://pkg.go.dev/github.com/client9/gospell#NewWordList) / [`NewWordListFile`](https://pkg.go.dev/github.com/client9/gospell#NewWordListFile)
- [`(*GoSpell).Spell`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.Spell) — direct base-only check (no WordLists)
- [`(*GoSpell).SetSuggester`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.SetSuggester) / [`(*GoSpell).Suggest`](https://pkg.go.dev/github.com/client9/gospell#GoSpell.Suggest)
- [`NewMutationSuggester`](https://pkg.go.dev/github.com/client9/gospell#NewMutationSuggester)
- [`NewSymSpellSuggester`](https://pkg.go.dev/github.com/client9/gospell#NewSymSpellSuggester)

## Hunspell Compatibility

This package understands the Hunspell dictionary format and supports:

- affix expansion
- compound rules
- iconv conversions
- case handling
- custom word lists

For a feature checklist and compatibility matrix, see
[`docs/hunspell-compatibility.md`](/Users/nickg/projects/gospell/docs/hunspell-compatibility.md).

## Dictionary Files

The repository includes an English Hunspell dictionary used by tests and benchmarks:

- `hunspell-en_US-2026/en_US.aff`
- `hunspell-en_US-2026/en_US.dic`

## Contributing

This project is still evolving. If you find a bug, a mismatch with Hunspell behavior, or a better suggestion strategy, patches are welcome.

## License

[MIT](/LICENSE)
