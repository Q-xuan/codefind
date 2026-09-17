# codefind

[![CI](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml/badge.svg)](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

English | [简体中文](README_CN.md)

Local prerelease: **0.2.0-rc.1**. See [JSON compatibility](docs/json-contract.md) and [release checks](RELEASE_CHECKLIST.md). Public installation commands below use published revisions and may not include this local candidate yet.

`codefind` is a budget-aware discovery CLI for AI coding agents working on game projects. It searches gameplay names, configuration IDs, historical aliases, and candidate symbols across Go source, Proto definitions, CSV / YAML configuration, and Markdown documentation—without building a code graph.

Text mode uses at most two bounded [`rg`](https://github.com/BurntSushi/ripgrep) literal searches. XLSX mode reads workbook content natively. Both return a small set of candidate locations for follow-up reading.

Its job is to narrow the reading surface—not to decide whether a feature exists. `codefind` is not a Code Graph and does not build semantic edges.

## Why game projects

Clues to a gameplay feature can span server logic, network protocols, balance tables, feature switches, and design documents—not just function calls.

- **Search across content types**: use gameplay names, protocol names, or configuration IDs to find literal-match candidates in source, protocols, configuration, and documentation.
- **No code package or Git repository required**: supported text files in ordinary directories are searchable without compilation or indexing.
- **Read current on-disk content**: locate implementation, tests, and data clues before an agent changes gameplay logic.
- **Keep evidence boundaries explicit**: shared matches do not prove a business relationship, and zero hits do not prove absence. Candidates still require inspection.

The current scope best fits games using Go for logic alongside Proto and CSV / YAML. It does not cover every game-engine language or binary asset, and does not replace type-aware call graphs or impact analysis.

## Highlights

### XLSX design workbooks

Use a separate read-only mode to search stored Excel cell values (including cached formula results) and legacy comments:

```sh
codefind --root ./design/math --format xlsx --term Pvp --term "挑战券" --timeout 10s
```

This mode needs neither rg nor Excel and builds no index. Matches contain `kind: config`, a relative workbook `path`, `text`, `groups`, and `workbook: {sheet, cell, source}`. `source` is `cell` or `comment`; source-code `line` is omitted. A cell and its comment may match separately; no comment match does not imply no comment exists. `query.format` identifies `text` or `xlsx`.

When the filename is unknown, search the directory once. Cheap discovery ranks workbooks by filename, sheet names, and shared-string literals (plus smaller size on ties), then **content-scans only the top workbook**. Other discovered files stay `pending` with reason `deferred`; follow up with `--path` on the next file. Do not exclude the 7–9 MB workbooks and rescan the remaining directory — 2–3 MB files still exhaust a shared slice. `--path` may still name a single `.xlsx` or exclude one with `!`. Naming multiple files content-scans each of them. The timeout ceiling stays **10s**; `20s` is still `invalid_request`. No gameplay aliases or synonyms are inferred. Name/sheet matching is case-insensitive and affects ordering only; shared-string hints and cell matching stay case-sensitive and literal. A hint miss never excludes a file. Matching sheet names are also scanned first inside the selected workbook. `codefind --help-xlsx` explains directory cold start, `read --range` vs `--field`, and zero-hit wording.

Output uses two-level round-robin allocation across matched workbooks and their matched sheets, preserving relevance order within each sheet. This reduces domination by repeated hits from one sheet, but cannot guarantee every sheet fits a small output budget. It only selects already-scanned candidates; scan scope and budgets do not expand. Cross-sheet output may trade strict symbol priority for source diversity.

`workbook_coverage.files` lists discovered files in scan order: `complete` means supported content was scanned, `partial` means unfinished, and `pending` means not started. `reason` distinguishes `file_timeout`, `total_timeout`, `match_limit`, `file_size_or_xml_limit`, and `deferred` (ranked below the one workbook chosen for a directory cold start). `deferred` is not a timeout and does not by itself produce `budget_exceeded`. `metadata_status` independently reports name-metadata availability. `discovery_complete: false` means enumeration was limited, so the list is not exhaustive. Complete does not imply OCR, formula evaluation, or business understanding.

Metadata uses at most 20% of the total timeout, with at most 200ms and 1 MiB of XML reads per workbook (sheet names plus a shared-string literal peek). Metadata failures fall back to filename/size ordering without dropping the file. A directory cold start gives the selected workbook the remaining request time. Explicitly named files still share time slices: total timeout divided by `min(named files, 4)`. File-time or size limits on a named multi-file request allow later named files to proceed; global time or match limits stop the request. Intentional `deferred` files are not an incomplete scan. There is no resumable cursor; retries restart reading.

Scope and limits:

- The default remains `--format text`. Run the modes separately; XLSX rejects non-auto `--encoding`.
- Matching is case-sensitive and literal. Sheet names, images, screenshots, threaded comments, and formula expressions are not searched. Formulas are not recalculated, cached results may be stale, numeric/date display formatting is not rendered, and merged cells are located at the cell actually storing the value. Fields are not interpreted.
- The request shares `--timeout`, `--max-matches`, and `--max-anchors`. Raw matches count cells or comments once even when both query groups match. At most 32 workbooks are scanned, each up to 32 MiB on disk with a cumulative XML decompression-read limit of 64 MiB per workbook. Limits produce `budget_exceeded` with partial candidates retained.
- `metrics.xlsx_files_scanned` counts attempted files. Corrupt or unreadable workbooks produce `execution_error`, not a misleading zero-hit result.
- Traversal stays within authorized root/path, does not follow symlinks, and skips dot-prefixed subdirectories, vendor, node_modules, and `~$` lock files. This native mode does not read `.gitignore`; restrict scope with `--path` (directory, named file, or `!` exclude). ZIP parts are never extracted to disk, external links are never fetched, and workbooks are never modified.
- Legacy `.xls`, encrypted workbooks, and screenshot rendering are unsupported. A zero lexical hit is **unknown**, not “the field is not in the workbook.” Near-synonyms are not inferred.

### Evidence first, understanding next

codefind does not prebuild a graph or vector index. It searches current on-disk text, leaving scope, candidate selection, and output budgets to the tool, and business understanding and further exploration to the agent. Use a search → read → search-again loop; the two-rg limit applies to one request, not the entire investigation.

For example, search documentation for a gameplay name, read the result to confirm a configuration ID, then query that ID inside authorized configuration directories. Shared matches are candidate evidence, not proven cross-file business relationships.

### Search and candidate output

- One process call and at most two internal `rg` calls: one for domain terms, one for candidate symbol/test names.
- All patterns use `rg --fixed-strings`; they are never interpreted as regular expressions or shell code.
- Explicit limits for raw matches, projected anchors, and total elapsed time.
- Repository-relative paths and line numbers in a single-line JSON response.
- Go candidates may include bounded `go/ast` syntax evidence (`definition`, `call`, or `reference`) without claiming type-resolved edges.
- Search paths must remain inside `--root`, including after symlink resolution.
- No index, daemon, model call, or write to the searched repository.
- File types take precedence over directory names, so Markdown notes and Go logic inside a proto directory are not mislabeled as protocols.
- Among equal-length literal matches, numeric queries favor complete numbers (not adjacent to other digits) to reduce configuration-ID substring noise. This does not imply CSV field equality, and substring candidates remain eligible. Longer matches, group priority, and kind quotas still affect final ordering.

## Requirements

- Go 1.22 or newer to build from source
- `rg` (ripgrep) available on `PATH` at runtime

Check the dependencies with:

```sh
go version
rg --version
```

## Installation

Install the latest version with Go:

```sh
go install github.com/Q-xuan/codefind/cmd/codefind@latest
```

Make sure the Go bin directory is on `PATH`. You can also build from source:

```sh
git clone https://github.com/Q-xuan/codefind.git
cd codefind
go build -o codefind ./cmd/codefind
```

On Windows, use `codefind.exe` as the output filename.

## Usage

### Multi-language source search

The default keeps Go source scope for compatibility. Repeat `--lang` to select other languages, or use `--lang all`:

```sh
codefind --root ./game-project --lang lua --lang ts --symbol ClaimReward
codefind --root ./game-project --lang all --term "reward"
```

| Language | Flag value | Extensions |
| --- | --- | --- |
| Go | go (default) | .go |
| Lua | lua | .lua |
| C# | csharp (aliases cs, c#) | .cs |
| C | c | .c, .h |
| C++ | cpp (alias c++) | .cpp, .cc, .cxx, .h, .hpp, .hh, .hxx, .C |
| JavaScript | js (alias javascript) | .js, .jsx, .mjs, .cjs |
| TypeScript | ts (alias typescript) | .ts, .tsx, .mts, .cts |

Language selection narrows source extensions, while Proto, Markdown, CSV and YAML remain searchable. Use `--path` to narrow directories or name/exclude a single file. Minified JS, vendor and node_modules remain excluded. Unknown languages produce invalid_request; xlsx rejects --lang. `query.languages` echoes normalized, deduplicated languages (empty for xlsx).

Non-Go results are lexical candidates without AST or definition/call claims: source means source text, and test directories are classified as test. Go keeps its existing AST enrichment. No compiler, index or language server is added; all languages share the existing call and resource budgets.

### Progressive readback after search

Read one explicitly authorized workbook and worksheet after locating a match:

```sh
codefind read --root ./design/math --file common.xlsx --sheet common --range B6:AD20
codefind read --root ./design/math --file common.xlsx --sheet common --anchor D20 --field "参数1" --field param1
```

Replace names and coordinates with actual search results. Supply exactly one of `--range` and `--anchor`. When the rectangle is known, **`--range` is safer than `--field`**: `--field 类型` can latch onto a nearby same-header column. `codefind --help-xlsx` and `codefind read --help` state this explicitly.

- Explicit ranges allow at most 4096 positions. `--max-cells` defaults to 96 returned cells (maximum 4096); `--max-chars` defaults to 24000 (maximum 200000) for returned source text, excluding JSON/coordinate overhead. Empty positions are not individually emitted.
- Anchor mode defaults to `--strategy adaptive`, with repeatable `--field` aliases. Structure selection finds equal row keys within the first 64 columns, then searches up to 32 rows above columns to their right for matching headers. If no field column is found, it falls back to the row plus the first 12 header rows. Multiple candidates remain unresolved.
- Explicit alternatives are `structure`, `row_headers`, and `window` (two rows above/below and four columns left/right). No strategy automatically expands authorization or crosses sheets.
- When upward selection finds no field, adaptive first checks the next 8 rows within the first 64 columns for field headers. It returns 9 rows starting at each candidate header in that column, plus row labels in the anchor column and its two right neighbors. It reports below_headers with reason no_field_columns_above; only when no lower header exists does it fall back to row_headers. Multiple candidates remain unresolved, and proximity does not establish a business relationship. Adaptive retained scope extends up to 16 rows below the anchor, within the same output and reading budgets.
- `codefind-read-v1` returns cells, field_candidates, merged_ranges, actual strategy, fallback_reason, and coverage. Cells distinguish value, formula, cached_value, and comment. Shared-formula attributes are preserved, not expanded. Values are not formatted or recalculated.
- `read_complete` means selected-scope parsing/output was not budget-truncated, not that a question was answered. `budget_exceeded` means incomplete reading/output. Errors retain invalid_request/exit 2 and execution_error/exit 1; result statuses, including budget exhaustion, exit 0.
- Timeout defaults to 2s (maximum 10s). Workbook/XML limits remain 32/64 MiB, with at most 100000 **in-scope** cell/comment events and 4096 merge regions scanned. Cells outside the selected range or anchor boxes are skipped and do not count toward that cap. Shared strings for the workbook are still read. `coverage.ranges` describes retained candidate coordinates, not disk I/O ranges.
- Merge coordinates are preserved. If the top-left value is outside retained scope, expand the range explicitly. Cropped text is marked text_truncated. Direct cell comments are not conflated with nearby explanatory text.

Use search → structural readback → explicit range expansion → other-sheet/document search as needed. The agent decides whether evidence is sufficient; the tool does not automatically complete the investigation.

Locate arena reward clues in an example game project (replace directories and patterns with ones that exist in your project):

```sh
codefind --root ./game-project \
  --path internal --path proto --path config --path docs \
  --term "arena_reward" --term "100126" \
  --symbol "ClaimArenaReward" --symbol "TestClaimArenaReward"
```

PowerShell:

```powershell
codefind --root .\game-project `
  --path internal --path proto --path config --path docs `
  --term arena_reward --term "100126" `
  --symbol ClaimArenaReward --symbol TestClaimArenaReward
```

Provide at least one `--term` or `--symbol`. Repeat either flag to send multiple literal patterns.

### Options

| Option | Meaning | Default / limit |
| --- | --- | --- |
| `--root` | Search root; need not be a Git repository; required | none |
| `--path` | Relative directory or file inside `root`; repeatable. `!` prefix excludes that path | `.` |
| `--term` | Domain term, action phrase, or historical alias; repeatable | at least one term or symbol |
| `--symbol` | Candidate symbol or test name; repeatable | at least one term or symbol |
| `--max-anchors` | Maximum projected anchors | 12 / maximum 50 |
| `--max-matches` | Maximum raw matches read from `rg` | 2000 / maximum 10000 |
| `--timeout` | Total search timeout. Filter large xlsx with `--path` first; do not raise this above 10s | 2s / maximum 10s |
| `--encoding` | File encoding for this request: `auto`, `utf-8`, `gbk`, `gb18030` | `auto` |
| `--format` | Search mode: `text` or `xlsx` | `text` |
| `--lang` | Source language, repeatable; all selects every supported language | `go` |
| `--version` | Print the version and exit | - |
| `--help-xlsx` | Explain xlsx `--path`, `read --range` vs `--field`, and zero-hit unknown | - |

## JSON contract

### Configuration table encoding

The default `auto` preserves rg's encoding behavior (including BOM detection); it does not guess GBK or retry with another encoding after zero hits. For GBK gameplay tables, specify:

```sh
codefind --root ./game-project --path data/tables --term "奖励" --encoding gbk
```

Encoding applies to every search path in the request. Search UTF-8 source and GBK tables separately. Query patterns and JSON output remain Unicode / UTF-8; target files are never transcoded on disk or modified. Go AST parsing still follows Go source rules; non-UTF-8 Go files that fail parsing remain lexical candidates. `query.encoding` records the normalized selected option, not a detected encoding for each file.

Every valid request writes one line of `codefind-result-v1` JSON to stdout:

```json
{"schema_version":"codefind-result-v1","engine":"codefind","version":"0.2.0-rc.1","status":"candidates_found","query":{"format":"text","encoding":"auto","terms":["configuration"],"symbols":["LoadConfig"],"paths":["cmd","internal"]},"anchors":[{"kind":"source","path":"internal/config/load.go","line":12,"text":"func LoadConfig(path string) error {","groups":["symbols"],"syntax":{"role":"definition","symbol":"LoadConfig","authority":"go_ast_syntax"}}],"unknowns":[],"metrics":{"agent_calls":1,"rg_calls":2,"elapsed_ms":8,"first_anchor_ms":3,"raw_matches":4,"projected_anchors":1,"truncated":false,"syntax_files_parsed":1,"syntax_anchors":1,"syntax_parse_errors":0,"syntax_files_skipped":0},"limits":{"max_anchors":12,"max_matches":2000,"timeout_ms":2000},"external_writes":0}
```

### Result fields

- `schema_version`: result schema identifier; consumers should check this first.
- `engine` / `version`: producer identity and CLI version.
- `status`: machine-readable result state.
- `query`: normalized, de-duplicated terms, symbols, and search paths actually used.
- `query.encoding`: selected file encoding option, defaulting to `auto`.
- `anchors`: bounded candidate locations. `path` is always relative to `root`. Optional `syntax` is syntax-only evidence from `go/ast`, never a type-resolved relation.
- `unknowns`: questions the current result cannot answer; never treat them as negative conclusions.
- `metrics`: calls, elapsed time, raw matches, projected anchors, truncation, and bounded Go syntax parsing counts. `first_anchor_ms` is `null` when no anchor was observed.
- `limits`: effective budgets for this request.
- `external_writes`: writes to the searched repository; currently always `0`.

The human-readable `text` and `unknowns` values may change. Branch on `schema_version` and `status`, not message text.

### Status values

| Status | Meaning |
| --- | --- |
| `candidates_found` | Bounded candidates were found; inspect the referenced source next. |
| `no_candidates` | Zero hits under the current terms, paths, and budget. This means unknown, not absence. |
| `budget_exceeded` | The time or raw-match budget was reached; returned candidates may be incomplete. |
| `tool_unavailable` | `rg` was not found, so no discovery was performed. |

### Anchor kinds

| Kind | Typical match |
| --- | --- |
| `test` | Go tests or files in test directories |
| `source` | Go declarations, or lexical source candidates in other selected languages (not proof of a definition) |
| `consumer` | Other source usages and call sites |
| `protocol` | Protocol Buffers definitions |
| `config` | CSV or YAML configuration |
| `docs` | Markdown documentation |
| `generated` | Recognized generated Go files |

An invalid request, including malformed flags or unexpected positional arguments, writes `codefind-error-v1` with status `invalid_request` and exits with code 2. Search execution failures write the same error schema with status `execution_error` and exit with code 1. JSON-output failures exit with code 1. All result statuses exit with code 0, so callers must inspect `status`. Help and version requests produce plain text.

## Budget semantics

Budgets are part of the result contract:

- Terms only: one `rg` call. Symbols only: one `rg` call. Both groups: at most two calls.
- `--max-matches` limits raw matches read from `rg`; `--max-anchors` limits the projected response.
- Reaching the time or raw-match limit returns `budget_exceeded`.
- Projection and de-duplication may reduce the response without exhausting a budget.
- Symbols are searched before terms using a shared match budget; terms may be skipped if symbols exhaust that budget.
- `metrics.truncated` indicates an exhausted budget or omitted unique anchors; duplicate matches alone do not set it.
- `rg` runs with `--no-config`, ignoring `RIPGREP_CONFIG_PATH` so user configuration cannot change the search contract.
- Go syntax enrichment shares the request timeout and only parses lexical shortlist files, at most 64 files and 1 MiB per file. Parse failures remain lexical-only and increment metrics.
- `no_candidates` only means the current query produced no anchors. It must never be converted into “not implemented” or “does not exist.”

## Default search scope

`text` mode defaults to Go, Protocol Buffers, Markdown, CSV, and YAML; `--lang` selects additional supported source languages. It excludes `.git`, `vendor`, `node_modules`, and minified JavaScript by default. See above for `xlsx` traversal rules. Neither mode expands beyond the directories or files supplied through `--path`.

Each request accepts one `--root`. Multiple repositories or ordinary directories under the same authorized root can be searched through repeated `--path` flags; arbitrary separate roots are not supported in one request. Applicable ripgrep ignore rules, including `.gitignore`, still apply, so not every file under the root is necessarily searched.

## Security model

- Query strings are passed as process arguments to `rg`, not through a shell.
- A `--` option terminator keeps dash-prefixed search paths from becoming `rg` options.
- Absolute search paths and paths that escape `root` are rejected.
- Symlinks are resolved before the containment check.
- Output can contain source excerpts; treat it as sensitive when searching private repositories.

## Non-goals

The following are deliberately outside `codefind` v0.1.x:

- Code Graphs, call graphs, or semantic edges
- Type-resolved receiver, interface-dispatch, reflection, or runtime-call claims; `go_ast_syntax` only describes source syntax
- Persistent or incremental indexes and background daemons
- Embeddings, RAG, vector databases, or model inference
- MCP servers, plugin systems, or editor-integration frameworks
- Business conclusions about whether code exists, is correct, or is safe to modify
- Editing, generating, or repairing files in the searched repository

## Development

Game regression scenarios cover source, protocols, CSV / YAML, documentation, generated code, numeric-ID ranking, and scoped follow-up queries in non-Git directories:

```sh
go test ./internal/find -run TestGame -count=1 -v
```

These are synthetic regression fixtures, not retrieval-quality benchmarks on a real game project or evidence of superiority over raw rg, graphs, or vector retrieval.

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./cmd/codefind
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines and [SECURITY.md](SECURITY.md) for vulnerability reporting.

## License

[MIT](LICENSE)
