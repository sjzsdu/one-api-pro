#!/usr/bin/env python3
"""oneapi.wnbgj.com CLI: 模型发现 / 按操作调用 / 通用请求 / 视频任务。

仅使用 Python 标准库。凭据从环境变量读取，脚本不内置任何 token：
  ONEAPI_BASE_URL      默认 https://oneapi.wnbgj.com
  ONEAPI_API_KEY       sk- 模型 token，调用 /v1/* 推理端点
  ONEAPI_ACCESS_TOKEN  账号 token，调用 /api/* 管理端点（模型发现等）
"""

import argparse
import base64
import json
import os
import sys
import uuid
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

BASE = os.environ.get("ONEAPI_BASE_URL", "https://oneapi.wnbgj.com").rstrip("/")
API_KEY = os.environ.get("ONEAPI_API_KEY", "")
ACCESS_TOKEN = os.environ.get("ONEAPI_ACCESS_TOKEN", "")

OPERATION_PATHS = {
    "chat.generate": "/v1/chat/completions",
    "chat": "/v1/chat/completions",
    "image.generate": "/v1/images/generations",
    "image.edit": "/v1/images/edits",
    "audio.speech": "/v1/audio/speech",
    "audio.transcribe": "/v1/audio/transcriptions",
    "embedding.create": "/v1/embeddings",
    "embedding": "/v1/embeddings",
    "rerank.score": "/v1/rerank",
    "rerank": "/v1/rerank",
    "moderation.check": "/v1/moderations",
    "video.generate": "/v1/media/jobs",
}


def die(message):
    print(f"error: {message}", file=sys.stderr)
    sys.exit(1)


def require(env_name, current):
    if not current:
        die(f"环境变量 {env_name} 未设置，请先设置凭据（本脚本不会创建 token）")
    return current


def http(path, method="GET", body=None, admin=False, extra_headers=None, timeout=300):
    token = ACCESS_TOKEN if admin else API_KEY
    require("ONEAPI_ACCESS_TOKEN" if admin else "ONEAPI_API_KEY", token)
    headers = {"Authorization": f"Bearer {token}"}
    data = None
    if body is not None:
        if isinstance(body, (dict, list)):
            body = json.dumps(body, ensure_ascii=False)
        data = body.encode("utf-8")
        headers["Content-Type"] = "application/json"
    if extra_headers:
        headers.update(extra_headers)
    req = Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urlopen(req, timeout=timeout) as resp:
            return resp.status, dict(resp.headers), resp.read()
    except HTTPError as exc:
        raw = exc.read()
        detail = raw.decode("utf-8", "replace")
        die(f"HTTP {exc.code} {method} {path}: {detail[:2000]}")
    except URLError as exc:
        die(f"网络错误 {method} {path}: {exc.reason}")


def parse_body(raw):
    text = raw.decode("utf-8", "replace")
    try:
        return json.loads(text)
    except ValueError:
        return text


def print_json(obj):
    print(json.dumps(obj, ensure_ascii=False, indent=2))


def save_or_print(raw, headers, out):
    """二进制直接写文件；JSON 里的 b64_json 也可落盘。"""
    content_type = headers.get("Content-Type", "")
    if "json" in content_type or raw[:1] in (b"{", b"["):
        obj = parse_body(raw)
        if out and isinstance(obj, dict):
            images = obj.get("data") or []
            saved = []
            for idx, item in enumerate(images):
                b64 = item.get("b64_json") if isinstance(item, dict) else None
                if b64:
                    path = out if len(images) == 1 else f"{out}.{idx + 1}.png"
                    with open(path, "wb") as handle:
                        handle.write(base64.b64decode(b64))
                    saved.append(path)
            if saved:
                print("saved: " + ", ".join(saved), file=sys.stderr)
        print_json(obj)
    else:
        if not out:
            sys.stdout.buffer.write(raw)
            return
        with open(out, "wb") as handle:
            handle.write(raw)
        print(f"saved: {out} ({len(raw)} bytes)", file=sys.stderr)


def show_headers(headers):
    for key, label in (("X-Inference-Id", "inference_id"), ("x-should-retry", "should_retry"),
                       ("X-Inference-Status", "inference_status"), ("Idempotency-Key", "idempotency_key")):
        value = next((v for k, v in headers.items() if k.lower() == key.lower()), None)
        if value is not None:
            print(f"{label}: {value}", file=sys.stderr)


def cmd_models(args):
    if args.fallback:
        _, _, raw = http("/v1/models", extra_headers=None)
        obj = parse_body(raw)
        if args.json:
            print_json(obj)
            return
        rows = obj.get("data", []) if isinstance(obj, dict) else []
        print(f"{'MODEL':<48} OWNED_BY")
        for item in rows:
            print(f"{item.get('id', ''):<48} {item.get('owned_by', '')}")
        print("\n提示：/v1/models 不含能力信息。设置 ONEAPI_ACCESS_TOKEN 后可查看能力。", file=sys.stderr)
        return

    path = "/api/model/options"
    if args.operation:
        from urllib.parse import urlencode
        path += "?" + urlencode({"operation": args.operation})
    _, _, raw = http(path, admin=True)
    obj = parse_body(raw)
    if args.json:
        print_json(obj)
        return
    rows = obj.get("data", []) if isinstance(obj, dict) else []
    print(f"{'MODEL':<38} {'CATEGORY':<12} OPERATIONS (MODES)")
    for item in rows:
        caps = item.get("capabilities") or {}
        ops = caps.get("operations") or {}
        support = item.get("gateway_support") or {}
        parts = []
        for name, spec in ops.items():
            modes = (support.get(name) or (spec.get("modes") if isinstance(spec, dict) else None) or [])
            parts.append(f"{name}[{','.join(modes)}]" if modes else name)
        print(f"{item.get('model_name', ''):<38} {caps.get('primary_category', ''):<12} {', '.join(parts)}")


