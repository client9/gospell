# Mutation-Based Suggestions

The mutation suggester is the lowest-overhead suggestion strategy in `gospell`.
It builds no inverted index or trigram table up front. Instead, each query
generates a bounded set of candidate words and checks them directly against the
dictionary.

## How It Works

For an input typo, the suggester generates one-edit candidates in this order:

1. delete one rune
2. transpose adjacent runes
3. substitute a rune with nearby QWERTY keys
4. substitute a rune with the rest of the English alphabet
5. insert a nearby QWERTY key
6. insert a rune from the English alphabet

Every generated candidate is checked with the dictionary's `HasWord` method.
Only real dictionary words survive.

## Why This Is Useful

This is a good fit when:

- startup time matters more than raw query throughput
- you want no precomputed index
- your dictionary is already available in memory
- the common errors are simple English typos

It handles the spelling mistakes people make most often:

- missing or extra letters
- doubled letters
- adjacent transpositions
- near-key substitutions on QWERTY keyboards

## Tradeoffs

- Fast to initialize
- Lower memory use than SymSpell or trigram indexing
- Slower per query than a precomputed index when the dictionary is large
- Less language-aware than Hunspell's full suggestion logic

## Design Note

This first pass intentionally avoids Hunspell-specific `TRY` and `REP`
machinery. It assumes English and a QWERTY keyboard, which keeps the
implementation small and predictable while still covering many practical typos.
