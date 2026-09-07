#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export CODEFIND_BENCH_ROOT="${CODEFIND_BENCH_ROOT:-$ROOT}"
exec /usr/bin/python3 "$ROOT/scripts/bench_lang_rg_vs_codefind.py" "$@"
