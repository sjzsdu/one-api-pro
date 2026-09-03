#!/usr/bin/env python3
"""Test upstream provider connectivity using API keys from environment variables.

This script does NOT go through one-api. It directly calls each provider's public
API with a small, cheap request and reports whether the provider key + endpoint +
representative model are usable.

Usage:
  python3 scripts/test_upstream_providers.py
  python3 scripts/test_upstream_providers.py --providers siliconflow,qwen,deepseek
  python3 scripts/test_upstream_providers.py --json

Environment variables used by default:
  OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY or GOOGLE_API_KEY,
  OPEN_ROUTE_API_KEY, SILICONFLOW_API_KEY, QWEN_API_KEY, DEEPSEEK_API_KEY,
  ZHIPU_API_KEY or ZP_API_KEY, GROQ_API_KEY, KIMI_API_KEY, MINIMAX_API_KEY,
  BAIDU_API_KEY or QIANFAN_API_KEY, DOUBAO_API_KEY, GROK_API_KEY.

Override representative models with --model provider=model, for example:
  python3 scripts/test_upstream_providers.py --model siliconflow=BAAI/bge-m3
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from typing import Any, Dict, Iterable, List, Optional, Tuple


@dataclass(frozen=True)
class ProviderCase:
    provider: str
    env_names: Tuple[str, ...]
    api_style: str  # openai_chat | openai_embeddings | anthropic | gemini
    base_url: str
    model: str
    note: str = ""


DEFAULT_CASES: List[ProviderCase] = [
    ProviderCase("openai", ("OPENAI_API_KEY",), "openai_chat", "https://api.openai.com/v1", "gpt-4o-mini"),
    ProviderCase("openai_embedding", ("OPENAI_API_KEY",), "openai_embeddings", "https://api.openai.com/v1", "text-embedding-3-small"),
    ProviderCase("anthropic", ("ANTHROPIC_API_KEY",), "anthropic", "https://api.anthropic.com", "claude-3-5-haiku-latest"),
    ProviderCase("gemini", ("GEMINI_API_KEY", "GOOGLE_API_KEY"), "gemini", "https://generativelanguage.googleapis.com/v1beta", "gemini-2.0-flash"),
    ProviderCase("openrouter", ("OPEN_ROUTE_API_KEY", "OPENROUTER_API_KEY"), "openai_chat", "https://openrouter.ai/api/v1", "deepseek/deepseek-chat"),
    ProviderCase("siliconflow", ("SILICONFLOW_API_KEY",), "openai_embeddings", "https://api.siliconflow.cn/v1", "BAAI/bge-m3", "try Qwen/Qwen3-Embedding-0.6B if account lacks BAAI/bge-m3"),
    ProviderCase("qwen", ("QWEN_API_KEY", "DASHSCOPE_API_KEY"), "openai_embeddings", "https://dashscope.aliyuncs.com/compatible-mode/v1", "text-embedding-v3"),
    ProviderCase("deepseek", ("DEEPSEEK_API_KEY",), "openai_chat", "https://api.deepseek.com/v1", "deepseek-chat"),
    ProviderCase("zhipu", ("ZHIPU_API_KEY", "ZP_API_KEY"), "openai_chat", "https://open.bigmodel.cn/api/paas/v4", "glm-5"),
    ProviderCase("groq", ("GROQ_API_KEY",), "openai_chat", "https://api.groq.com/openai/v1", "llama-3.1-8b-instant"),
    ProviderCase("kimi", ("KIMI_API_KEY",), "openai_chat", "https://api.moonshot.cn/v1", "moonshot-v1-8k", "429 engine_overloaded_error is usually transient; rerun later"),
    ProviderCase("minimax", ("MINIMAX_API_KEY",), "openai_chat", "https://api.minimax.chat/v1", "MiniMax-Text-01"),
    ProviderCase("baidu_qianfan", ("QIANFAN_API_KEY", "BAIDU_API_KEY"), "openai_chat", "https://qianfan.baidubce.com/v2", "ernie-4.0-turbo-8k", "requires a Qianfan v2 Bearer API key, not AK/SK"),
    ProviderCase("doubao", ("DOUBAO_API_KEY",), "openai_chat", "https://ark.cn-beijing.volces.com/api/v3", "doubao-1-5-lite-32k", "Volcengine often requires your endpoint ID as model; override with --model doubao=<endpoint-id>"),
    ProviderCase("xai", ("GROK_API_KEY", "XAI_API_KEY"), "openai_chat", "https://api.x.ai/v1", "grok-3-mini"),
]


@dataclass
class TestResult:
    provider: str
    env: str
    has_key: bool
    api_style: str
    model: str
    ok: bool
    status: Optional[int]
    latency_ms: int
    error: str
    note: str


def mask_env_name(env_names: Tuple[str, ...]) -> Tuple[str, str]:
    for name in env_names:
        if os.getenv(name):
            return name, os.environ[name]
    return env_names[0], ""


def post_json(url: str, headers: Dict[str, str], payload: Dict[str, Any], timeout: float) -> Tuple[int, Dict[str, Any], str]:
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers=headers, method="POST")
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
        for key in ("message", "msg", "error_msg", "detail", "error_description"):
            if parsed.get(key):
                return str(parsed[key])
    return raw[:500]


def build_request(case: ProviderCase, key: str) -> Tuple[str, Dict[str, str], Dict[str, Any]]:
    if case.api_style == "openai_chat":
        return (
            case.base_url.rstrip("/") + "/chat/completions",
            {"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
            {
                "model": case.model,
                "messages": [{"role": "user", "content": "Reply with OK only."}],
                "temperature": 0,
                "max_tokens": 8,
                "stream": False,
            },
        )
    if case.api_style == "openai_embeddings":
        return (
            case.base_url.rstrip("/") + "/embeddings",
            {"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
            {"model": case.model, "input": "provider connectivity test"},
        )
    if case.api_style == "anthropic":
        return (
            case.base_url.rstrip("/") + "/v1/messages",
            {
                "x-api-key": key,
                "anthropic-version": "2023-06-01",
                "Content-Type": "application/json",
            },
            {
                "model": case.model,
                "max_tokens": 8,
                "temperature": 0,
                "messages": [{"role": "user", "content": "Reply with OK only."}],
            },
        )
    if case.api_style == "gemini":
        return (
            f"{case.base_url.rstrip('/')}/models/{case.model}:generateContent?key={key}",
            {"Content-Type": "application/json"},
            {
                "contents": [{"parts": [{"text": "Reply with OK only."}]}],
                "generationConfig": {"temperature": 0, "maxOutputTokens": 8},
            },
        )
    raise ValueError(f"unsupported api_style: {case.api_style}")


def response_is_ok(case: ProviderCase, parsed: Dict[str, Any], raw: str) -> bool:
    if case.api_style == "openai_chat":
        return bool(parsed.get("choices")) if parsed else bool(raw)
    if case.api_style == "openai_embeddings":
        return bool(parsed.get("data")) if parsed else bool(raw)
    if case.api_style == "anthropic":
        return bool(parsed.get("content")) if parsed else bool(raw)
    if case.api_style == "gemini":
        return bool(parsed.get("candidates")) if parsed else bool(raw)
    return bool(raw)


def run_case(case: ProviderCase, timeout: float) -> TestResult:
    env_name, key = mask_env_name(case.env_names)
    if not key:
        return TestResult(case.provider, env_name, False, case.api_style, case.model, False, None, 0, "missing env", case.note)

    url, headers, payload = build_request(case, key)
    started = time.monotonic()
    status, parsed, raw = post_json(url, headers, payload, timeout)
    latency_ms = int((time.monotonic() - started) * 1000)
    ok = 200 <= status < 300 and response_is_ok(case, parsed, raw)
    return TestResult(
        provider=case.provider,
        env=env_name,
        has_key=True,
        api_style=case.api_style,
        model=case.model,
        ok=ok,
        status=status or None,
        latency_ms=latency_ms,
        error="" if ok else extract_error(status, parsed, raw),
        note=case.note,
    )


def parse_model_overrides(values: Iterable[str]) -> Dict[str, str]:
    out: Dict[str, str] = {}
    for value in values:
        if "=" not in value:
            raise SystemExit(f"--model must be provider=model, got {value!r}")
        provider, model = value.split("=", 1)
        out[provider.strip()] = model.strip()
    return out


def select_cases(providers: str, overrides: Dict[str, str]) -> List[ProviderCase]:
    cases = DEFAULT_CASES
    if providers:
        wanted = {p.strip() for p in providers.split(",") if p.strip()}
        known = {c.provider for c in cases}
        unknown = sorted(wanted - known)
        if unknown:
            raise SystemExit(f"unknown provider(s): {', '.join(unknown)}\nKnown: {', '.join(sorted(known))}")
        cases = [c for c in cases if c.provider in wanted]
    result = []
    for c in cases:
        if c.provider in overrides:
            result.append(ProviderCase(c.provider, c.env_names, c.api_style, c.base_url, overrides[c.provider], c.note))
        else:
            result.append(c)
    return result


def print_table(results: List[TestResult]) -> None:
    headers = ["provider", "env", "key", "style", "model", "ok", "status", "ms", "error"]
    rows = []
    for r in results:
        rows.append([
            r.provider,
            r.env,
            "YES" if r.has_key else "NO",
            r.api_style,
            r.model,
            "YES" if r.ok else "NO",
            str(r.status or ""),
            str(r.latency_ms),
            r.error.replace("\n", " ")[:140],
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
    have_keys = sum(1 for r in results if r.has_key)
    passed = sum(1 for r in results if r.ok)
    print(f"\nSummary: {passed}/{len(results)} passed, {have_keys}/{len(results)} have keys")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--providers", default="", help="comma-separated providers to test")
    parser.add_argument("--model", action="append", default=[], help="override model as provider=model. Can be repeated")
    parser.add_argument("--timeout", type=float, default=30.0, help="per-request timeout seconds")
    parser.add_argument("--json", action="store_true", help="print JSON output")
    parser.add_argument("--list", action="store_true", help="list default cases and exit")
    args = parser.parse_args()

    cases = select_cases(args.providers, parse_model_overrides(args.model))
    if args.list:
        for c in cases:
            envs = ",".join(c.env_names)
            print(f"{c.provider}\t{envs}\t{c.api_style}\t{c.model}\t{c.note}")
        return 0

    results = [run_case(c, args.timeout) for c in cases]
    if args.json:
        print(json.dumps([asdict(r) for r in results], ensure_ascii=False, indent=2))
    else:
        print_table(results)
    return 0 if all(r.ok or not r.has_key for r in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())

