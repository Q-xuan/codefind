# codefind

[![CI](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml/badge.svg)](https://github.com/Q-xuan/codefind/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

English | [简体中文](README_CN.md)

`codefind` is a budget-aware code discovery CLI for AI coding agents. It turns domain terms and candidate symbols into at most two bounded [`rg`](https://github.com/BurntSushi/ripgrep) literal searches, then returns a small set of source anchors for follow-up reading.

Its job is to narrow the reading surface—not to decide whether a feature exists. `codefind` is not a Code Graph and does not build semantic edges.

## Highlights

- One process call and at most two internal `rg` calls: one for domain terms, one for candidate symbol/test names.
- All patterns use `rg --fixed-strings`; they are never interpreted as regular expressions or shell code.
- Explicit limits for raw matches, projected anchors, and total elapsed time.
- Repository-relative paths and line numbers in a single-line JSON response.
- Go candidates may include bounded `go/ast` syntax evidence (`definition`, `call`, or `reference`) without claiming type-resolved edges.
- Search paths must remain inside `--root`, including after symlink resolution.
- No index, daemon, model call, or write to the searched repository.

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

Search an example Go repository for configuration-loading code:

```sh
codefind --root ./example-repo \
  --path cmd --path internal --path docs \
  --term "configuration" --term "load config" \
  --symbol "LoadConfig" --symbol "TestLoadConfig"
```

PowerShell:

```powershell
codefind --root .\example-repo `
  --path cmd --path internal --path docs `
  --term configuration --term "load config" `
  --symbol LoadConfig --symbol TestLoadConfig
```

Provide at least one `--term` or `--symbol`. Repeat either flag to send multiple literal patterns.

### Options

| Option | Meaning | Default / limit |
| --- | --- | --- |
| `--root` | Repository root to search; required | none |
| `--path` | Relative directory inside `root`; repeatable | `.` |
| `--term` | Domain term, action phrase, or historical alias; repeatable | at least one term or symbol |
| `--symbol` | Candidate symbol or test name; repeatable | at least one term or symbol |
| `--max-anchors` | Maximum projected anchors | 12 / maximum 50 |
| `--max-matches` | Maximum raw matches read from `rg` | 2000 / maximum 10000 |
| `--timeout` | Total search timeout | 2s / maximum 10s |
| `--version` | Print the version and exit | - |

## JSON contract

Every valid request writes one line of `codefind-result-v1` JSON to stdout:

```json
{"schema_version":"codefind-result-v1","engine":"codefind","version":"0.1.0","status":"candidates_found","query":{"terms":["configuration"],"symbols":["LoadConfig"],"paths":["cmd","internal"]},"anchors":[{"kind":"source","path":"internal/config/load.go","line":12,"text":"func LoadConfig(path string) error {","groups":["symbols"],"syntax":{"role":"definition","symbol":"LoadConfig","authority":"go_ast_syntax"}}],"unknowns":[],"metrics":{"agent_calls":1,"rg_calls":2,"elapsed_ms":8,"first_anchor_ms":3,"raw_matches":4,"projected_anchors":1,"truncated":false,"syntax_files_parsed":1,"syntax_anchors":1,"syntax_parse_errors":0,"syntax_files_skipped":0},"limits":{"max_anchors":12,"max_matches":2000,"timeout_ms":2000},"external_writes":0}
```

### Result fields

- `schema_version`: result schema identifier; consumers should check this first.
- `engine` / `version`: producer identity and CLI version.
- `status`: machine-readable result state.
- `query`: normalized, de-duplicated terms, symbols, and search paths actually used.
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
| `source` | Go declarations such as `func`, `type`, `const`, or `var` |
| `consumer` | Other source usages and call sites |
| `protocol` | Protocol Buffers definitions |
| `config` | CSV or YAML configuration |
| `docs` | Markdown documentation |
| `generated` | Recognized generated Go files |

An invalid request writes `codefind-error-v1` with status `invalid_request` and exits with code 2. JSON-output failures exit with code 1. All result statuses exit with code 0, so callers must inspect `status`.

## Budget semantics

Budgets are part of the result contract:

- Terms only: one `rg` call. Symbols only: one `rg` call. Both groups: at most two calls.
- `--max-matches` limits raw matches read from `rg`; `--max-anchors` limits the projected response.
- Reaching the time or raw-match limit returns `budget_exceeded`.
- Projection and de-duplication may reduce the response without exhausting a budget.
- Go syntax enrichment shares the request timeout and only parses lexical shortlist files, at most 64 files and 1 MiB per file. Parse failures remain lexical-only and increment metrics.
- `no_candidates` only means the current query produced no anchors. It must never be converted into “not implemented” or “does not exist.”

## Default search scope

`codefind` searches Go, Protocol Buffers, Markdown, CSV, and YAML files. It excludes `.git`, `vendor`, `node_modules`, and minified JavaScript by default. It never expands beyond the directories supplied through `--path`.

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

```sh
go fmt ./...
go test ./...
go vet ./...
go build ./cmd/codefind
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines and [SECURITY.md](SECURITY.md) for vulnerability reporting.

## License

[MIT](LICENSE)
