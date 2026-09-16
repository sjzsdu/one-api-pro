#!/usr/bin/env python3
"""Validate SiliconFlow models by actually calling them.

By default this script uses relay/adaptor/provider/siliconflow/adaptor.go as the model
source and skips image/audio/video/ocr/tts/rerank models unless explicitly asked.
It prints pass/fail per model without exposing SILICONFLOW_API_KEY.

Usage:
  SILICONFLOW_API_KEY=sk-... python3 scripts/test_siliconflow_models.py
  python3 scripts/test_siliconflow_models.py --include-non-chat
  python3 scripts/test_siliconflow_models.py --models BAAI/bge-m3,Qwen/Qwen3-Embedding-0.6B
  python3 scripts/test_siliconflow_models.py --json
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple


MODEL_FILE = Path("relay/adaptor/provider/siliconflow/adaptor.go")
BASE_URL = "https://api.siliconflow.cn/v1"

EMBEDDING_HINTS = ("Embedding", "embedding", "bge-")
RERANK_HINTS = ("Reranker", "reranker")
SKIP_HINTS = (
    "Image",
    "image",
    "OCR",
    "PaddleOCR",
    "TeleSpeech",
    "SenseVoice",
    "CosyVoice",
    "TTSD",
    "Wan2.",
    "Kolors",
    "Hunyuan-MT",
    "Z-Image",
)


@dataclass
class Result:
    model: str
    kind: str
    skipped: bool
    ok: bool
    status: Optional[int]
    latency_ms: int
    error: str


def read_models_from_go(path: Path) -> List[str]:
    text = path.read_text()
    models = re.findall(r'"([^"\\]*(?:\\.[^"\\]*)*)"\s*,', text)
    out: List[str] = []
    seen = set()
    for m in models:
        m = bytes(m, "utf-8").decode("unicode_escape")
        if m not in seen:
            out.append(m)
            seen.add(m)
    return out


def classify(model: str) -> str:
    if any(h in model for h in RERANK_HINTS):
        return "rerank"
    if any(h in model for h in EMBEDDING_HINTS):
        return "embeddings"
    if any(h in model for h in SKIP_HINTS):
        return "non-chat"
    return "chat"


def post_json(url: str, key: str, payload: Dict[str, Any], timeout: float) -> Tuple[int, Dict[str, Any], str]:
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
            try:
                return resp.status, json.loads(raw), raw
            except json.JSONDecodeError:
                return resp.status, {}, raw
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            parsed = {}
        return e.code, parsed, raw
    except Exception as e:
        return 0, {}, str(e)


def extract_error(status: int, parsed: Dict[str, Any], raw: str) -> str:
    if 200 <= status < 300:
        return ""
    if isinstance(parsed, dict):
        err = parsed.get("error")
        if isinstance(err, dict):
            vals = [str(err.get(k)) for k in ("message", "type", "code") if err.get(k)]
            if vals:
                return " | ".join(vals)
        if isinstance(err, str):
            return err
        for key in ("message", "msg", "detail", "code"):
            if parsed.get(key):
                return str(parsed[key])
    return raw[:500]


def test_model(model: str, kind: str, key: str, timeout: float) -> Result:
    if kind == "embeddings":
        url = BASE_URL + "/embeddings"
        payload = {"model": model, "input": "siliconflow model validation"}
    elif kind == "chat":
        url = BASE_URL + "/chat/completions"
        payload = {
            "model": model,
            "messages": [{"role": "user", "content": "Reply with OK only."}],
            "temperature": 0,
            "max_tokens": 8,
        }
    elif kind == "rerank":
        url = BASE_URL + "/rerank"
        payload = {"model": model, "query": "hello", "documents": ["hello world", "goodbye"]}
    else:
        return Result(model, kind, True, False, None, 0, "skipped non-chat/non-embedding model")

    started = time.monotonic()
    status, parsed, raw = post_json(url, key, payload, timeout)
    latency_ms = int((time.monotonic() - started) * 1000)
    ok = 200 <= status < 300
    return Result(model, kind, False, ok, status or None, latency_ms, "" if ok else extract_error(status, parsed, raw))


def print_table(results: List[Result]) -> None:
    headers = ["model", "kind", "ok", "status", "ms", "error"]
    rows = []
    for r in results:
        rows.append([
            r.model,
            r.kind,
            "SKIP" if r.skipped else ("YES" if r.ok else "NO"),
            str(r.status or ""),
            str(r.latency_ms),
            r.error.replace("\n", " ")[:140],
        ])
    widths = [len(h) for h in headers]
    for row in rows:
        for i, cell in enumerate(row):
            widths[i] = min(max(widths[i], len(cell)), 72 if i == 0 else 140)

    def trunc(cell: str, width: int) -> str:
        return cell if len(cell) <= width else cell[: width - 1] + "…"

    def fmt(row: List[str]) -> str:
        return "  ".join(trunc(cell, widths[i]).ljust(widths[i]) for i, cell in enumerate(row))

    print(fmt(headers))
    print(fmt(["-" * w for w in widths]))
    for row in rows:
        print(fmt(row))
    tested = [r for r in results if not r.skipped]
    passed = [r for r in tested if r.ok]
    skipped = [r for r in results if r.skipped]
    print(f"\nSummary: {len(passed)}/{len(tested)} tested passed, {len(skipped)} skipped")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--models", default="", help="comma-separated explicit model list")
    parser.add_argument("--include-non-chat", action="store_true", help="also attempt rerank and non-chat classifications where supported")
    parser.add_argument("--limit", type=int, default=0, help="test only first N selected models")
    parser.add_argument("--timeout", type=float, default=45.0)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()

    key = os.getenv("SILICONFLOW_API_KEY")
    if not key:
        print("Missing SILICONFLOW_API_KEY", file=sys.stderr)
        return 2

    models = [m.strip() for m in args.models.split(",") if m.strip()] if args.models else read_models_from_go(MODEL_FILE)
    if args.limit:
        models = models[: args.limit]

    results: List[Result] = []
    for model in models:
        kind = classify(model)
        if kind in ("non-chat", "rerank") and not args.include_non_chat:
            results.append(Result(model, kind, True, False, None, 0, "skipped; pass --include-non-chat to test"))
            continue
        results.append(test_model(model, kind, key, args.timeout))

    if args.json:
        print(json.dumps([asdict(r) for r in results], ensure_ascii=False, indent=2))
    else:
        print_table(results)

    tested = [r for r in results if not r.skipped]
    return 0 if all(r.ok for r in tested) else 1


if __name__ == "__main__":
    raise SystemExit(main())

