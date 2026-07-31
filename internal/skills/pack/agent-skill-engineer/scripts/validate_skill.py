#!/usr/bin/env python3
"""Validate Agent Skill structure, metadata, resources, and hygiene."""

from __future__ import annotations

import argparse
import ast
import json
import re
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

from skill_utils import (
    YamlSubsetError,
    load_skill_metadata,
    load_yaml,
    strip_fenced_code,
    validate_skill_name,
)


LINK_RE = re.compile(r"\[[^\]]+\]\(([^)]+)\)")
SEMVER_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$")
HEX_COLOR_RE = re.compile(r"^#[0-9A-Fa-f]{6}$")
ALLOWED_FRONTMATTER = {
    "name",
    "description",
    "license",
    "compatibility",
    "metadata",
    "allowed-tools",
}
SECRET_PATTERNS = {
    "OpenAI-style API key": re.compile(r"\bsk-[A-Za-z0-9_-]{20,}\b"),
    "GitHub token": re.compile(r"\bgh[pousr]_[A-Za-z0-9]{20,}\b"),
    "Private key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    "Generic secret assignment": re.compile(
        r"(?i)\b(?:api[_-]?key|token|password|secret)\s*[:=]\s*['\"][^'\"]{8,}['\"]"
    ),
}


@dataclass
class ValidationReport:
    skill: str
    yaml_backend: str
    errors: list[str]
    warnings: list[str]

    @property
    def passed(self) -> bool:
        return not self.errors


def inspect_frontmatter(
    skill_dir: Path, metadata: dict[str, Any], body: str
) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    warnings: list[str] = []

    name = metadata.get("name")
    description = metadata.get("description")

    if not isinstance(name, str):
        errors.append("Frontmatter 'name' must be a non-empty string.")
        name = ""
    else:
        errors.extend(validate_skill_name(name.strip()))
        name = name.strip()

    if name and skill_dir.name != name:
        warnings.append(
            f"Directory name '{skill_dir.name}' does not match skill name '{name}'."
        )

    if not isinstance(description, str) or not description.strip():
        errors.append("Frontmatter 'description' must be a non-empty string.")
    else:
        description = description.strip()
        if len(description) > 1024:
            errors.append("'description' must be 1024 characters or fewer.")
        if re.search(r"<[^>]+>", description):
            errors.append("'description' must not contain XML or HTML tags.")
        lowered = description.lower()
        if "use when" not in lowered and "use for" not in lowered:
            warnings.append(
                "'description' should explicitly state when to use the skill."
            )

    license_value = metadata.get("license")
    if license_value is not None and not isinstance(license_value, str):
        errors.append("Optional 'license' must be a string.")

    compatibility = metadata.get("compatibility")
    if compatibility is not None:
        if not isinstance(compatibility, str):
            errors.append("Optional 'compatibility' must be a string.")
        elif not 1 <= len(compatibility.strip()) <= 500:
            errors.append("'compatibility' must contain 1–500 characters.")

    custom_metadata = metadata.get("metadata")
    if custom_metadata is not None:
        if not isinstance(custom_metadata, dict):
            errors.append("Optional 'metadata' must be a mapping.")
        elif any(
            not isinstance(key, str) or not isinstance(value, str)
            for key, value in custom_metadata.items()
        ):
            errors.append("'metadata' keys and values must all be strings.")

    allowed_tools = metadata.get("allowed-tools")
    if allowed_tools is not None and not isinstance(allowed_tools, str):
        errors.append("Optional 'allowed-tools' must be a space-separated string.")

    unknown = sorted(set(metadata) - ALLOWED_FRONTMATTER)
    if unknown:
        warnings.append(
            "Unknown or host-specific frontmatter keys: " + ", ".join(unknown)
        )

    if not body.strip():
        errors.append("SKILL.md has no instruction body.")

    return errors, warnings


def inspect_links(skill_dir: Path, skill_text: str) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    warnings: list[str] = []
    prose = strip_fenced_code(skill_text)
    for target in LINK_RE.findall(prose):
        if re.match(r"^[a-z]+://", target) or target.startswith(("#", "mailto:")):
            continue
        clean = target.split("#", 1)[0]
        resolved = (skill_dir / clean).resolve()
        try:
            resolved.relative_to(skill_dir.resolve())
        except ValueError:
            warnings.append(f"Link points outside the skill directory: {target}")
            continue
        if not resolved.exists():
            errors.append(f"Referenced file does not exist: {target}")
        if clean.startswith("references/") and clean.count("/") > 1:
            warnings.append(f"Reference is nested more than one level: {target}")
    return errors, warnings


