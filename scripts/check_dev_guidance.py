#!/usr/bin/env python3
"""Check reviewed development Markdown in a standalone Soha checkout.

Checks local Markdown link destinations and skill name/description metadata;
remote URLs and heading fragments are not checked. Unknown paths keep full CI.
"""
import json
from pathlib import Path
import re
import sys
import subprocess
from urllib.parse import unquote, urlsplit

# Exact reviewed inputs: never exempt a directory containing executable tools.
GUIDANCE_PATHS = frozenset({
    "AGENTS.md", "CLAUDE.md", "docs/testing.md", "web/docs/testing.md",
    "web/AGENTS.md", "cli/AGENTS.md", "agent/AGENTS.md", "contracts/AGENTS.md",
    "skills/AGENTS.md", "connectors/AGENTS.md",
    ".agents/skills/soha-agent/SKILL.md",
    ".agents/skills/soha-agent/references/testing.md",
    ".agents/skills/soha-agent/references/go-engineering-standards.md",
    ".agents/skills/soha-backend/SKILL.md",
    ".agents/skills/soha-backend/references/architecture.md",
    ".agents/skills/soha-backend/references/go-engineering-standards.md",
    ".agents/skills/soha-backend/references/platform-rules.md",
    ".agents/skills/soha-backend-testing/SKILL.md",
    ".agents/skills/soha-cli/SKILL.md",
    ".agents/skills/soha-connectors/SKILL.md",
    ".agents/skills/soha-contracts/SKILL.md",
    ".agents/skills/soha-contracts/references/testing.md",
    ".agents/skills/soha-deploy/SKILL.md",
    ".agents/skills/soha-deploy/references/deployment-modes.md",
    ".agents/skills/soha-deploy/references/runtime-checklist.md",
    ".agents/skills/soha-deploy/references/security-and-build.md",
    ".agents/skills/soha-design/SKILL.md",
    ".agents/skills/soha-design/references/interaction-feedback.md",
    ".agents/skills/soha-design/references/product-experience.md",
    ".agents/skills/soha-frontend/SKILL.md",
    ".agents/skills/soha-frontend/references/architecture-development.md",
    ".agents/skills/soha-frontend/references/product-experience.md",
    ".agents/skills/soha-frontend/references/theme-system.md",
    ".agents/skills/soha-security/SKILL.md",
    ".agents/skills/soha-security/references/ocr.md",
    ".agents/skills/soha-skills/SKILL.md",
    ".agents/skills/soha-web-testing/SKILL.md",
})


def check_metadata(text, path):
    lines = text.splitlines()
    if not lines or lines[0] != "---" or "---" not in lines[1:]:
        raise ValueError("SKILL.md requires YAML front matter with name and description")
    result = subprocess.run(
        ["ruby", "-rjson", "-ryaml", "-e", "puts JSON.generate(YAML.safe_load(STDIN.read))"],
        input="\n".join(lines[1:lines[1:].index("---") + 1]), text=True, capture_output=True,
    )
    if result.returncode:
        raise ValueError("invalid skill YAML front matter: " + result.stderr.splitlines()[0])
    metadata = json.loads(result.stdout)
    if not isinstance(metadata, dict):
        raise ValueError("skill front matter must be a mapping")
    if metadata.get("name") != path.parent.name or not isinstance(metadata.get("name"), str):
        raise ValueError(f"skill name must match directory {path.parent.name!r}")
    if not isinstance(metadata.get("description"), str) or not metadata["description"].strip():
        raise ValueError("skill description must be a nonempty string")


def markdown_links(text):
    # Keep line offsets while excluding examples in fenced and inline code.
    fence = None
    lines = []
    for line in text.splitlines(keepends=True):
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})", line)
        hidden = fence is not None or marker is not None
        if marker:
            if fence is None:
                fence = marker[1]
            elif marker[1][0] == fence[0] and len(marker[1]) >= len(fence):
                fence = None
        lines.append(re.sub(r"[^\n]", " ", line) if hidden else line)
    text = "".join(lines)
    text = re.sub(r"(`+)(?!`)(.+?)(?<!`)\1(?!`)", lambda m: re.sub(r"[^\n]", " ", m[0]), text, flags=re.S)
    definitions = {match[1].strip().casefold() for match in re.finditer(r"(?m)^ {0,3}\[([^]\n]+)\]:", text)}
    for match in re.finditer(r"\[([^]\n]+)\]\[([^]\n]*)\]", text):
        label = (match[2] or match[1]).strip().casefold()
        if label not in definitions:
            raise ValueError(f"line {text.count(chr(10), 0, match.start()) + 1}: undefined Markdown reference {label!r}")
    destination = r"(<[^>\n]+>|(?:\\.|[^\s()\\]|\((?:\\.|[^()\\])*\))+?)"
    for pattern in (r"\[[^]\n]*\]\(\s*" + destination + r"(?:\s+[\"'][^\n]*[\"'])?\s*\)",
                    r"(?m)^ {0,3}\[[^]\n]+\]:\s*" + destination + r"(?=\s|$)"):
        for match in re.finditer(pattern, text):
            yield text.count("\n", 0, match.start()) + 1, match[1].strip("<>")


def check_guidance(root):
    root = root.resolve()
    errors = []
    for name in sorted(GUIDANCE_PATHS):
        path = root / name
        # Deleted/renamed documents are checked through surviving incoming links.
        if not path.exists() and not path.is_symlink():
            continue
        try:
            if path.is_symlink() or not path.resolve().is_relative_to(root):
                raise ValueError("development guidance must be a regular repository file, not a symlink")
            text = path.read_text(encoding="utf-8")
            if path.name == "SKILL.md":
                check_metadata(text, path)
            for line, destination in markdown_links(text):
                target = urlsplit(re.sub(r"\\([() ])", r"\1", destination))
                if target.scheme or target.netloc or not target.path:
                    continue
                linked = (path.parent / unquote(target.path)).resolve()
                if not linked.is_relative_to(root) or not linked.exists():
                    errors.append(f"{name}:{line}: missing or outside-repository local link {destination!r}; fix the relative path from this document")
        except (OSError, UnicodeError, ValueError) as error:
            errors.append(f"{name}: {error}")
    if errors:
        raise ValueError("\n".join(errors))


if __name__ == "__main__":
    try:
        check_guidance(Path(__file__).resolve().parents[1])
    except ValueError as error:
        sys.exit(str(error))
    print("Development guidance local links and skill metadata passed")
