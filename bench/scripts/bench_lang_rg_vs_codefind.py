#!/usr/bin/env python3
"""Compare single rg vs codefind --lang on a multi-language game-like fixture.

Does NOT overwrite xlsx bench results (latest.*); writes lang_*.
"""
from __future__ import annotations

import csv
import json
import os
import statistics
import subprocess
import time
from collections import Counter
from pathlib import Path
from bench_common import resolve_binary, match_rows, count_matches, redact, environment
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[1]  # bench/scripts -> repo root
ROOT = Path(os.environ.get("CODEFIND_BENCH_ROOT", str(SCRIPT_DIR.parent)))  # bench/
FIXTURE = ROOT / "fixtures" / "multilang_game"
RESULTS = Path(os.environ.get("BENCH_RESULTS_DIR", str(ROOT / "results" / "v2")))
MAX_ANCHORS = int(os.environ.get("BENCH_MAX_ANCHORS", "12"))
RUNS = int(os.environ.get("BENCH_RUNS", "3"))
TIMEOUT_S = os.environ.get("BENCH_TIMEOUT", "10")


def resolve_codefind_bin() -> Path:
    return resolve_binary(REPO_ROOT)


CODEFIND = resolve_codefind_bin()


def rel_display(path: Path) -> str:
    """Prefer repo-relative path (bench/...) in generated summaries."""
    try:
        return str(path.resolve().relative_to(REPO_ROOT.resolve()))
    except ValueError:
        try:
            return str(path.resolve().relative_to(ROOT.resolve().parent))
        except ValueError:
            return "<external>/" + path.name

# Globs mirroring codefind searchGlobs for go+domain (default) and selected langs.
DOMAIN_GLOBS = [
    "--glob", "*.proto",
    "--glob", "*.md",
    "--glob", "*.csv",
    "--glob", "*.yaml",
    "--glob", "*.yml",
]
EXCLUDE_GLOBS = [
    "--glob", "!**/.git/**",
    "--glob", "!**/vendor/**",
    "--glob", "!**/node_modules/**",
    "--glob", "!**/*.min.js",
    "--glob", "!**/*.min.mjs",
    "--glob", "!**/*.min.cjs",
]
LANG_GLOBS = {
    "go": [".go"],
    "lua": [".lua"],
    "csharp": [".cs"],
    "c": [".c", ".h"],
    "cpp": [".cpp", ".cc", ".cxx", ".h", ".hpp", ".hh", ".hxx", ".C"],
    "js": [".js", ".jsx", ".mjs", ".cjs"],
    "ts": [".ts", ".tsx", ".mts", ".cts"],
}


def lang_to_rg_globs(langs: list[str]) -> list[str]:
    if "all" in langs:
        langs = list(LANG_GLOBS.keys())
    seen: list[str] = []
    out: list[str] = []
    for name in langs:
        for ext in LANG_GLOBS.get(name, []):
            g = f"*{ext}"
            if g not in seen:
                seen.append(g)
                out.extend(["--glob", g])
    out.extend(DOMAIN_GLOBS)
    out.extend(EXCLUDE_GLOBS)
    return out


# Queries: (label, mode, value, polarity, notes)
# mode: symbol | term | id-as-term
QUERIES: list[dict[str, Any]] = [
    {
        "id": "symbol_ClaimArenaReward",
        "mode": "symbol",
        "value": "ClaimArenaReward",
        "polarity": "pos",
        "notes": "shared symbol across langs + domain",
    },
    {
        "id": "term_arena_reward",
        "mode": "term",
        "value": "arena_reward",
        "polarity": "pos",
        "notes": "shared term",
    },
    {
        "id": "id_100126",
        "mode": "term",
        "value": "100126",
        "polarity": "pos",
        "notes": "shared config id",
    },
    {
        "id": "lua_only",
        "mode": "term",
        "value": "LUA_ONLY_CLUE_MoonlitArenaChest",
        "polarity": "pos",
        "notes": "Lua-only string; default Go should miss source",
    },
    {
        "id": "ts_only",
        "mode": "term",
        "value": "TS_ONLY_CLUE_SeasonPassArenaBadge",
        "polarity": "pos",
        "notes": "TS-only string; default Go should miss source",
    },
    {
        "id": "neg_NotInTreeAnywhere",
        "mode": "term",
        "value": "NotInTreeAnywhere_ZZZ999",
        "polarity": "neg",
        "notes": "negative control",
    },
    {
        "id": "decoy_vendor_should_exclude",
        "mode": "term",
        "value": "DECOY_ONLY_IN_VENDOR_ShouldNeverSurface",
        "polarity": "neg",
        "notes": "only in vendor/; should be excluded by scoped search",
    },
]


