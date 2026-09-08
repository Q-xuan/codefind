"""Default-12 coordinate/readback check against an independent XML oracle.

This checks stored cell values only; not comments, business semantics or OCR.
No original workbook is changed. Results are written to results/v2 by default.
"""
import hashlib
import json
import os
import posixpath
import subprocess
import time
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path
from bench_common import environment, resolve_binary
from bench_rg_vs_codefind import QUERIES

REPO = Path(__file__).resolve().parents[2]
ROOT = Path(os.environ.get("CODEFIND_BENCH_ROOT", str(REPO / "bench")))
OUTPUT = Path(os.environ.get("BENCH_RESULTS_DIR", str(ROOT / "results" / "v2")))


def oracle(file):
    cells = {}
    ns = {"s": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
    with zipfile.ZipFile(file) as z:
        shared = []
        if "xl/sharedStrings.xml" in z.namelist():
            for si in ET.fromstring(z.read("xl/sharedStrings.xml")).findall("s:si", ns):
                shared.append("".join(t.text or "" for t in si.findall(".//s:t", ns)))
        rels = {r.attrib["Id"]: r.attrib["Target"] for r in ET.fromstring(z.read("xl/_rels/workbook.xml.rels")) if r.attrib.get("TargetMode") != "External"}
        for sheet in ET.fromstring(z.read("xl/workbook.xml")).findall("s:sheets/s:sheet", ns):
            target = rels[sheet.attrib["{http://schemas.openxmlformats.org/officeDocument/2006/relationships}id"]]
            path = target.lstrip("/") if target.startswith("/") else posixpath.normpath(posixpath.join("xl", target))
            for c in ET.fromstring(z.read(path)).findall(".//s:sheetData/s:row/s:c", ns):
                value = c.findtext("s:v", default="", namespaces=ns)
                if c.attrib.get("t") == "s":
                    value = shared[int(value)]
                elif c.attrib.get("t") == "inlineStr":
                    value = "".join(t.text or "" for t in c.findall("s:is//s:t", ns))
                if value:
                    cells[(sheet.attrib["name"], c.attrib["r"])] = value
    return cells


def call(binary, args):
    start = time.perf_counter()
    p = subprocess.run([str(binary), *args], capture_output=True, timeout=15, check=False)
    if p.returncode != 0:
        raise RuntimeError(f"CLI failed with exit {p.returncode}; inspect locally, diagnostics not published")
    return json.loads(p.stdout), round((time.perf_counter() - start) * 1000, 3), len(p.stdout)


def main():
    binary = resolve_binary(REPO)
    file = ROOT / "fixtures/game_tables/survivor_game_tables.xlsx"
    before = hashlib.sha256(file.read_bytes()).hexdigest()
    expected = oracle(file)
    rows = []
    for term, polarity in QUERIES:
        golden = {key for key, text in expected.items() if term in text}
        result, search_ms, output_bytes = call(binary, ["--root", str(file.parent), "--format", "xlsx", "--term", term, "--timeout", "10s"])
        if result["status"] not in ("candidates_found", "no_candidates"):
            raise RuntimeError("Incomplete search; do not score it as a complete run")
        anchors = result["anchors"]
        actual = {(a["workbook"]["sheet"], a["workbook"]["cell"]) for a in anchors}
        correct = actual & golden
        row = {"query": term, "expected_cells": len(golden), "returned": len(anchors), "correct_coordinates": len(correct), "recall_at_12": len(correct) / len(golden) if golden else None, "search_wall_ms": search_ms, "stdout_bytes": output_bytes, "tool_calls": 1, "read_verified": None}
        if anchors:
            a = anchors[0]
            read, read_ms, read_bytes = call(binary, ["read", "--root", str(file.parent), "--file", a["path"], "--sheet", a["workbook"]["sheet"], "--range", a["workbook"]["cell"]])
            value = next((c.get("value", c.get("cached_value")) for c in read["cells"] if c["cell"] == a["workbook"]["cell"]), None)
            row.update(read_verified=read["status"] == "read_complete" and value == expected.get((a["workbook"]["sheet"], a["workbook"]["cell"])), read_wall_ms=read_ms, end_to_end_wall_ms=round(search_ms + read_ms, 3), stdout_bytes=output_bytes + read_bytes, tool_calls=2)
        if len(actual) != len(correct) or row["read_verified"] is False or len(anchors) > 12:
            raise RuntimeError("Coordinate/readback assertion failed")
        rows.append(row)
    if hashlib.sha256(file.read_bytes()).hexdigest() != before:
        raise RuntimeError("Fixture changed")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    report = {"environment": environment(binary), "fixture_sha256": before, "note": "One run per query, functional acceptance only; no latency significance claim", "results": rows}
    (OUTPUT / "readback.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"queries": len(rows), "positive_readbacks_verified": sum(r["read_verified"] is True for r in rows), "source_unchanged": True}))


if __name__ == "__main__":
    main()
