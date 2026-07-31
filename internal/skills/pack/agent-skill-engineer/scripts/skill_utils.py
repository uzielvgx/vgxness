#!/usr/bin/env python3
"""Shared, dependency-light utilities for Agent Skill tooling."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any


NAME_RE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")


class YamlSubsetError(ValueError):
    """Raised when fallback YAML parsing encounters unsupported syntax."""


@dataclass(frozen=True)
class YamlLoadResult:
    data: Any
    backend: str


def _strip_inline_comment(value: str) -> str:
    quote: str | None = None
    escaped = False
    for index, char in enumerate(value):
        if escaped:
            escaped = False
            continue
        if char == "\\" and quote == '"':
            escaped = True
            continue
        if char in {"'", '"'}:
            if quote is None:
                quote = char
            elif quote == char:
                quote = None
            continue
        if char == "#" and quote is None and (index == 0 or value[index - 1].isspace()):
            return value[:index].rstrip()
    return value.rstrip()


def _parse_scalar(value: str) -> Any:
    value = _strip_inline_comment(value.strip())
    if not value:
        return ""
    if value[0] in {'"', "'"}:
        if value[0] == '"':
            try:
                return json.loads(value)
            except json.JSONDecodeError as exc:
                raise YamlSubsetError(f"Invalid quoted string: {value}") from exc
        if len(value) < 2 or value[-1] != "'":
            raise YamlSubsetError(f"Unclosed quoted string: {value}")
        return value[1:-1].replace("''", "'")
    lowered = value.lower()
    if lowered in {"true", "false"}:
        return lowered == "true"
    if lowered in {"null", "~"}:
        return None
    if re.fullmatch(r"-?(?:0|[1-9][0-9]*)", value):
        return int(value)
    if re.fullmatch(r"-?(?:0|[1-9][0-9]*)\.[0-9]+", value):
        return float(value)
    if value.startswith("[") or value.startswith("{"):
        try:
            return json.loads(value)
        except json.JSONDecodeError as exc:
            raise YamlSubsetError(
                "Fallback YAML parser supports inline collections only when valid JSON."
            ) from exc
    return value


def _tokenize_yaml(text: str) -> list[tuple[int, str, int]]:
    tokens: list[tuple[int, str, int]] = []
    for line_number, raw in enumerate(text.splitlines(), start=1):
        if "\t" in raw[: len(raw) - len(raw.lstrip())]:
            raise YamlSubsetError(f"Tabs are not supported for indentation at line {line_number}.")
        stripped = raw.strip()
        if not stripped or stripped.startswith("#") or stripped in {"---", "..."}:
            continue
        indent = len(raw) - len(raw.lstrip(" "))
        tokens.append((indent, raw[indent:], line_number))
    return tokens


def _split_mapping(text: str, line_number: int) -> tuple[str, str]:
    quote: str | None = None
    escaped = False
    for index, char in enumerate(text):
        if escaped:
            escaped = False
            continue
        if char == "\\" and quote == '"':
            escaped = True
            continue
        if char in {"'", '"'}:
            if quote is None:
                quote = char
            elif quote == char:
                quote = None
            continue
        if char == ":" and quote is None:
            key = text[:index].strip()
            if not key:
                raise YamlSubsetError(f"Empty mapping key at line {line_number}.")
            return key.strip("\"'"), text[index + 1 :].strip()
    raise YamlSubsetError(f"Expected a key-value pair at line {line_number}.")


def parse_yaml_subset(text: str) -> Any:
    """Parse the conservative YAML subset used by Agent Skill metadata."""

    tokens = _tokenize_yaml(text)
    if not tokens:
        return {}

    def parse_block(index: int, indent: int) -> tuple[Any, int]:
        if index >= len(tokens):
            return {}, index
        is_list = tokens[index][1].startswith("- ")
        container: Any = [] if is_list else {}

        while index < len(tokens):
            current_indent, content, line_number = tokens[index]
            if current_indent < indent:
                break
            if current_indent > indent:
                raise YamlSubsetError(f"Unexpected indentation at line {line_number}.")

            if is_list:
                if not content.startswith("- "):
                    break
                item_text = content[2:].strip()
                if not item_text:
                    if index + 1 >= len(tokens) or tokens[index + 1][0] <= indent:
                        container.append(None)
                        index += 1
                    else:
                        child_indent = tokens[index + 1][0]
                        child, index = parse_block(index + 1, child_indent)
                        container.append(child)
                    continue

                if ":" in item_text:
                    key, raw_value = _split_mapping(item_text, line_number)
                    item: dict[str, Any] = {}
                    if raw_value in {"|", ">"}:
                        raise YamlSubsetError(
                            f"Block scalars inside list mappings are unsupported at line {line_number}."
                        )
                    item[key] = _parse_scalar(raw_value) if raw_value else None
                    index += 1
                    while index < len(tokens) and tokens[index][0] > indent:
                        child_indent, child_text, child_line = tokens[index]
                        child_key, child_value = _split_mapping(child_text, child_line)
                        if child_value:
                            item[child_key] = _parse_scalar(child_value)
                            index += 1
                        elif index + 1 < len(tokens) and tokens[index + 1][0] > child_indent:
                            nested, index = parse_block(index + 1, tokens[index + 1][0])
                            item[child_key] = nested
                        else:
                            item[child_key] = {}
                            index += 1
                    container.append(item)
                else:
                    container.append(_parse_scalar(item_text))
                    index += 1
                continue

            if content.startswith("- "):
                break
            key, raw_value = _split_mapping(content, line_number)
            if key in container:
                raise YamlSubsetError(f"Duplicate key '{key}' at line {line_number}.")

            if raw_value in {"|", ">"}:
                style = raw_value
                index += 1
                scalar_lines: list[str] = []
                while index < len(tokens) and tokens[index][0] > indent:
                    scalar_lines.append(tokens[index][1])
                    index += 1
                container[key] = (
                    "\n".join(scalar_lines) if style == "|" else " ".join(scalar_lines)
                )
                continue

            if raw_value:
                container[key] = _parse_scalar(raw_value)
                index += 1
                continue

            if index + 1 < len(tokens) and tokens[index + 1][0] > indent:
                child_indent = tokens[index + 1][0]
                child, index = parse_block(index + 1, child_indent)
                container[key] = child
            else:
                container[key] = {}
                index += 1

        return container, index

    parsed, final_index = parse_block(0, tokens[0][0])
    if final_index != len(tokens):
        _, _, line_number = tokens[final_index]
        raise YamlSubsetError(f"Could not parse YAML near line {line_number}.")
    return parsed


def load_yaml(text: str) -> YamlLoadResult:
    """Load YAML with PyYAML when available, otherwise use the safe subset parser."""

    try:
        import yaml  # type: ignore
    except ModuleNotFoundError:
        return YamlLoadResult(parse_yaml_subset(text), "builtin-subset")

    try:
        return YamlLoadResult(yaml.safe_load(text), "pyyaml")
    except yaml.YAMLError as exc:  # type: ignore[attr-defined]
        raise YamlSubsetError(str(exc)) from exc


def split_frontmatter(text: str) -> tuple[str, str]:
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        raise YamlSubsetError("SKILL.md must begin with YAML frontmatter delimited by ---.")
    for index, line in enumerate(lines[1:], start=1):
        if line.strip() == "---":
            return "\n".join(lines[1:index]), "\n".join(lines[index + 1 :]).strip()
    raise YamlSubsetError("SKILL.md frontmatter has no closing --- delimiter.")


def load_skill_metadata(skill_file: Path) -> tuple[dict[str, Any], str, str]:
    frontmatter, body = split_frontmatter(skill_file.read_text(encoding="utf-8"))
    loaded = load_yaml(frontmatter)
    if not isinstance(loaded.data, dict):
        raise YamlSubsetError("SKILL.md frontmatter must be a YAML mapping.")
    return loaded.data, body, loaded.backend


def strip_fenced_code(text: str) -> str:
    """Remove fenced code blocks before inspecting prose links or directives."""

    output: list[str] = []
    fence: str | None = None
    for line in text.splitlines():
        match = re.match(r"^\s*(`{3,}|~{3,})", line)
        if match:
            marker = match.group(1)[0]
            if fence is None:
                fence = marker
            elif fence == marker:
                fence = None
            output.append("")
            continue
        output.append("" if fence else line)
    return "\n".join(output)


def validate_skill_name(name: str) -> list[str]:
    errors: list[str] = []
    if not name:
        return ["Skill name is required."]
    if len(name) > 64:
        errors.append("Skill name must be 64 characters or fewer.")
    if not NAME_RE.fullmatch(name):
        errors.append(
            "Skill name must contain lowercase letters, digits, and single hyphens only."
        )
    return errors


def title_from_name(name: str) -> str:
    return " ".join(part.capitalize() for part in name.split("-"))


def yaml_quote(value: str) -> str:
    """Return a JSON-quoted string, which is also a valid YAML scalar."""

    return json.dumps(value, ensure_ascii=False)
