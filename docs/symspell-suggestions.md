# SymSpell-Style Suggestions

`gospell` now includes a SymSpell-style suggester alongside the brute-force
Levenshtein and trigram-based engines.

## The Problem

The naive way to suggest corrections is to compare the query against every
dictionary word. That is easy to implement, but it scales linearly with the
dictionary size. For a large Hunspell dictionary, that means every typo incurs
a full scan.

SymSpell uses a different idea: instead of comparing the query to every word,
it builds a reverse index of delete forms.

## The Core Idea

If two words are within a small edit distance, then they usually share at
least one string formed by deleting a few characters. For example:

- `silly`
- `sillly`

Both words share a delete form such as `silly` once one `l` is removed from
the misspelling.

During indexing, each dictionary word is stored under every string that can be
created by deleting up to `MaxDistance` runes from the word prefix. During
lookup, the query generates the same family of delete strings. Any dictionary
word that lands on one of those keys becomes a candidate correction.

That turns suggestion lookup from “scan everything” into “probe a compact
reverse index, then score the few hits that come back”.

## Why It Works

The delete forms act like a lossy signature. They are not unique, and that is
fine. Their job is only to create a candidate set that is small enough to rank
cheaply with a true distance metric.

The final ranking step still uses Levenshtein distance, so the suggester keeps
the same intuitive score semantics as the brute-force engine. The delete index
is only a search accelerator.

## Why Use a Prefix

Classic SymSpell implementations limit deletes to a fixed prefix length. That
keeps the index smaller without losing much practical recall, because the first
few characters carry a lot of information and most English typos stay close to
the start of the word.

This implementation uses a default prefix length of 7 runes and a default edit
distance of 2. Those values keep the candidate explosion under control while
still covering the kinds of misspellings people usually expect suggestions for.

## Tradeoffs

- Faster lookup than a brute-force scan on larger dictionaries
- Smaller candidate set than n-gram overlap in many cases
- Higher memory use than a plain scan, because every word contributes multiple
  delete keys
- Best suited to small edit distances; the delete table grows quickly as the
  maximum distance increases

## When To Use It

Use the SymSpell-style suggester when you want:

- predictable low-latency suggestion lookup
- a simple engine with easy-to-understand scores
- a practical default for interactive typo correction

Use the trigram suggester if you want a different recall/latency tradeoff or a
candidate filter that behaves more like approximate string search. Use the
brute-force suggester if you want the simplest possible reference behavior.
