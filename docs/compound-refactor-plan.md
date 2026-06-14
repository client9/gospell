# Compound Refactor Plan

This document is the working plan for fixing Hunspell compound behavior in
`gospell`.

The current implementation has accumulated too many responsibilities in one
path:

- affix expansion
- homonym handling
- standalone spell acceptance
- compound eligibility
- compound start/middle/end positioning
- `ONLYINCOMPOUND`
- `COMPOUNDFLAG` / `COMPOUNDPERMITFLAG` / `COMPOUNDFORBIDFLAG`
- `CHECKCOMPOUNDPATTERN`
- `COMPOUNDBEGIN` / `COMPOUNDMIDDLE` / `COMPOUNDEND`

The result is that fixing one fixture can easily break another.

## Goal

Make compound behavior explicit and testable by separating:

1. what surfaces exist
2. whether a surface spells standalone
3. whether a surface can participate in compounds
4. where a surface can appear in a compound
5. which compound boundaries are blocked

## Step 1: Introduce a surface model

Stop treating dictionary output as plain strings only.

Each generated surface should carry metadata such as:

- `word`
- `standaloneAllowed`
- `compoundStartAllowed`
- `compoundMiddleAllowed`
- `compoundEndAllowed`
- `compoundForbidden`
- `onlyInCompound`
- raw flags

This does not mean changing every caller at once. It means the expansion path
should be able to produce records, not just strings.

## Step 2: Separate exact lookup from compound lookup

The checker should answer two different questions:

- Is this surface allowed as a standalone word?
- If not, can it still be accepted as part of a compound?

These must remain independent.

Expected behavior:

- A word may be rejected standalone but accepted in compounds.
- A word may be accepted standalone but blocked inside compounds.
- A word may have multiple homonyms with different compound behavior.

## Step 3: Make `ONLYINCOMPOUND` explicit

`ONLYINCOMPOUND` is the main source of ambiguity right now.

It needs to be handled in two distinct cases:

- root dictionary entries marked `ONLYINCOMPOUND`
- affix-generated surfaces that inherit compound-only behavior

The plan is:

- keep the root surface behavior explicit
- do not infer standalone rejection from unrelated maps
- do not let compound-only status propagate silently through every derived form

## Step 4: Preserve homonyms as separate records

The `1592880` case shows why this matters:

- `weg/Qoz`
- `weg/P`

Same spelling, different behavior.

That means the loader needs to preserve per-entry metadata, not collapse
everything into one spelling-level boolean.

## Step 5: Make compound position checks a separate pass

Compound checking should be a second step after surfaces are known.

The compound pass should:

1. split the candidate word into parts
2. look up each part as a surface or surface set
3. verify positional constraints:
   - start part
   - middle part
   - end part
4. apply `CHECKCOMPOUNDPATTERN`
5. apply `COMPOUNDRULE`
6. apply case and length restrictions

This pass should not mutate dictionary state.

## Step 6: Keep boundary flags positional

The boundary-related directives should be represented as surface metadata:

- `COMPOUNDBEGIN`
- `COMPOUNDMIDDLE`
- `COMPOUNDEND`

Likewise, the compound-flag family should remain explicit:

- `COMPOUNDFLAG`
- `COMPOUNDPERMITFLAG`
- `COMPOUNDFORBIDFLAG`

These should affect surface records, not just be merged into flat string maps.

## Step 7: Rebuild the tests around fixture cases

Each of these should have a focused regression test:

- `onlyincompound`
- `onlyincompound2`
- `1592880`
- `digits_in_words`
- `checkcompoundpattern`
- `checkcompoundpattern2`
- `checkcompoundpattern3`
- `checkcompoundpattern4`
- `compoundaffix`
- `compoundforbid`

The tests should assert the smallest useful behavior:

- exact standalone rejection
- exact compound acceptance
- exact boundary blocking
- exact homonym handling

## Step 8: Keep the model small until it stabilizes

Do not add more Hunspell features into the compound path until the surface
model is stable.

The current failure mode is not lack of feature coverage.
It is that one path is doing too much at once.

## Suggested implementation order

1. Define a surface record type.
2. Change expansion to emit surface records, not just strings.
3. Update exact spell lookup to use surface metadata.
4. Update compound lookup to use surface metadata.
5. Rework `ONLYINCOMPOUND` handling with homonyms in mind.
6. Re-run the targeted compound regression tests.
7. Only then add more compound directives.

## Success criteria

The refactor is done when:

- `onlyincompound` and `onlyincompound2` both pass
- `1592880` passes
- compound pattern fixtures pass
- no fixture is fixed by adding a broad special-case that breaks another
  fixture