def run_cmd(argv: list[str], *, timeout: float = 60.0) -> tuple[int, str, str, int]:
    t0 = time.perf_counter()
    try:
        p = subprocess.run(
            argv,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            check=False,
        )
        out = p.stdout.decode("utf-8", errors="replace")
        err = p.stderr.decode("utf-8", errors="replace")
        ec = p.returncode
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or b"").decode("utf-8", errors="replace")
        err = (e.stderr or b"").decode("utf-8", errors="replace")
        ec = 124
    ms = round((time.perf_counter() - t0) * 1000, 3)
    return ec, out, err, ms


def median_ms(values: list[int]) -> int:
    return round(statistics.median(values), 3) if values else 0


def analyze_rg(out: str, exit_code: int) -> dict[str, Any]:
    rows = match_rows(out, exit_code)
    lines = [f"{p}:{n}:{text}" for p, n, text in rows]
    paths = [p for p, _, _ in rows]
    hit_count = len(rows)
    # noise: hits under excluded dirs / min.js (naive search)
    noise = 0
    for p in paths:
        pl = p.replace("\\", "/")
        if "/vendor/" in pl or pl.startswith("vendor/") or "/node_modules/" in pl or "node_modules/" in pl:
            noise += 1
        elif pl.endswith(".min.js") or ".min.js" in pl:
            noise += 1
    has_path_line = 1 if hit_count and all(":" in ln for ln in lines[: min(5, len(lines))]) else (1 if hit_count else 0)
    return {
        "hit_count": hit_count,
        "agent_json": 0,
        "has_path_line": has_path_line,
        "noise_hits": noise,
        "kind_distribution": {},
        "has_syntax": 0,
        "status": "rg_match" if hit_count else ("rg_no_match" if exit_code in (0, 1) else f"rg_ec_{exit_code}"),
        "query_languages": [],
        "rg_calls": None,
        "budget_fields": 0,
        "sample_paths": sorted(set(paths))[:12],
    }


def analyze_codefind(output: str, exit_code: int | None = None) -> dict[str, Any]:
    try:
        data = json.loads(output)
    except json.JSONDecodeError:
        return {
            "hit_count": 0,
            "agent_json": 0,
            "has_path_line": 0,
            "noise_hits": 0,
            "kind_distribution": {},
            "has_syntax": 0,
            "status": "json_parse_error",
            "query_languages": [],
            "rg_calls": None,
            "budget_fields": 0,
            "sample_paths": [],
            "raw_matches": 0,
        }
    if data.get("status") == "invalid_request" or "error" in data and data.get("schema_version") == "codefind-error-v1":
        return {
            "hit_count": 0,
            "agent_json": 1,
            "has_path_line": 0,
            "noise_hits": 0,
            "kind_distribution": {},
            "has_syntax": 0,
            "status": data.get("status", "invalid_request"),
            "query_languages": (data.get("query") or {}).get("languages") or [],
            "rg_calls": None,
            "budget_fields": 0,
            "sample_paths": [],
            "raw_matches": 0,
            "error": data.get("error"),
        }
    anchors = data.get("anchors") or []
    kinds = Counter(a.get("kind") or "?" for a in anchors)
    has_syntax = 1 if any(a.get("syntax") for a in anchors) else 0
    paths = [a.get("path") or "" for a in anchors]
    noise = 0
    for p in paths:
        pl = p.replace("\\", "/")
        if "/vendor/" in pl or pl.startswith("vendor/") or "/node_modules/" in pl or pl.endswith(".min.js"):
            noise += 1
    metrics = data.get("metrics") or {}
    limits = data.get("limits") or {}
    budget = 1 if limits or ("rg_calls" in metrics and "elapsed_ms" in metrics) else 0
    path_line_ok = 1 if anchors and all(a.get("path") and a.get("line") is not None for a in anchors) else (0 if not anchors else 0)
    if anchors and all(isinstance(a.get("path"), str) and a.get("line") is not None for a in anchors):
        path_line_ok = 1
    return {
        "hit_count": len(anchors),
        "agent_json": 1,
        "has_path_line": path_line_ok,
        "noise_hits": noise,
        "kind_distribution": dict(kinds),
        "has_syntax": has_syntax,
        "status": data.get("status", ""),
        "query_languages": (data.get("query") or {}).get("languages") or [],
        "rg_calls": metrics.get("rg_calls"),
        "budget_fields": budget,
        "sample_paths": paths[:12],
        "raw_matches": int(metrics.get("raw_matches") or len(anchors)),
        "elapsed_ms_internal": metrics.get("elapsed_ms"),
        "syntax_anchors": metrics.get("syntax_anchors"),
        "syntax_files_parsed": metrics.get("syntax_files_parsed"),
    }


