#!/usr/bin/env python3
"""Example MCP Knowledge Server for OpenFusion.

This is a minimal MCP server that provides domain knowledge retrieval
via stdio transport. It follows the MCP specification (JSON-RPC 2.0
over Content-Length framed stdio).

Usage:
    In OpenFusion skill YAML:
        mcp_knowledge:
          sources:
            - server_cmd: "python scripts/mcp-knowledge-server.py"
              tool_name: "search_knowledge"
              max_tokens: 4000

Protocol:
    Initialize → tools/list → tools/call (search_knowledge)
    Messages use Content-Length: N framing over stdin/stdout.
"""

import json
import sys
import os
from typing import Any


def read_message() -> dict[str, Any] | None:
    """Read a JSON-RPC message from stdin with Content-Length framing."""
    content_length = 0
    while True:
        line = sys.stdin.readline()
        if not line:
            return None
        line = line.strip()
        if line.startswith("Content-Length:"):
            content_length = int(line[15:].strip())
        elif line == "":
            # End of headers
            break

    if content_length == 0:
        return None

    body = sys.stdin.read(content_length)
    return json.loads(body)


def send_message(msg: dict[str, Any]) -> None:
    """Send a JSON-RPC message to stdout with Content-Length framing."""
    body = json.dumps(msg, ensure_ascii=False)
    sys.stdout.write(f"Content-Length: {len(body.encode('utf-8'))}\r\n")
    sys.stdout.write("Content-Type: application/json\r\n")
    sys.stdout.write("\r\n")
    sys.stdout.write(body)
    sys.stdout.flush()


# ---------------------------------------------------------------------------
# Knowledge sources
# ---------------------------------------------------------------------------

def search_local_files(query: str) -> str:
    """Simple local file search for demonstration.

    Replace this with actual vector DB search, SQLite query, or
    API call to your knowledge base.
    """
    results = []
    kb_dir = os.environ.get("MCP_KB_DIR", os.path.expanduser("~/.openfusion/knowledge"))

    if os.path.isdir(kb_dir):
        keywords = query.lower().split()
        for fname in os.listdir(kb_dir):
            if fname.endswith((".md", ".txt", ".json")):
                fpath = os.path.join(kb_dir, fname)
                try:
                    with open(fpath) as f:
                        content = f.read()
                    # Simple keyword match
                    content_lower = content.lower()
                    match_count = sum(1 for kw in keywords if kw in content_lower)
                    if match_count > 0:
                        results.append({
                            "file": fname,
                            "relevance": match_count,
                            "snippet": content[:500]
                        })
                except Exception:
                    pass

    if not results:
        return f"No knowledge found for: {query}"

    # Sort by relevance and format
    results.sort(key=lambda r: r["relevance"], reverse=True)
    parts = [f"Found {len(results)} relevant documents:\n"]
    for r in results[:3]:
        parts.append(f"--- {r['file']} (score: {r['relevance']}) ---")
        parts.append(r["snippet"])
        parts.append("")

    return "\n".join(parts)


# ---------------------------------------------------------------------------
# Main MCP server loop
# ---------------------------------------------------------------------------

def main():
    message_id = 0

    while True:
        msg = read_message()
        if msg is None:
            break

        method = msg.get("method", "")
        msg_id = msg.get("id")

        if method == "initialize":
            send_message({
                "jsonrpc": "2.0",
                "id": msg_id,
                "result": {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {
                        "tools": {"listChanged": True},
                        "resources": {"listChanged": True}
                    },
                    "serverInfo": {
                        "name": "openfusion-knowledge",
                        "version": "0.1.0"
                    }
                }
            })
            # Send initialized notification (fire-and-forget)
        elif method == "notifications/initialized":
            pass  # No response needed

        elif method == "tools/list":
            send_message({
                "jsonrpc": "2.0",
                "id": msg_id,
                "result": {
                    "tools": [
                        {
                            "name": "search_knowledge",
                            "description": "Search domain knowledge base",
                            "inputSchema": {
                                "type": "object",
                                "properties": {
                                    "query": {
                                        "type": "string",
                                        "description": "Search query"
                                    },
                                    "max_tokens": {
                                        "type": "integer",
                                        "description": "Max context length"
                                    }
                                },
                                "required": ["query"]
                            }
                        }
                    ]
                }
            })

        elif method == "tools/call":
            params = msg.get("params", {})
            tool_name = params.get("name", "")
            arguments = params.get("arguments", {})

            if tool_name == "search_knowledge":
                query = arguments.get("query", "")
                result = search_local_files(query)
                send_message({
                    "jsonrpc": "2.0",
                    "id": msg_id,
                    "result": {
                        "content": [
                            {"type": "text", "text": result}
                        ],
                        "isError": False
                    }
                })
            else:
                send_message({
                    "jsonrpc": "2.0",
                    "id": msg_id,
                    "error": {
                        "code": -32601,
                        "message": f"Unknown tool: {tool_name}"
                    }
                })

        else:
            send_message({
                "jsonrpc": "2.0",
                "id": msg_id,
                "error": {
                    "code": -32601,
                    "message": f"Method not found: {method}"
                }
            })


if __name__ == "__main__":
    main()
