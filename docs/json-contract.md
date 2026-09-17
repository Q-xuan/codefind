# JSON contract: v0.2.0-rc.2

## Compatibility

`query.languages` echoes normalized source-language selection already in tree (default `["go"]`, empty for xlsx). Supported values: go, lua, csharp, c, cpp, js, ts, plus `all`. `--lang` is repeatable; Proto / Markdown / CSV / TSV / YAML remain available. Non-Go `source` anchors are lexical locations, not declarations or syntax relationships. No new syntax authority is introduced.

The default remains text search. `codefind-result-v1` retains positive physical `line` numbers for text anchors. `query.format`, `query.encoding`, `query.encoding_applied` and `metrics.encoding_retries` are additive. Consumers must ignore unknown fields and dispatch by schema plus format, not human-readable messages or exact JSON key order. A csv/tsv encoding retry does not add a new `status`.

`query.encoding` is the request option (`auto` / `utf-8` / `gbk` / `gb18030`). It is not a detection conclusion. `query.encoding_applied` is the de-duplicated list actually used: `["auto"]` after the main search only; `["auto","gb18030"]` after a fallback; explicit encodings echo themselves. `metrics.encoding_retries` is `0` or `1`.

XLSX search is explicitly requested by `--format xlsx`: its anchors use `workbook.sheet/cell/source` and omit `line`. Do not pass these anchors to an old text-only validator. `source` is cell or comment. Cached formula results may be stale. Search output is a shortlist, not proof of absence or a semantic relation.

`codefind read` returns the separate `codefind-read-v1` schema. `read_complete` means the selected scope was parsed and returned without a budget truncation, not that a business question is resolved. `coverage.parse_complete` describes parsing; `coverage.truncated` separately describes incomplete reading or output. Empty cells are not emitted individually. `field_candidates` are heuristic positions: value-row positions for structure, header positions for below_headers.

## Status and process exit

| Schema | Status | Exit |
| --- | --- | --- |
| codefind-result-v1 | candidates_found, no_candidates, budget_exceeded, tool_unavailable | 0 |
| codefind-read-v1 | read_complete, budget_exceeded | 0 |
| codefind-error-v1 | invalid_request | 2 |
| codefind-error-v1 | execution_error | 1 |
| No guaranteed JSON if output cannot be written | output failure | 1 |

Help and version are plain text. Unknown flags, malformed values and extra positional arguments are invalid requests. Consumers must inspect status even when exit is zero.

## Bounds and provenance

- `metrics.truncated` includes projection loss, not only scanning limits. `workbook_coverage.discovery_complete` and per-file status identify scan coverage; completed scanning still does not cover unsupported images or threaded comments.
- `value` and `cached_value` in read output are strings holding stored values. `type` is the original XLSX cell type; absence denotes the default numeric representation. Do not treat numeric-looking strings as numbers automatically. A formula has separate expression/attributes and optional cached_value.
- `--max-chars` limits source text (including formula metadata/type strings), not the complete encoded JSON length. A cropped field requires text_truncated; missing output after a cap is not blank source data.
- A range refers to retained cell coordinates. Reading may scan the target worksheet XML and shared strings. No random-access performance or snapshot isolation is promised. Re-read if a source changes during investigation.
- Same-root containment is checked after symlink resolution. XLSX discovery does not follow links; explicit read validates the resolved file. Do not run against a directory being maliciously modified concurrently; this is not an OS sandbox.
- Multi-file raw matching still obeys global budgets: symbols can consume the text budget before terms; XLSX ordering cannot recover candidates never scanned.

The release's compiled-executable E2E checks search → returned location → read, truncation, error JSON and source hash preservation. Remote platform CI is a separate release gate.

## Adapter notes

1. Read `schema_version` and `status` first, then `query.format`.
2. `query.encoding == auto` is not “detected as UTF-8”. Use `encoding_applied`.
3. `no_candidates` after a retry is still unknown; do not treat it as “the field is absent” or “every encoding was exhausted”.
4. Non-Go `source` anchors are lexical text locations. Use a language service when a definition jump is required.
5. Old adapters that only read `query.encoding` and `status` remain valid if they ignore unknown keys.