def rg_argv(term: str, *, naive: bool, langs: list[str] | None) -> list[str]:
    # single rg -F with -n for path:line
    base = ["rg", "--no-config", "-F", "-n", "--", term, str(FIXTURE)]
    if naive:
        # broad: no lang discipline, maybe still skip .git lightly but include vendor noise
        # agent-naive: often just rg -F term .
        return ["rg", "--no-config", "-F", "-n", "--", term, str(FIXTURE)]
    assert langs is not None
    return ["rg", "--no-config", "-F", "-n", *lang_to_rg_globs(langs), "--", term, str(FIXTURE)]


def codefind_argv(q: dict[str, Any], langs: list[str] | None) -> list[str]:
    argv = [
        str(CODEFIND),
        "--root",
        str(FIXTURE),
        "--format",
        "text",
        "--timeout",
        f"{TIMEOUT_S}s",
        "--max-matches",
        "2000",
        "--max-anchors",
                        str(MAX_ANCHORS),
    ]
    if q["mode"] == "symbol":
        argv.extend(["--symbol", q["value"]])
    else:
        argv.extend(["--term", q["value"]])
    if langs is not None:
        for lang in langs:
            argv.extend(["--lang", lang])
    return argv


def timed_runs(label: str, argv: list[str], analyzer, query_id: str, polarity: str, n: int) -> tuple[dict, list[dict]]:
    times: list[int] = []
    last: dict[str, Any] = {}
    runs: list[dict] = []
    last_ec = 0
    for i in range(1, n + 1):
        ec, out, err, ms = run_cmd(argv)
        payload = out  # stderr is diagnostic data, never a match
        ana = analyzer(payload, ec)
        times.append(ms)
        last = ana
        last_ec = ec
        runs.append(
            {
                "method": label,
                "query_id": query_id,
                "polarity": polarity,
                "run": i,
                "wall_ms": ms,
                "exit_code": ec,
                "argv": argv,
                "stdout_bytes": len(out.encode("utf-8")),
                "stderr": err,
                **{k: v for k, v in ana.items() if k != "error"},
                **({"error": ana["error"]} if ana.get("error") else {}),
            }
        )
    summary = {
        "median_ms": median_ms(times),
        "exit_code": last_ec,
        **{
            k: last.get(k)
            for k in (
                "hit_count",
                "agent_json",
                "has_path_line",
                "noise_hits",
                "kind_distribution",
                "has_syntax",
                "status",
                "query_languages",
                "rg_calls",
                "budget_fields",
                "sample_paths",
                "raw_matches",
                "syntax_anchors",
            )
            if k in last or True
        },
    }
    if last.get("error"):
        summary["error"] = last["error"]
    return summary, runs


