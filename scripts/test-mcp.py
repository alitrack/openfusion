#!/usr/bin/env python3
"""Quick test: starts the MCP knowledge server and sends a test query."""
import subprocess
import json
import sys


def test_mcp_server():
    proc = subprocess.Popen(
        ["python3", "scripts/mcp-knowledge-server.py"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        cwd="/mnt/d/wsl2/dotnet/openfusion",
        env={"MCP_KB_DIR": "/home/lhy/.openfusion/knowledge", **__import__("os").environ},
    )

    def send(body: str) -> str:
        data = body.encode("utf-8")
        msg = f"Content-Length: {len(data)}\r\nContent-Type: application/json\r\n\r\n".encode("utf-8") + data
        proc.stdin.write(msg)
        proc.stdin.flush()
        # Read response
        while True:
            line = proc.stdout.readline()
            if not line:
                break
            line = line.decode().strip()
            if line.startswith("Content-Length:"):
                length = int(line.split(":")[1].strip())
            elif line == "":
                # Read body
                body_bytes = proc.stdout.read(length)
                return json.loads(body_bytes)

    # 1. Initialize
    init = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}
    })
    result = send(init)
    print(f"[init] server: {result.get('result', {}).get('serverInfo', {})}")

    # 2. List tools
    tools_req = json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
    result = send(tools_req)
    tools = result.get("result", {}).get("tools", [])
    print(f"[tools] {[t['name'] for t in tools]}")

    # 3. Search knowledge
    search_req = json.dumps({
        "jsonrpc": "2.0", "id": 3, "method": "tools/call",
        "params": {"name": "search_knowledge", "arguments": {"query": "变电站负荷预测"}}
    })
    result = send(search_req)
    text = result.get("result", {}).get("content", [{}])[0].get("text", "")
    print(f"[search] result ({len(text)} chars):")
    print(text[:300])

    proc.terminate()
    return 0


if __name__ == "__main__":
    sys.exit(test_mcp_server())
