#!/usr/bin/env python3
"""Smoke-test common one-api providers through the OpenAI-compatible API.

The script sends a minimal request for one representative model per provider and
prints a compact availability table. It tests the full one-api path: token,
group/model permission, channel routing, base URL, upstream API key, and upstream
model availability.

Usage:
  ONE_API_KEY=sk-... python3 scripts/test_providers.py --base-url http://localhost:3000
  ONE_API_KEY=sk-... python3 scripts/test_providers.py --providers siliconflow,alibailian
  ONE_API_KEY=sk-... python3 scripts/test_providers.py --json

Notes:
  - A provider can only pass if you have a one-api channel configured for the
    representative model below.
  - Override any model with --model provider=model, for example:
      --model siliconflow=BAAI/bge-m3 --model alibailian=text-embedding-v3
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, asdict
from typing import Any, Dict, Iterable, List, Optional, Tuple


@dataclass(frozen=True)
class ProviderCase:
    provider: str
    kind: str  # chat | embeddings
    model: str
    note: str = ""


# One representative, commonly used model per provider. These are intentionally
# conservative defaults, not exhaustive model lists.
DEFAULT_CASES: List[ProviderCase] = [
    ProviderCase("openai", "chat", "gpt-4o-mini", "OpenAI low-cost chat"),
    ProviderCase("openai_embedding", "embeddings", "text-embedding-3-small", "OpenAI embedding"),
    ProviderCase("openrouter", "chat", "deepseek/deepseek-chat", "OpenRouter common chat"),
    ProviderCase("siliconflow", "embeddings", "BAAI/bge-m3", "SiliconFlow embedding, try Qwen/Qwen3-Embedding-0.6B if unavailable"),
    ProviderCase("alibailian", "embeddings", "text-embedding-v3", "Ali DashScope compatible-mode embedding"),
    ProviderCase("ali", "chat", "qwen-turbo", "Ali DashScope native chat"),
    ProviderCase("deepseek", "chat", "deepseek-chat", "DeepSeek official"),
    ProviderCase("zhipu", "chat", "glm-4-flash", "Zhipu GLM low-cost chat"),
    ProviderCase("baidu_v2", "chat", "ernie-speed-8k", "Baidu Qianfan v2 chat"),
    ProviderCase("doubao", "chat", "doubao-1-5-lite-32k", "Volcengine Ark/Doubao chat"),
    ProviderCase("moonshot", "chat", "moonshot-v1-8k", "Moonshot/Kimi chat"),
    ProviderCase("tencent", "chat", "hunyuan-lite", "Tencent Hunyuan chat"),
    ProviderCase("xunfei", "chat", "lite", "iFlyTek Spark v2 common chat"),
    ProviderCase("minimax", "chat", "MiniMax-Text-01", "MiniMax chat"),
    ProviderCase("lingyiwanwu", "chat", "yi-lightning", "01.AI chat"),
    ProviderCase("stepfun", "chat", "step-1-flash", "StepFun chat"),
    ProviderCase("anthropic", "chat", "claude-3-5-haiku-latest", "Anthropic fast chat"),
    ProviderCase("gemini", "chat", "gemini-2.0-flash", "Google Gemini chat"),
    ProviderCase("groq", "chat", "llama-3.1-8b-instant", "Groq fast chat"),
    ProviderCase("mistral", "chat", "mistral-small-latest", "Mistral chat"),
    ProviderCase("xai", "chat", "grok-3-mini", "xAI chat"),
    ProviderCase("togetherai", "chat", "meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo", "Together AI chat"),
    ProviderCase("novita", "chat", "meta-llama/llama-3.1-8b-instruct", "Novita chat"),
    ProviderCase("ollama", "chat", "llama3.2:latest", "Local Ollama, only if configured"),
]


@dataclass
class TestResult:
    provider: str
    kind: str
    model: str
    ok: bool
    status: Optional[int]
    latency_ms: int
    error: str
    note: str


def normalize_base_url(base_url: str) -> str:
    base_url = base_url.rstrip("/")
    if base_url.endswith("/v1"):
        return base_url
    return base_url + "/v1"


def request_json(url: str, api_key: str, payload: Dict[str, Any], timeout: float) -> Tuple[int, Dict[str, Any], str]:
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8", errors="replace")
            try:
                return resp.status, json.loads(body), body
            except json.JSONDecodeError:
                return resp.status, {}, body
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")
        try:
            parsed = json.loads(body)
        except json.JSONDecodeError:
            parsed = {}
        return e.code, parsed, body
    except Exception as e:  # network timeout, DNS, connection refused, etc.
        return 0, {}, str(e)


def extract_error(status: int, parsed: Dict[str, Any], raw: str) -> str:
    if 200 <= status < 300:
        return ""
    if isinstance(parsed, dict):
        err = parsed.get("error")
        if isinstance(err, dict):
            parts = []
            for key in ("message", "type", "code"):
                value = err.get(key)
                if value:
                    parts.append(str(value))
            if parts:
                return " | ".join(parts)
        if isinstance(err, str):
            return err
        for key in ("message", "msg", "error_msg", "detail"):
            value = parsed.get(key)
            if value:
                return str(value)
    return raw[:500]


def build_payload(case: ProviderCase) -> Tuple[str, Dict[str, Any]]:
    if case.kind == "embeddings":
        return "/embeddings", {
            "model": case.model,
            "input": "one-api provider smoke test",
        }
    if case.kind == "chat":
        return "/chat/completions", {
            "model": case.model,
            "messages": [{"role": "user", "content": "Reply with OK only."}],
            "temperature": 0,
            "max_tokens": 8,
            "stream": False,
        }
    raise ValueError(f"unsupported kind: {case.kind}")


def run_case(base_url_v1: str, api_key: str, case: ProviderCase, timeout: float) -> TestResult:
    path, payload = build_payload(case)
    started = time.monotonic()
    status, parsed, raw = request_json(base_url_v1 + path, api_key, payload, timeout)
    latency_ms = int((time.monotonic() - started) * 1000)

    ok = False
    if 200 <= status < 300:
        if case.kind == "embeddings":
            ok = bool(parsed.get("data")) if parsed else bool(raw)
        else:
            ok = bool(parsed.get("choices")) if parsed else bool(raw)

    return TestResult(
        provider=case.provider,
        kind=case.kind,
        model=case.model,
        ok=ok,
        status=status or None,
        latency_ms=latency_ms,
        error="" if ok else extract_error(status, parsed, raw),
        note=case.note,
    )


def parse_model_overrides(values: Iterable[str]) -> Dict[str, str]:
    overrides: Dict[str, str] = {}
    for value in values:
        if "=" not in value:
            raise SystemExit(f"--model must be provider=model, got: {value}")
        provider, model = value.split("=", 1)
        provider = provider.strip()
        model = model.strip()
        if not provider or not model:
            raise SystemExit(f"--model must be provider=model, got: {value}")
        overrides[provider] = model
    return overrides


def select_cases(providers: str, overrides: Dict[str, str]) -> List[ProviderCase]:
    cases = DEFAULT_CASES
    if providers:
        wanted = {p.strip() for p in providers.split(",") if p.strip()}
        known = {case.provider for case in cases}
        unknown = sorted(wanted - known)
        if unknown:
            raise SystemExit(f"unknown provider(s): {', '.join(unknown)}\nKnown: {', '.join(sorted(known))}")
        cases = [case for case in cases if case.provider in wanted]

    out: List[ProviderCase] = []
    for case in cases:
        if case.provider in overrides:
            out.append(ProviderCase(case.provider, case.kind, overrides[case.provider], case.note))
        else:
            out.append(case)
    return out


def print_table(results: List[TestResult]) -> None:
    headers = ["provider", "kind", "model", "ok", "status", "ms", "error"]
    rows = []
    for r in results:
        rows.append([
            r.provider,
            r.kind,
            r.model,
            "YES" if r.ok else "NO",
            str(r.status or ""),
            str(r.latency_ms),
            r.error.replace("\n", " ")[:120],
        ])
    widths = [len(h) for h in headers]
    for row in rows:
        for i, cell in enumerate(row):
            widths[i] = max(widths[i], len(cell))

    def fmt(row: List[str]) -> str:
        return "  ".join(cell.ljust(widths[i]) for i, cell in enumerate(row))

    print(fmt(headers))
    print(fmt(["-" * w for w in widths]))
    for row in rows:
        print(fmt(row))

    ok_count = sum(1 for r in results if r.ok)
    print(f"\nSummary: {ok_count}/{len(results)} provider cases passed")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base-url", default=os.getenv("ONE_API_BASE_URL", "http://localhost:3000"), help="one-api base URL, with or without /v1")
    parser.add_argument("--api-key", default=os.getenv("ONE_API_KEY", ""), help="one-api token. Defaults to ONE_API_KEY")
    parser.add_argument("--providers", default="", help="comma-separated provider names to test")
    parser.add_argument("--model", action="append", default=[], help="override model as provider=model. Can be repeated")
    parser.add_argument("--timeout", type=float, default=30.0, help="per-request timeout seconds")
    parser.add_argument("--json", action="store_true", help="print JSON instead of a table")
    parser.add_argument("--list", action="store_true", help="list default provider cases and exit")
    args = parser.parse_args()

    overrides = parse_model_overrides(args.model)
    cases = select_cases(args.providers, overrides)

    if args.list:
        for case in cases:
            print(f"{case.provider}\t{case.kind}\t{case.model}\t{case.note}")
        return 0

    if not args.api_key:
        print("Missing API key. Set ONE_API_KEY or pass --api-key.", file=sys.stderr)
        return 2

    base_url_v1 = normalize_base_url(args.base_url)
    results = [run_case(base_url_v1, args.api_key, case, args.timeout) for case in cases]

    if args.json:
        print(json.dumps([asdict(r) for r in results], ensure_ascii=False, indent=2))
    else:
        print_table(results)

    return 0 if all(r.ok for r in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())