def main() -> None:
    if not FIXTURE.is_dir():
        raise SystemExit(f"missing fixture {FIXTURE}")
    if not CODEFIND.is_file():
        raise SystemExit(f"missing codefind {CODEFIND}")
    import shutil

    if not shutil.which("rg"):
        raise SystemExit("rg not on PATH")

    RESULTS.mkdir(parents=True, exist_ok=True)

    all_runs: list[dict] = []
    medians: list[dict] = []

    # Extra scenario: invalid lang (once, not timed heavily)
    print(">>> invalid_lang foobar", flush=True)
    inv_argv = [
        str(CODEFIND),
        "--root",
        str(FIXTURE),
        "--term",
        "arena_reward",
        "--lang",
        "foobar",
        "--timeout",
        f"{TIMEOUT_S}s",
    ]
    inv_sum, inv_runs = timed_runs(
        "codefind_invalid_lang",
        inv_argv,
        analyze_codefind,
        "invalid_lang_foobar",
        "neg",
        RUNS,
    )
    all_runs.extend(inv_runs)
    medians.append(
        {
            "type": "median_summary",
            "query_id": "invalid_lang_foobar",
            "value": "foobar",
            "polarity": "neg",
            "notes": "unknown lang → invalid_request",
            "methods": {"codefind_invalid_lang": inv_sum},
        }
    )
    print(
        f"  codefind_invalid_lang={inv_sum['median_ms']}ms status={inv_sum['status']} ec={inv_sum['exit_code']}",
        flush=True,
    )

    # Methods per query
    # 1 rg_single_naive
    # 2 rg_single_scoped (langs matching the codefind_lang under test; for shared clues use all)
    # 3 codefind_default
    # 4 codefind_lang_lua_ts (for lua/ts-focused) and codefind_lang_all

    for q in QUERIES:
        qid = q["id"]
        print(f">>> {qid} ({q['polarity']}) value={q['value']}", flush=True)
        method_stats: dict[str, dict] = {}

        # Determine scoped langs for fair rg baseline / codefind_lang
        if qid == "lua_only":
            scoped_langs = ["lua"]
            dual_langs = ["lua", "ts"]
        elif qid == "ts_only":
            scoped_langs = ["ts"]
            dual_langs = ["lua", "ts"]
        else:
            scoped_langs = ["all"]
            dual_langs = ["lua", "ts"]

        # A: rg naive
        s, runs = timed_runs(
            "rg_single_naive",
            rg_argv(q["value"], naive=True, langs=None),
            analyze_rg,
            qid,
            q["polarity"],
            RUNS,
        )
        method_stats["rg_single_naive"] = s
        all_runs.extend(runs)

        # B: rg scoped to all (or lua/ts for only-clues) + domain + excludes
        s, runs = timed_runs(
            "rg_single_scoped",
            rg_argv(q["value"], naive=False, langs=scoped_langs if scoped_langs != ["all"] else ["all"]),
            analyze_rg,
            qid,
            q["polarity"],
            RUNS,
        )
        method_stats["rg_single_scoped"] = s
        all_runs.extend(runs)

        # C: codefind default (Go)
        s, runs = timed_runs(
            "codefind_default",
            codefind_argv(q, None),
            analyze_codefind,
            qid,
            q["polarity"],
            RUNS,
        )
        method_stats["codefind_default"] = s
        all_runs.extend(runs)

        # D: codefind --lang lua --lang ts
        s, runs = timed_runs(
            "codefind_lang_lua_ts",
            codefind_argv(q, dual_langs),
            analyze_codefind,
            qid,
            q["polarity"],
            RUNS,
        )
        method_stats["codefind_lang_lua_ts"] = s
        all_runs.extend(runs)

        # E: codefind --lang all
        s, runs = timed_runs(
            "codefind_lang_all",
            codefind_argv(q, ["all"]),
            analyze_codefind,
            qid,
            q["polarity"],
            RUNS,
        )
        method_stats["codefind_lang_all"] = s
        all_runs.extend(runs)

        medians.append(
            {
                "type": "median_summary",
                "query_id": qid,
                "value": q["value"],
                "mode": q["mode"],
                "polarity": q["polarity"],
                "notes": q["notes"],
                "methods": method_stats,
            }
        )

        def brief(m: str) -> str:
            st = method_stats[m]
            return (
                f"{m}={st['median_ms']}ms hits={st['hit_count']} "
                f"json={st['agent_json']} syn={st.get('has_syntax')} "
                f"noise={st.get('noise_hits')} status={st.get('status')} langs={st.get('query_languages')}"
            )

        print("  " + " | ".join(brief(m) for m in method_stats), flush=True)

    # Write results — lang_* only
    payload = {
        "fixture": rel_display(FIXTURE),
        "codefind": rel_display(CODEFIND) if CODEFIND.is_absolute() else str(CODEFIND),
        "runs_per_query": RUNS,
        "queries": QUERIES,
        "medians": medians,
        "runs": all_runs,
    }

    version = "unknown"
    try:
        version = subprocess.check_output([str(CODEFIND), "--version"], text=True).strip()
    except Exception:
        pass
    git_sha = "unknown"
    git_subj = ""
    try:
        git_sha = subprocess.check_output(
            ["git", "-C", str(REPO_ROOT), "rev-parse", "--short", "HEAD"],
            text=True,
        ).strip()
        git_subj = subprocess.check_output(
            ["git", "-C", str(REPO_ROOT), "log", "-1", "--pretty=%s"],
            text=True,
        ).strip()
    except Exception:
        pass
    payload["codefind_version"] = version
    payload["git"] = {"sha": git_sha, "subject": git_subj}

    safe_paths = [(CODEFIND, "<CODEFIND_BIN>"), (FIXTURE, "bench/fixtures/multilang_game"), (REPO_ROOT, "<REPO>"), (ROOT, "bench")]
    payload["environment"] = environment(CODEFIND)
    payload["max_anchors"] = MAX_ANCHORS
    payload = redact(payload, safe_paths)
    all_runs = redact(all_runs, safe_paths)
    medians = redact(medians, safe_paths)
    json_path = RESULTS / "lang_latest.json"
    with json_path.open("w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)

    csv_path = RESULTS / "lang_latest.csv"
    fields = [
        "method",
        "query_id",
        "polarity",
        "run",
        "wall_ms",
        "exit_code",
        "hit_count",
        "raw_matches",
        "agent_json",
        "has_path_line",
        "noise_hits",
        "has_syntax",
        "status",
        "query_languages",
        "rg_calls",
        "budget_fields",
        "kind_distribution",
    ]
    with csv_path.open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fields, extrasaction="ignore")
        w.writeheader()
        for row in all_runs:
            r = dict(row)
            if isinstance(r.get("query_languages"), list):
                r["query_languages"] = ",".join(r["query_languages"])
            if isinstance(r.get("kind_distribution"), dict):
                r["kind_distribution"] = json.dumps(r["kind_distribution"], ensure_ascii=False)
            w.writerow(r)

    # Chinese summary
    md = RESULTS / "lang_summary.md"
    lines: list[str] = []
    lines.append("# codefind `--lang` vs 单次 rg：多语言游戏夹具基准")
    lines.append("")
    lines.append(f"- 夹具：`{rel_display(FIXTURE)}`")
    lines.append(f"- 构建：codefind `{version}` / git `{git_sha}` — {git_subj}")
    lines.append("- 方法：")
    lines.append("  - **rg_single_naive**：单次 `rg -F -n`，无语言/排除纪律（代理常见写法）")
    lines.append("  - **rg_single_scoped**：单次 `rg -F -n` + 与 codefind 对齐的语言 glob + proto/md/csv/yaml + 排除 vendor/node_modules/*.min.js")
    lines.append("  - **codefind_default**：不传 `--lang`（默认 **Go only**，域文件仍可搜）")
    lines.append("  - **codefind_lang_lua_ts**：`--lang lua --lang ts`")
    lines.append("  - **codefind_lang_all**：`--lang all`")
    lines.append(f"- 每查询跑 {RUNS} 次取 **median** 墙钟（ms）")
    lines.append("- **未覆盖** xlsx `latest.*`；本文件为 `lang_*`")
    lines.append("")
    lines.append("## 夹具埋点")
    lines.append("")
    lines.append("| 路径 | 线索 |")
    lines.append("|---|---|")
    lines.append("| `internal/reward/claim.go` | ClaimArenaReward / arena_reward / 100126（Go AST） |")
    lines.append("| `client/lua/arena_reward.lua` | 同上 + `LUA_ONLY_CLUE_MoonlitArenaChest` |")
    lines.append("| `client/ts/arenaReward.ts` | 同上 + `TS_ONLY_CLUE_SeasonPassArenaBadge` |")
    lines.append("| `client/csharp` / `engine/c` / `engine/cpp` / `client/js` | 共享符号/词/ID |")
    lines.append("| `proto` / `config` / `docs` | 域证据（默认语言收窄时仍可搜） |")
    lines.append("| `vendor` / `node_modules` / `*.min.js` | 仅 decoy，应被排除 |")
    lines.append("")
    lines.append("## 结果表（median）")
    lines.append("")
    lines.append(
        "| 查询 | 极性 | rg_naive ms/hits/noise | rg_scoped ms/hits/noise | "
        "cf_default ms/hits/syn/status/langs | cf_lua_ts ms/hits/syn/status | cf_all ms/hits/syn/status |"
    )
    lines.append("|---|---|---|---|---|---|---|")

    for m in medians:
        if m["query_id"] == "invalid_lang_foobar":
            inv = m["methods"]["codefind_invalid_lang"]
            lines.append(
                f"| `invalid_lang foobar` | neg | — | — | "
                f"**status=`{inv['status']}`** ms={inv['median_ms']} ec={inv['exit_code']} | — | — |"
            )
            continue
        methods = m["methods"]

        def cell_rg(key: str) -> str:
            st = methods[key]
            return f"{st['median_ms']} / {st['hit_count']} / {st.get('noise_hits', 0)}"

        def cell_cf(key: str) -> str:
            st = methods[key]
            langs = st.get("query_languages") or []
            lang_s = ",".join(langs) if langs else "-"
            return (
                f"{st['median_ms']} / {st['hit_count']} / syn={st.get('has_syntax')} / "
                f"`{st.get('status')}` / [{lang_s}]"
            )

        lines.append(
            f"| `{m['query_id']}` (`{m['value']}`) | {m['polarity']} | "
            f"{cell_rg('rg_single_naive')} | {cell_rg('rg_single_scoped')} | "
            f"{cell_cf('codefind_default')} | {cell_cf('codefind_lang_lua_ts')} | {cell_cf('codefind_lang_all')} |"
        )

    lines.append("")
    lines.append("## 能力差距（产品真相）")
    lines.append("")
    lines.append("1. **默认 = Go**：不传 `--lang` 时 `query.languages=[\"go\"]`。非 Go 源码中的线索（Lua/TS only）在 default 下为 **0 源码命中**；域文件（proto/md/csv/yaml）仍可能命中共享词。")
    lines.append("2. **`--lang` 是显式 opt-in**：`--lang lua --lang ts` 或 `--lang all` 才能打开对应扩展；未知 lang → `invalid_request`（exit 2）。")
    lines.append("3. **非 Go 只有词法候选**：`kind` 为 source/test/config 等，**无 `syntax` AST**；仅 Go 命中可带 `go_ast_syntax`。")
    lines.append("4. **结构化合同**：codefind 输出 path+line JSON、`metrics.rg_calls`（text 模式 ≤2）、limits 预算；单次 rg 有 path:line 文本但 **无 kind / syntax / budget 字段**。")
    lines.append("5. **rg 能搜到字面量，但纪律靠人**：naive rg 会打进 vendor/node_modules/*.min.js（noise）；scoped rg 接近 codefind 文件范围，仍无 AST/kind 合同。")
    lines.append("6. **zero ≠ absent**：负例与 default 漏掉非 Go 线索时，`no_candidates` / 0 命中只表示 unknown，不能断言功能不存在——需扩大 `--lang` 或换稳定 symbol。")
    lines.append("7. **xlsx 拒绝 `--lang`**：本基准为 text 模式；xlsx 对比见既有 `summary.md` / `latest.*`（未改写）。")
    lines.append("")
    lines.append("## 脚本")
    lines.append("")
    lines.append("- `bench/scripts/bench_lang_rg_vs_codefind.py`")
    lines.append("- `bench/scripts/bench_lang_rg_vs_codefind.sh`")
    lines.append("")
    lines.append("## 产出")
    lines.append("")
    lines.append("- `bench/results/lang_latest.json` / `lang_latest.csv` / `lang_summary.md`")
    lines.append("")

    md.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"Wrote {json_path}", flush=True)
    print(f"Wrote {csv_path}", flush=True)
    print(f"Wrote {md}", flush=True)


if __name__ == "__main__":
    main()
