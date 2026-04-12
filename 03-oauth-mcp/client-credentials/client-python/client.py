"""MCP client using the OAuth 2.0 Client Credentials extension.

Verifies the client-credentials MCP server by fetching an access token from an
external authorization server (e.g. AuthGate) via the client_credentials grant,
then connecting to the MCP server, listing tools, and calling each one.

Requires mcp >= 1.27 (the `ClientCredentialsOAuthProvider` extension).
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys

from mcp import ClientSession
from mcp.client.auth.extensions.client_credentials import (
    ClientCredentialsOAuthProvider,
)
from mcp.client.auth.oauth2 import TokenStorage
from mcp.client.streamable_http import streamablehttp_client
from mcp.shared.auth import OAuthClientInformationFull, OAuthToken


class InMemoryTokenStorage(TokenStorage):
    def __init__(self) -> None:
        self._tokens: OAuthToken | None = None

    async def get_tokens(self) -> OAuthToken | None:
        return self._tokens

    async def set_tokens(self, tokens: OAuthToken) -> None:
        self._tokens = tokens

    # ClientCredentialsOAuthProvider supplies a fixed client_info internally,
    # so storage's client-info methods are never exercised.
    async def get_client_info(self) -> OAuthClientInformationFull | None:
        return None

    async def set_client_info(self, client_info: OAuthClientInformationFull) -> None:
        pass


def _print_tool_result(name: str, result) -> None:
    if getattr(result, "isError", False):
        print(f"[{name}] reported an error", file=sys.stderr)
    for item in result.content:
        text = getattr(item, "text", None)
        if text is not None:
            print(f"[{name}] text: {text}")
    structured = getattr(result, "structuredContent", None)
    if structured:
        print(f"[{name}] structured: {json.dumps(structured, ensure_ascii=False)}")


async def run(args: argparse.Namespace) -> None:
    provider = ClientCredentialsOAuthProvider(
        server_url=args.mcp_url,
        storage=InMemoryTokenStorage(),
        client_id=args.client_id,
        client_secret=args.client_secret,
        token_endpoint_auth_method=args.auth_method,
        scopes=args.scopes,
    )

    print(f"connecting to {args.mcp_url} ...", file=sys.stderr)
    async with streamablehttp_client(args.mcp_url, auth=provider) as (
        read_stream,
        write_stream,
        _,
    ):
        async with ClientSession(read_stream, write_stream) as session:
            init = await session.initialize()
            print(
                f"connected: {init.serverInfo.name} v{init.serverInfo.version}",
                file=sys.stderr,
            )

            tools = await session.list_tools()
            print(f"available tools: {[t.name for t in tools.tools]}")

            echo = await session.call_tool(
                "echo_message",
                {"message": "hello from python-sdk"},
            )
            _print_tool_result("echo_message", echo)

            add = await session.call_tool("add_numbers", {"a": 21, "b": 21})
            _print_tool_result("add_numbers", add)

    print("verification complete", file=sys.stderr)


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument(
        "--mcp-url",
        default="http://localhost:8096/mcp",
        help="MCP streamable HTTP endpoint",
    )
    p.add_argument("--client-id", default="my-service", help="OAuth 2.0 client id")
    p.add_argument("--client-secret", default="s3cr3t", help="OAuth 2.0 client secret")
    p.add_argument(
        "--scopes",
        default="mcp:read mcp:write",
        help="space-separated scopes to request",
    )
    p.add_argument(
        "--auth-method",
        choices=["client_secret_basic", "client_secret_post"],
        default="client_secret_basic",
        help="token-endpoint authentication method",
    )
    return p.parse_args()


if __name__ == "__main__":
    asyncio.run(run(_parse_args()))
