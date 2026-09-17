# Changelog

## Unreleased

Local candidate after 0.2.0-rc.1; no remote tag or public release is implied. Binary `Version` is `0.2.0-rc.2`.

### Added

- Record already-in-tree lexical `--lang` values in the shipped candidate notes: lua, csharp, c, cpp, js, ts. `--lang all` selects every supported language. Default Go scope and domain evidence remain unchanged; only Go receives AST enrichment.
- `auto` encoding: if an authorized `*.csv` / `*.tsv` is not valid UTF-8, run one additional `rg --encoding gb18030` limited to those globs. No whole-tree retry. Explicit `utf-8` / `gbk` / `gb18030` do not fall back. Main-search `budget_exceeded` skips the retry.
- Additive contract fields: `query.encoding_applied` and `metrics.encoding_retries` (`0` or `1`). `query.encoding` stays the request option.
- Treat `*.tsv` as domain evidence and `kind: config`, matching CSV.

### Changed

- README / json-contract / CLI `--encoding` help now describe the csv/tsv fallback instead of “auto never guesses”.
- Non-goals heading refers to the current version, not v0.1.x.

## [0.2.0-rc.1] - 2026-09-07

Local prerelease candidate; no remote tag or public release is implied.

### Added

- Game-oriented discovery across source, protocols, configuration and documentation.
- Explicit text encodings: auto, utf-8, gbk and gb18030.
- Read-only XLSX search with workbook/sheet/cell/comment anchors, metadata ordering, per-file budgets and scan coverage.
- Workbook/sheet-diverse candidate projection to reduce repeated hits crowding out other sources.
- `read` subcommand with explicit ranges, adaptive structure selection, downward-header fallback, formula/cache separation, comments and merge coordinates.
- Synthetic game cases, real-executable search-to-read E2E checks and bounded-context evaluation.

### Fixed

- Search symbols before broad terms; compute projection truncation after deduplication.
- Disable user ripgrep configuration; distinguish invalid requests and execution failures in JSON.
- Classify supported files by extension before directory hints.
- Prefer whole-number ID matches when equal-length patterns compete.
- Preserve physical Go line positions despite //line directives.
- Report output failures for help and respect cancellation during readback selection.

### Boundaries

- Existing default text mode remains compatible with positive-line text adapters.
- XLSX search explicitly uses workbook coordinates instead of line; read uses codefind-read-v1.
- No OCR, formula recalculation, indexing, runtime business claims or automatic cross-root access.
- Windows amd64 package is locally validated; remote Windows/Linux/macOS CI and public publication remain separate gates.

## [0.1.0] - Previous baseline

### Added

- Budget-aware literal code discovery, codefind-result-v1 and bounded Go AST syntax evidence.
- Root/symlink containment checks and Windows/Linux/macOS CI configuration.