def inspect_openai_yaml(
    skill_dir: Path, skill_name: str
) -> tuple[list[str], list[str], str | None]:
    errors: list[str] = []
    warnings: list[str] = []
    path = skill_dir / "agents" / "openai.yaml"
    if not path.exists():
        return errors, ["agents/openai.yaml is not present."], None

    try:
        loaded = load_yaml(path.read_text(encoding="utf-8"))
    except YamlSubsetError as exc:
        return [f"agents/openai.yaml is invalid: {exc}"], warnings, None

    data = loaded.data
    if not isinstance(data, dict):
        return ["agents/openai.yaml must contain a mapping."], warnings, loaded.backend

    interface = data.get("interface")
    if not isinstance(interface, dict):
        errors.append("agents/openai.yaml must define an 'interface' mapping.")
        return errors, warnings, loaded.backend

    display_name = interface.get("display_name")
    short_description = interface.get("short_description")
    default_prompt = interface.get("default_prompt")

    if not isinstance(display_name, str) or not display_name.strip():
        warnings.append("interface.display_name should be a non-empty string.")
    if short_description is None:
        warnings.append("interface.short_description is not present.")
    elif not isinstance(short_description, str):
        errors.append("interface.short_description must be a string.")
    elif not 25 <= len(short_description.strip()) <= 64:
        warnings.append("interface.short_description should contain 25–64 characters.")
    if default_prompt is None:
        warnings.append("interface.default_prompt is not present.")
    elif not isinstance(default_prompt, str) or not default_prompt.strip():
        errors.append("interface.default_prompt must be a non-empty string when provided.")
    elif f"${skill_name}" not in default_prompt:
        errors.append(f"interface.default_prompt must mention '${skill_name}'.")

    for field in ("icon_small", "icon_large"):
        value = interface.get(field)
        if value is None:
            continue
        if not isinstance(value, str):
            errors.append(f"interface.{field} must be a string path.")
            continue
        target = (skill_dir / value).resolve()
        try:
            target.relative_to(skill_dir.resolve())
        except ValueError:
            errors.append(f"interface.{field} points outside the skill directory.")
            continue
        if not target.exists():
            errors.append(f"interface.{field} does not exist: {value}")

    brand_color = interface.get("brand_color")
    if brand_color is not None and (
        not isinstance(brand_color, str) or not HEX_COLOR_RE.fullmatch(brand_color)
    ):
        errors.append("interface.brand_color must be a six-digit hex color.")

    policy = data.get("policy")
    if policy is not None:
        if not isinstance(policy, dict):
            errors.append("policy must be a mapping.")
        elif "allow_implicit_invocation" in policy and not isinstance(
            policy["allow_implicit_invocation"], bool
        ):
            errors.append("policy.allow_implicit_invocation must be boolean.")

    dependencies = data.get("dependencies")
    if dependencies is not None:
        if not isinstance(dependencies, dict) or not isinstance(
            dependencies.get("tools"), list
        ):
            errors.append("dependencies.tools must be a list.")
        else:
            for index, tool in enumerate(dependencies["tools"]):
                if not isinstance(tool, dict):
                    errors.append(f"dependencies.tools[{index}] must be a mapping.")
                    continue
                if tool.get("type") != "mcp":
                    errors.append(
                        f"dependencies.tools[{index}].type must currently be 'mcp'."
                    )
                for key in ("value", "description"):
                    if not isinstance(tool.get(key), str) or not tool[key].strip():
                        errors.append(
                            f"dependencies.tools[{index}].{key} must be a non-empty string."
                        )

    return errors, warnings, loaded.backend


def inspect_manifest(skill_dir: Path) -> tuple[list[str], list[str]]:
    path = skill_dir / "skill-manifest.json"
    if not path.exists():
        return [], ["skill-manifest.json is not present; lifecycle metadata is unavailable."]
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return [f"skill-manifest.json is invalid JSON: {exc}"], []
    if not isinstance(data, dict):
        return ["skill-manifest.json must contain an object."], []

    errors: list[str] = []
    warnings: list[str] = []
    version = data.get("version")
    if not isinstance(version, str) or not SEMVER_RE.fullmatch(version):
        errors.append("skill-manifest.json 'version' must use semantic versioning.")
    for field in ("provenance", "license_status"):
        if not isinstance(data.get(field), str) or not data[field].strip():
            errors.append(f"skill-manifest.json '{field}' must be a non-empty string.")
    owner = data.get("owner")
    if owner in {None, "", "unspecified"}:
        warnings.append("skill-manifest.json has no accountable owner.")
    return errors, warnings


