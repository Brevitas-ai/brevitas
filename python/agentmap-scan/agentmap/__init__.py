"""agentmap — map every AI API call in a codebase, offline. No LLM, no keys."""
from .scanner import (
    Finding, scan, call_sites, providers_found, routing,
    hardcoded_sites, apply_autofix,
)
from .agents import Agent, extract_agents, purpose_by_file

__all__ = [
    "Finding", "scan", "call_sites", "providers_found", "routing",
    "hardcoded_sites", "apply_autofix",
    "Agent", "extract_agents", "purpose_by_file",
]
__version__ = "0.1.0"
