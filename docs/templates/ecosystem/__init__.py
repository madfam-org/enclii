"""ECOSYSTEM.md template package.

Usage:

    from docs.templates.ecosystem import REPOS_FULL, render

Or regenerate all files:

    python3 -m docs.templates.ecosystem.generator
"""
from .metadata import REPOS_FULL
from .generator import render

__all__ = ["REPOS_FULL", "render"]
