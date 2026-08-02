"""
agentmap CLI.

    agentmap [PATH]            scan a repo → open a webpage of its AI calls (default)
    agentmap install [PATH]    write routing env vars; --auto rewrites hardcoded URLs
"""
from __future__ import annotations

from collections import Counter
from pathlib import Path

import click

from .scanner import scan, call_sites, routing, hardcoded_sites, apply_autofix
from .agents import extract_agents, purpose_by_file
from .report import render_and_open

_click_echo = click.echo


def _echo(msg: str = "") -> None:
    """click.echo that degrades to ASCII on legacy Windows consoles (cp1252 etc.)."""
    try:
        _click_echo(msg)
    except UnicodeEncodeError:
        _click_echo(str(msg).encode("ascii", "replace").decode("ascii"))


class DefaultGroup(click.Group):
    """Group that runs `scan` when the first arg isn't a known subcommand.

    Lets `agentmap PATH --no-open` work (PATH is not a command → falls to scan)
    while `agentmap install …` still resolves normally.
    """
    def resolve_command(self, ctx, args):
        try:
            return super().resolve_command(ctx, args)
        except click.UsageError:
            return "scan", self.get_command(ctx, "scan"), args


@click.group(cls=DefaultGroup, invoke_without_command=True)
@click.pass_context
def main(ctx: click.Context) -> None:
    """Map every AI API call in a codebase. Bare `agentmap` scans the current dir."""
    if ctx.invoked_subcommand is None:
        ctx.invoke(scan_cmd)


@main.command(name="scan")
@click.argument("path", default=".")
@click.option("--target", default="http://localhost:4242", show_default=True,
              help="Gateway URL to route calls through")
@click.option("--open/--no-open", "open_", default=True, help="Open the report in a browser")
def scan_cmd(path: str, target: str, open_: bool) -> None:
    """Scan PATH and open a webpage of its AI calls, models, and agents."""
    try:
        findings = scan(path)
    except FileNotFoundError as e:
        raise click.ClickException(str(e))
    calls = call_sites(findings)
    plan = routing(findings, proxy=target)
    agents = extract_agents(path)
    purposes = purpose_by_file(agents)

    counts = Counter(f.provider for f in calls)
    _echo(f"\n{len(calls)} AI call sites · {len(counts)} providers · {len(agents)} agents")
    for pid, n in counts.most_common():
        tag = "routed" if pid in plan["auto"] else "manual"
        _echo(f"  {n:>4}  {pid:<16} ({tag})")
    if agents:
        _echo("\nagents:")
        for a in agents:
            _echo(f"  {a.name:<16} {a.role}")

    out = render_and_open(path, findings, plan, agents, purposes, open_browser=open_)
    _echo(f"\n✓ Report: {out}" + ("" if open_ else "  (drop --no-open to launch it)"))


@main.command()
@click.argument("path", default=".")
@click.option("--target", default="http://localhost:4242", show_default=True, help="Gateway URL")
@click.option("--auto", is_flag=True, help="Rewrite hardcoded provider URLs in place (edits your files)")
@click.option("--env-file", default=".env.agentmap", show_default=True, help="Where to write routing env vars")
def install(path: str, target: str, auto: bool, env_file: str) -> None:
    """Route PATH's calls through the gateway: write env vars + handle hardcoded URLs."""
    try:
        findings = scan(path)
    except FileNotFoundError as e:
        raise click.ClickException(str(e))
    plan = routing(findings, proxy=target)
    hard = hardcoded_sites(findings)
    if not plan["env"] and not hard:
        _echo("Nothing to route — no OpenAI/Anthropic-compatible calls found.")
        return

    lines = ["# agentmap routing — `source` this before running your app\n"]
    lines += [f"export {k}={v}\n" for k, v in plan["env"].items()]
    try:
        Path(env_file).write_text("".join(lines))
    except OSError as e:
        raise click.ClickException(f"cannot write {env_file}: {e}")
    _echo(f"\n✓ Wrote {env_file}  →  source {env_file}")
    for k, v in plan["env"].items():
        _echo(f"    {k}={v}")
    if plan["manual"]:
        _echo(f"\nManual (own SDK, point base_url at {target}): {', '.join(plan['manual'])}")

    if auto:
        edits = apply_autofix(findings, proxy=target)
        _echo(f"\n✓ Rewrote {len(edits)} hardcoded URL(s)")
        for pth, ln, new in edits[:20]:
            _echo(f"    {pth}:{ln}  {new}")
    elif hard:
        _echo(f"\n{len(hard)} hardcoded URL(s) — env vars can't override these. Edit or re-run with --auto:")
        for f in hard[:20]:
            _echo(f"    {f.path}:{f.line}  {f.snippet}")


if __name__ == "__main__":
    main()
