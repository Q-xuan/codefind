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
- `query.paths` may list directories, a named file, or `!` excludes. Only-exclude lists still search `.`. Consumers that ignore unknown path shapes should treat `!` as an exclude prefix, not a filename.
- `--timeout` maximum remains 10s. Raising the ceiling is not part of this contract. A directory or single-file xlsx request content-scans one workbook unless `--max-files` (1..32) asks for an explicit batch. `workbook_coverage.files[]` includes additive `score` and `size`. `next_path` is the next ranked sibling after the scanned file. Other files use `reason=deferred` and do not by themselves produce `budget_exceeded`. Hop with `--path` on `next_path`, or run `next_command`. `--cursor` skips files already marked complete (`reason=cursor_skipped`) and does not repeat them. A batch that stops early sets `content_complete` false and must not be read as a full-directory scan. Named multi-file paths still scan each named file. A read failure on one file in a multi-file batch is `reason=read_error` and does not cancel the others; a single-file read failure is still `execution_error`. Single-term ties are not broken by smaller-file-first.
- XLSX anchors include `matched_terms`. `--match exact` requires the whole cell or comment text to equal a term; the default `substring` is unchanged. Neither mode adds synonyms. `query.match` echoes `substring` or `exact` for xlsx.
- `--sheet` limits content scan to named worksheets. Other sheets are `skipped` / `not_requested`. Workbook size limits stay 32 MiB on disk and 64 MiB of XML. A limit is `file_size`, `xml_part`, or `xml_total`, with `limit_bytes`, `actual_bytes`, `budget_bytes`, and `entry` / `sheet` when known. One sheet over the XML budget does not drop anchors from sheets that were read; the file stays `partial` with `reason=sheet_limit`.
- `read` coverage adds `requested_cells`, `returned_cells`, `last_cell`, `cell_truncated`, and `char_truncated`. `parse_complete` is parsing; `cell_truncated` is the cell cap; `char_truncated` / `text_truncated` is the character cap. `resume_cursor` and `next_ranges` continue a `--range` without repeating returned cells. `--cursor` on `read` is valid only inside that range. Allowed ceilings remain max-anchors 1..50, max-cells 1..4096, max-chars 1..200000.
- A shared-formula follower keeps an empty stored `expression` and adds `shared_anchor` plus `anchor_expression`. `resolved_expression` is present only when relative references were shifted mechanically. `resolve_status` is `mechanical` or `unknown`. Unknown keeps the original text and does not invent a value. Cached values are not evidence of recalculation.
- A range refers to retained cell coordinates. Read skips cells outside the selected range or anchor boxes; those cells do not count toward the 100000 in-scope event cap. Shared strings are still read. No random-access performance or snapshot isolation is promised. Re-read if a source changes during investigation.
- Same-root containment is checked after symlink resolution. XLSX discovery does not follow links; explicit read validates the resolved file. Do not run against a directory being maliciously modified concurrently; this is not an OS sandbox.
- Zero lexical hits remain `no_candidates` / unknown. Do not translate them as “not in the workbook.”
- Multi-file raw matching still obeys global budgets: symbols can consume the text budget before terms; XLSX ordering cannot recover candidates never scanned.

The release's compiled-executable E2E checks search → returned location → read, truncation, error JSON and source hash preservation. Remote platform CI is a separate release gate.

## Adapter notes

1. Read `schema_version` and `status` first, then `query.format`.
2. `query.encoding == auto` is not “detected as UTF-8”. Use `encoding_applied`.
3. `no_candidates` after a retry is still unknown; do not treat it as “the field is absent” or “every encoding was exhausted”.
4. Non-Go `source` anchors are lexical text locations. Use a language service when a definition jump is required.
5. Old adapters that only read `query.encoding` and `status` remain valid if they ignore unknown keys.