def cmd_call(args):
    path = OPERATION_PATHS.get(args.operation)
    if not path:
        die(f"未知操作 {args.operation}；可选：{', '.join(sorted(OPERATION_PATHS))}")
    params = json.loads(args.params) if args.params else {}
    if not isinstance(params, dict):
        die("--params 必须是 JSON 对象")
    params["model"] = args.model
    extra = {}
    if args.operation in ("video.generate", "video"):
        extra["Idempotency-Key"] = args.idempotency_key or str(uuid.uuid4())
    status, headers, raw = http(path, method="POST", body=params, extra_headers=extra, timeout=args.timeout)
    show_headers(headers)
    save_or_print(raw, headers, args.out)


def cmd_job(args):
    if args.job_action == "create":
        if not args.model or not args.prompt:
            die("job create 需要 --model 和 --prompt")
        body = {"model": args.model, "operation": "video.generate", "prompt": args.prompt}
        for key in ("size", "duration", "seed", "negative_prompt"):
            value = getattr(args, key)
            if value is not None:
                body[key] = value
        status, headers, raw = http("/v1/media/jobs", method="POST", body=body,
                                    extra_headers={"Idempotency-Key": args.idempotency_key or str(uuid.uuid4())})
        show_headers(headers)
        print_json(parse_body(raw))
        return
    job_id = args.job_id or die("job get/cancel/asset 需要 --id")
    if args.job_action == "get":
        _, _, raw = http(f"/v1/media/jobs/{job_id}")
        print_json(parse_body(raw))
    elif args.job_action == "cancel":
        _, _, raw = http(f"/v1/media/jobs/{job_id}/cancel", method="POST", body={})
        print_json(parse_body(raw))
    elif args.job_action == "asset":
        status, headers, raw = http(f"/v1/media/assets/{job_id}")
        save_or_print(raw, headers, args.out)


def cmd_request(args):
    body = None
    if args.data:
        if args.data.startswith("@"):
            with open(args.data[1:], "r", encoding="utf-8") as handle:
                body = handle.read()
        else:
            body = args.data
    extra = {}
    for item in args.header or []:
        if "=" not in item:
            die(f"--header 格式应为 KEY=VALUE，收到 {item}")
        key, value = item.split("=", 1)
        extra[key] = value
    status, headers, raw = http(args.path, method=args.method, body=body,
                                admin=args.admin, extra_headers=extra, timeout=args.timeout)
    show_headers(headers)
    save_or_print(raw, headers, args.out)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)

    p_models = sub.add_parser("models", help="发现可用模型及能力")
    p_models.add_argument("--operation", help="只列支持该操作的模型，如 audio.speech")
    p_models.add_argument("--fallback", action="store_true", help="用 /v1/models（无能力信息，只需 API_KEY）")
    p_models.add_argument("--json", action="store_true", help="输出原始 JSON")
    p_models.set_defaults(func=cmd_models)

    p_call = sub.add_parser("call", help="按操作调用模型")
    p_call.add_argument("--operation", required=True, help="如 chat.generate / image.generate / audio.speech")
    p_call.add_argument("--model", required=True)
    p_call.add_argument("--params", help="请求体 JSON 对象（会自动补 model 字段）")
    p_call.add_argument("--out", help="二进制输出文件；b64_json 图片也会保存到此")
    p_call.add_argument("--idempotency-key", help="video.generate 的幂等键，缺省自动生成")
    p_call.add_argument("--timeout", type=int, default=300)
    p_call.set_defaults(func=cmd_call)

    p_job = sub.add_parser("job", help="视频任务 create/get/cancel/asset")
    p_job.add_argument("job_action", choices=["create", "get", "cancel", "asset"])
    p_job.add_argument("--id", dest="job_id", help="任务 ID（get/cancel/asset）")
    p_job.add_argument("--model", help="create: 模型名")
    p_job.add_argument("--prompt", help="create: 提示词")
    p_job.add_argument("--size", help="如 1280*720")
    p_job.add_argument("--duration", type=int, help="2–15 秒")
    p_job.add_argument("--seed", type=int)
    p_job.add_argument("--negative-prompt", dest="negative_prompt")
    p_job.add_argument("--idempotency-key")
    p_job.add_argument("--out", help="asset: 输出文件")
    p_job.set_defaults(func=cmd_job)

    p_req = sub.add_parser("request", help="通用原始请求")
    p_req.add_argument("--path", required=True, help="如 /v1/audio/translations 或 /api/model/options")
    p_req.add_argument("--method", default="GET")
    p_req.add_argument("--data", help="JSON 字符串或 @文件路径")
    p_req.add_argument("--admin", action="store_true", help="用 ACCESS_TOKEN 调 /api/*")
    p_req.add_argument("--header", action="append", help="附加请求头 KEY=VALUE，可重复")
    p_req.add_argument("--out", help="二进制输出文件")
    p_req.add_argument("--timeout", type=int, default=300)
    p_req.set_defaults(func=cmd_request)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
