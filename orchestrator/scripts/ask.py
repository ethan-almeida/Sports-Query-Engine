import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request


def env_int(name: str, fallback: int) -> int:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return fallback
    return int(raw)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("question", nargs="?", default=os.environ.get("Q", ""))
    parser.add_argument("--sport", default=os.environ.get("SPORT", "soccer"))
    parser.add_argument("--max-rows", type=env_int, nargs="?", default=50,
                        const=50, dest="max_rows")
    parser.add_argument("--url", default=os.environ.get("ASK_URL", "http://localhost:8080/api/v1/query"))
    parser.add_argument("--api-key", default=os.environ.get("API_KEY", ""))
    args = parser.parse_args()
    if not args.question.strip():
        print("empty question", file=sys.stderr)
        return 2

    payload = json.dumps({
        "sport": args.sport, "question": args.question.strip(), "max_rows": args.max_rows,
    }).encode()
    headers = {"Content-Type": "application/json", "Accept": "application/json"}
    if args.api_key:
        headers["X-API-Key"] = args.api_key
    request = urllib.request.Request(args.url, data=payload, headers=headers, method="POST")

    started = time.monotonic()
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            body = json.loads(response.read().decode())
            cache = response.headers.get("X-Cache", "")
            request_id = response.headers.get("X-Request-Id", "")
    except urllib.error.HTTPError as error:
        raw = error.read().decode()
        try:
            envelope = json.loads(raw)
        except json.JSONDecodeError:
            envelope = {}
        code = envelope.get("code", f"HTTP {error.code}")
        message = envelope.get("message", raw.strip() or error.reason)
        print(f"{code}: {message}", file=sys.stderr)
        return 1
    except urllib.error.URLError as error:
        print(f"orchestrator unreachable: {error.reason}", file=sys.stderr)
        return 1
    elapsed = time.monotonic() - started

    columns = body.get("columns", [])
    rows = body.get("rows", [])
    badge = cache.rjust(4)
    print(f"[{badge}] row_count={body.get('row_count', len(rows))} "
          f"pipeline={body.get('elapsed_ms', '?')}ms wall={int(elapsed * 1000)}ms "
          f"trunc={body.get('truncated', False)} req={request_id}")
    if not rows:
        print("No rows returned.")
        return 0
    widths = [min(len(str(column)), 24) for column in columns]
    for row in rows:
        for index, value in enumerate(row):
            if index < len(widths):
                widths[index] = min(max(widths[index], len(str(value))), 28)
    header = " | ".join(str(columns[index]).ljust(widths[index]) for index in range(len(widths)))
    print(header)
    print("-" * len(header))
    for row in rows[: args.max_rows]:
        print(" | ".join(str(row[index] if index < len(row) else "").ljust(widths[index])
                         for index in range(len(widths))))
    return 0


if __name__ == "__main__":
    sys.exit(main())