def inspect_python(skill_dir: Path) -> list[str]:
    errors: list[str] = []
    for path in sorted((skill_dir / "scripts").glob("*.py")) if (
        skill_dir / "scripts"
    ).is_dir() else []:
        try:
            ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        except (SyntaxError, UnicodeDecodeError) as exc:
            errors.append(f"Python script is invalid: {path.name}: {exc}")
    return errors


def inspect_secrets(skill_dir: Path) -> list[str]:
    findings: list[str] = []
    for path in sorted(skill_dir.rglob("*")):
        if not path.is_file() or path.stat().st_size > 2_000_000:
            continue
        try:
            content = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        for label, pattern in SECRET_PATTERNS.items():
            if pattern.search(content):
                findings.append(f"{label} pattern found in {path.relative_to(skill_dir)}")
    return findings


def validate(skill_dir: Path) -> ValidationReport:
    errors: list[str] = []
    warnings: list[str] = []
    yaml_backends: set[str] = set()
    skill_file = skill_dir / "SKILL.md"

    if not skill_dir.is_dir():
        return ValidationReport(str(skill_dir), "none", [f"Directory not found: {skill_dir}"], [])
    if not skill_file.is_file():
        return ValidationReport(str(skill_dir), "none", ["Missing required SKILL.md."], [])

    text = skill_file.read_text(encoding="utf-8")
    try:
        metadata, body, backend = load_skill_metadata(skill_file)
        yaml_backends.add(backend)
    except YamlSubsetError as exc:
        return ValidationReport(str(skill_dir), "invalid", [str(exc)], [])

    current_errors, current_warnings = inspect_frontmatter(skill_dir, metadata, body)
    errors.extend(current_errors)
    warnings.extend(current_warnings)

    line_count = len(text.splitlines())
    if line_count > 500:
        errors.append(f"SKILL.md has {line_count} lines; keep it at or below 500.")
    estimated_tokens = max(1, len(text) // 4)
    if estimated_tokens > 5000:
        warnings.append(
            f"SKILL.md is approximately {estimated_tokens} tokens; consider more disclosure."
        )

    link_errors, link_warnings = inspect_links(skill_dir, text)
    errors.extend(link_errors)
    warnings.extend(link_warnings)

    recommended_sections = [
        "inputs and preconditions",
        "hard rules",
        "workflow",
        "decision gates",
        "verification",
        "output contract",
        "failure and escalation",
    ]
    body_lower = body.lower()
    missing = [section for section in recommended_sections if f"## {section}" not in body_lower]
    if missing:
        warnings.append("Recommended workflow sections not found: " + ", ".join(missing))

    name = metadata.get("name") if isinstance(metadata.get("name"), str) else ""
    openai_errors, openai_warnings, openai_backend = inspect_openai_yaml(skill_dir, name)
    errors.extend(openai_errors)
    warnings.extend(openai_warnings)
    if openai_backend:
        yaml_backends.add(openai_backend)

    manifest_errors, manifest_warnings = inspect_manifest(skill_dir)
    errors.extend(manifest_errors)
    warnings.extend(manifest_warnings)
    errors.extend(inspect_python(skill_dir))
    errors.extend(inspect_secrets(skill_dir))

    unnecessary = {
        "README.md",
        "INSTALLATION_GUIDE.md",
        "QUICK_REFERENCE.md",
        "CHANGELOG.md",
    }
    present = sorted(path.name for path in skill_dir.iterdir() if path.name in unnecessary)
    if present:
        warnings.append("Potentially unnecessary packaged documentation: " + ", ".join(present))

    return ValidationReport(
        skill=str(skill_dir),
        yaml_backend=",".join(sorted(yaml_backends)) or "none",
        errors=errors,
        warnings=warnings,
    )


def print_human(report: ValidationReport) -> None:
    print(f"Skill: {report.skill}")
    print(f"YAML backend: {report.yaml_backend}")
    print(f"Errors: {len(report.errors)}")
    for error in report.errors:
        print(f"  ERROR: {error}")
    print(f"Warnings: {len(report.warnings)}")
    for warning in report.warnings:
        print(f"  WARNING: {warning}")
    print("Result: PASS" if report.passed else "Result: FAIL")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("skill_dir", type=Path, help="Path to the skill directory")
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON")
    args = parser.parse_args()

    report = validate(args.skill_dir.expanduser().resolve())
    if args.json:
        print(json.dumps({**asdict(report), "passed": report.passed}, indent=2))
    else:
        print_human(report)
    return 0 if report.passed else 1


if __name__ == "__main__":
    sys.exit(main())
