# Hunspell Compatibility Checklist

This checklist tracks Hunspell features against the current `gospell` core.
The opt-in external fixture test uses the same support matrix to skip fixtures
that rely on unsupported directives.

## Core Parsing

- [x] `SET UTF-8`
- [x] `SET ISO8859-1`, `ISO8859-2`, `ISO8859-15`
- [x] `TRY`
- [x] `ICONV`
- [ ] `OCONV`
- [x] `REP`
- [x] `PFX`
- [x] `SFX`
- [x] `COMPOUNDMIN`
- [x] `ONLYINCOMPOUND`
- [x] `COMPOUNDRULE`
- [x] `NOSUGGEST`
- [ ] `FLAG` modes beyond single-byte ASCII-style flags
- [x] `WORDCHARS` for CLI tokenization

## Core Checking

- [x] exact word lookup
- [x] affix expansion
- [x] compound rules
- [x] case fallback
- [x] custom word lists
- [ ] `KEEPCASE`
- [ ] `FORBIDDENWORD`
- [ ] `NEEDAFFIX`
- [ ] `FULLSTRIP`
- [ ] `PSEUDOROOT`

## Compound / Language Rules

- [ ] `COMPOUNDFLAG`
- [ ] `COMPOUNDBEGIN`
- [ ] `COMPOUNDMIDDLE`
- [ ] `COMPOUNDEND`
- [ ] `CHECKCOMPOUNDCASE`
- [ ] `CHECKCOMPOUNDDUP`
- [ ] `CHECKCOMPOUNDPATTERN`
- [ ] `CHECKCOMPOUNDTRIPLE`
- [ ] `CHECKSHARPS`
- [ ] `CIRCUMFIX`
- [ ] `COMPLEXPREFIXES`
- [ ] `IGNORE`
- [ ] `LANG`
- [ ] `MAP`
- [ ] `PHONE`
- [ ] `BREAK`

## Suggestions

- [x] English/QWERTY mutation suggester
- [x] simplified n-gram fallback suggester
- [x] `MAXNGRAMSUGS` is accepted as a suggester option
- [ ] Hunspell-style suggestion parity
- [ ] phonetic suggestion rules

## Fixture Expectations

- `.good` and `.wrong` files are used for opt-in compatibility testing
- `.sug` files are intentionally ignored for now
- fixtures requiring unsupported directives are skipped with a reason
