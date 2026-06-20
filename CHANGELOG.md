# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

## [v0.9.2] - 2026-16-019

## Added

- Multiple ".dic" support.

## Changed

- Field order in struct `MutationSuggester` changed with [betteralign](https://github.com/dkorunic/betteralign) to save memory.
 
## Fixed

- Hunspell 353 - ignore unicode numbers not just [0-9]
- improved code coverage

## [0.9.1] - 2026-06-15

- Same API
- Reworked to use "lazy lookup" - loading dictionaries is 10x faster (60ms), but finding suggestions is a slower.
- Suggestion engine is now "mutation" with "n-gram root fallback" -- this is similar to how hunspell works.
- Other suggestion engines are removed since they require full dictionary materialization.

## [0.9.0] - 2026-06-14

- First initial release.
- Most (all?) Hunspell features implemented.
- This version "materialized" the dictionary (read .dic file then expand all work forms using .aff rules immediately). This makes suggestions fast, but loading slow (600ms).

