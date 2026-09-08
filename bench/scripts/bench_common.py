"""Small shared benchmark utilities: identity, portable parsing, safe reporting."""
import hashlib
import os
import platform
import re
import shutil
import subprocess
from pathlib import Path


def resolve_binary(repo):
    explicit = os.environ.get("CODEFIND_BIN")
    if explicit:
        return Path(explicit).resolve()
    for name in ("codefind.exe", "codefind") if os.name == "nt" else ("codefind",):
        candidate = repo / name
        if candidate.is_file():
            return candidate
    found = shutil.which("codefind")
    return Path(found) if found else repo / "codefind"


def match_rows(output, exit_code):
    if exit_code not in (0, 1):
        return []
    # The numeric line delimiter, not the drive colon, separates a path.
    return [m.groups() for line in output.splitlines()
            if (m := re.match(r"^(.*?):([1-9][0-9]*):(.*)$", line))]


def count_matches(output, exit_code):
    if exit_code not in (0, 1):
        return 0
    return sum(int(part) for line in output.splitlines()
               if (part := line.rsplit(":", 1)[-1].strip()).isdigit())


def redact(value, replacements):
    if isinstance(value, dict):
        return {k: redact(v, replacements) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [redact(v, replacements) for v in value]
    if not isinstance(value, str):
        return value
    for actual, label in sorted(replacements, key=lambda p: -len(str(p[0]))):
        for path in {str(actual), str(actual).replace("\\", "/")}:
            if path:
                value = value.replace(path, label)
    value = re.sub(r"[A-Za-z]:[\\/]Users[\\/][^\\/\s\"']+", "<HOME>", value)
    value = re.sub(r"/(?:Users|home)/[^/\s\"']+", "<HOME>", value)
    return value


def environment(binary):
    p = subprocess.run(["rg", "--version"], capture_output=True, check=False)
    return {"os": platform.system(), "architecture": platform.machine(),
            "python": platform.python_version(), "rg_version": p.stdout.decode("utf-8", "replace").splitlines()[0] if p.stdout else "unknown",
            "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
            "timing": "process wall time; no cold-cache claim; repository revision is not binary provenance"}
