# Mutation-Based Suggestions

The mutation suggester is the built-in suggestion strategy in `gospell`.
It builds no materialized dictionary up front. Instead, each query generates a
bounded set of candidate words and checks them directly against the lazy
dictionary.

## How It Works

For an input typo, the suggester generates one-edit candidates in this order:

1. delete one rune
2. transpose adjacent runes
3. substitute a rune with nearby QWERTY keys
4. substitute a rune with the rest of the English alphabet
5. insert a nearby QWERTY key
6. insert a rune from the English alphabet

Every generated candidate is checked with the dictionary's `Spell` method, so
affixed words and compound forms can be suggested without precomputing every
surface form.

If the mutation pass finds no candidates, the suggester scans root dictionary
entries using a simplified n-gram score, keeps the best roots, expands only
those roots, and reranks the expanded candidates.

## Why This Is Useful

This is a good fit when:

- startup time matters more than raw query throughput
- you want no full surface materialization
- your dictionary is loaded lazily
- the common errors are simple English typos

It handles the spelling mistakes people make most often:

- missing or extra letters
- doubled letters
- adjacent transpositions
- near-key substitutions on QWERTY keyboards

## Tradeoffs

- Fast to initialize
- Lower memory use than materialized or indexed candidate sets
- N-gram fallback scans roots and is slower than the one-edit mutation pass
- Less language-aware than Hunspell's full suggestion logic

## Design Note

The first pass uses Hunspell `TRY` characters when available, plus English and
QWERTY-keyboard mutations. This keeps the implementation small and predictable
while still covering many practical typos.
