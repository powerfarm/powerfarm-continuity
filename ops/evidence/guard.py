#!/usr/bin/env python3
"""Refuse to export evidence that contains a credential.

Evidence is published; a credential in it is published too. This scans a
directory tree and exits non-zero on the first file that looks like it carries
one, naming the file and the pattern that matched. It is deliberately noisy
rather than clever: a false positive costs one look, a false negative costs a
credential.

    python3 ops/evidence/guard.py evidence/heartime-ingress-20260916
"""

import re
import sys
from pathlib import Path

# Content-addressed references are not secrets, and every Powerfarm record is
# full of them.
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")

PATTERNS = {
    "bearer credential": re.compile(r"(?i)bearer\s+[A-Za-z0-9._\-]{8,}"),
    "named secret": re.compile(r"(?i)\b(api[_-]?key|secret|password|passphrase|private[_-]key)\b"),
    "OpenAI-style key": re.compile(r"\bsk-[A-Za-z0-9]{8,}"),
    "xAI key": re.compile(r"\bxai-[A-Za-z0-9]{8,}"),
    "GitHub token": re.compile(r"\bgh[pousr]_[A-Za-z0-9]{8,}"),
    "JSON web token": re.compile(r"\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}"),
    "AWS access key": re.compile(r"\b(AKIA|ASIA)[0-9A-Z]{16}\b"),
    "PEM private key": re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----"),
}


def scan(root: Path) -> list[tuple[Path, str, str]]:
    findings = []
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        text = DIGEST.sub("", path.read_bytes().decode("utf-8", "replace"))
        for name, pattern in PATTERNS.items():
            match = pattern.search(text)
            if match:
                findings.append((path, name, match.group(0)[:48]))
    return findings


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    root = Path(sys.argv[1])
    if not root.is_dir():
        print(f"{root} is not a directory", file=sys.stderr)
        return 2
    findings = scan(root)
    for path, name, excerpt in findings:
        print(f"REFUSED {path}: {name}: {excerpt}", file=sys.stderr)
    if findings:
        print(f"{len(findings)} finding(s); this export must not be published", file=sys.stderr)
        return 1
    print(f"{root}: no credential found")
    return 0


if __name__ == "__main__":
    sys.exit(main())
