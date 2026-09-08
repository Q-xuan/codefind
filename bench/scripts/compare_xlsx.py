"""Paired single-workbook comparison; alternating order, identical output check."""
import argparse
import hashlib
import json
import statistics
import subprocess
import time
from pathlib import Path


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--before", required=True)
    p.add_argument("--after", required=True)
    p.add_argument("--runs", type=int, default=10)
    args = p.parse_args()
    if args.runs < 2:
        p.error("runs must be at least 2")
    root = Path(__file__).resolve().parents[1] / "fixtures/game_tables"
    samples = {"before": [], "after": []}
    expected = None
    for i in range(args.runs):
        for label in (("before", "after") if i % 2 == 0 else ("after", "before")):
            start = time.perf_counter()
            proc = subprocess.run([getattr(args, label), "--root", str(root), "--format", "xlsx", "--term", "energyMax", "--timeout", "10s"], capture_output=True, timeout=15)
            wall = (time.perf_counter() - start) * 1000
            if proc.returncode:
                raise RuntimeError(f"{label}: exit {proc.returncode}")
            result = json.loads(proc.stdout)
            if result["status"] != "candidates_found":
                raise RuntimeError("Expected complete positive query")
            semantic = {k: result[k] for k in ("anchors", "status", "query", "limits", "external_writes")}
            if expected is None:
                expected = semantic
            if semantic != expected:
                raise RuntimeError("Candidate/contract regression")
            samples[label].append({"wall_ms": round(wall, 3), "internal_ms": result["metrics"]["elapsed_ms"]})
    summary = {label: {"binary_sha256": hashlib.sha256(Path(getattr(args, label)).read_bytes()).hexdigest(), "median_wall_ms": statistics.median(row["wall_ms"] for row in values), "median_internal_ms": statistics.median(row["internal_ms"] for row in values), "samples": values} for label, values in samples.items()}
    print(json.dumps({"runs_per_binary": args.runs, "candidate_contract_equal": True, "note": "Warm/alternating process runs, not cold-cache or statistical significance proof", "results": summary}, indent=2))


if __name__ == "__main__":
    main()
