#!/usr/bin/env python3
"""Generate or check deterministic agents/openai.yaml metadata."""

from __future__ import annotations

import argparse
import errno
import json
import os
import stat
import sys
import tempfile
from pathlib import Path
from typing import Any

from skill_utils import (
    YamlSubsetError,
    load_skill_metadata,
    load_yaml,
    title_from_name,
    yaml_quote,
)


def scalar_yaml(value: Any) -> str:
    if isinstance(value, str):
        return yaml_quote(value)
    if isinstance(value, bool):
        return "true" if value else "false"
    if value is None:
        return "null"
    if isinstance(value, (int, float)):
        return str(value)
    raise TypeError(f"Unsupported YAML scalar: {type(value).__name__}")


def render_yaml(data: dict[str, Any], indent: int = 0) -> str:
    lines: list[str] = []
    prefix = " " * indent
    for key, value in data.items():
        if isinstance(value, dict):
            lines.append(f"{prefix}{key}:")
            lines.extend(render_yaml(value, indent + 2).splitlines())
        elif isinstance(value, list):
            lines.append(f"{prefix}{key}:")
            for item in value:
                if isinstance(item, dict):
                    item_entries = list(item.items())
                    if not item_entries:
                        lines.append(f"{prefix}  - {{}}")
                        continue
                    first_key, first_value = item_entries[0]
                    if isinstance(first_value, (dict, list)):
                        lines.append(f"{prefix}  - {first_key}:")
                        nested = (
                            render_yaml(first_value, indent + 6)
                            if isinstance(first_value, dict)
                            else ""
                        )
                        if nested:
                            lines.extend(nested.splitlines())
                    else:
                        lines.append(
                            f"{prefix}  - {first_key}: {scalar_yaml(first_value)}"
                        )
                    for child_key, child_value in item_entries[1:]:
                        if isinstance(child_value, dict):
                            lines.append(f"{prefix}    {child_key}:")
                            lines.extend(
                                render_yaml(child_value, indent + 6).splitlines()
                            )
                        elif isinstance(child_value, list):
                            raise TypeError("Nested lists in list items are not supported.")
                        else:
                            lines.append(
                                f"{prefix}    {child_key}: {scalar_yaml(child_value)}"
                            )
                else:
                    lines.append(f"{prefix}  - {scalar_yaml(item)}")
        else:
            lines.append(f"{prefix}{key}: {scalar_yaml(value)}")
    return "\n".join(lines) + "\n"


def existing_document(path: Path) -> dict[str, Any]:
    try:
        info = path.lstat()
    except FileNotFoundError:
        return {}
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise ValueError(f"Expected a regular file: {path}")
    try:
        loaded = load_yaml(path.read_text(encoding="utf-8"))
    except YamlSubsetError:
        return {}
    return loaded.data if isinstance(loaded.data, dict) else {}


def normalized_short_description(value: str, display_name: str) -> str:
    value = " ".join(value.split()).strip()
    if len(value) > 64:
        value = value[:61].rstrip() + "..."
    if len(value) < 25:
        value = f"Reusable workflow for {display_name}"
    if len(value) > 64:
        value = value[:64].rstrip()
    return value


def build_document(
    skill_dir: Path,
    *,
    display_name: str | None = None,
    short_description: str | None = None,
    default_prompt: str | None = None,
    allow_implicit: bool | None = None,
    mcp_dependencies: list[tuple[str, str, str]] | None = None,
) -> dict[str, Any]:
    metadata, _, _ = load_skill_metadata(skill_dir / "SKILL.md")
    name = metadata.get("name")
    description = metadata.get("description")
    if not isinstance(name, str) or not name:
        raise ValueError("SKILL.md must contain a string 'name'.")
    if not isinstance(description, str) or not description:
        raise ValueError("SKILL.md must contain a string 'description'.")

    output_path = skill_dir / "agents" / "openai.yaml"
    existing = existing_document(output_path)
    existing_interface = (
        existing.get("interface") if isinstance(existing.get("interface"), dict) else {}
    )

    chosen_display = (
        display_name
        or existing_interface.get("display_name")
        or title_from_name(name)
    )
    chosen_short = normalized_short_description(
        short_description
        or existing_interface.get("short_description")
        or description.split(".")[0],
        chosen_display,
    )
    chosen_prompt = (
        default_prompt
        or existing_interface.get("default_prompt")
        or f"Use ${name} to complete this workflow reliably."
    )
    if f"${name}" not in chosen_prompt:
        raise ValueError(f"default_prompt must mention '${name}'.")

    interface: dict[str, Any] = {
        "display_name": chosen_display,
        "short_description": chosen_short,
    }
    for field in ("icon_small", "icon_large", "brand_color"):
        if field in existing_interface:
            interface[field] = existing_interface[field]
    interface["default_prompt"] = chosen_prompt

    document: dict[str, Any] = {"interface": interface}
    if mcp_dependencies:
        document["dependencies"] = {
            "tools": [
                {
                    "type": "mcp",
                    "value": value,
                    "description": dependency_description,
                    "transport": "streamable_http",
                    "url": url,
                }
                for value, url, dependency_description in mcp_dependencies
            ]
        }
    elif isinstance(existing.get("dependencies"), dict):
        document["dependencies"] = existing["dependencies"]

    existing_policy = existing.get("policy")
    if allow_implicit is None and isinstance(existing_policy, dict):
        allow_implicit = existing_policy.get("allow_implicit_invocation")
    document["policy"] = {
        "allow_implicit_invocation": True if allow_implicit is None else allow_implicit
    }
    return document


def validate_skill_directory(skill_dir: Path) -> None:
    info = skill_dir.lstat()
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        raise ValueError(f"Skill directory must be a non-symlink directory: {skill_dir}")


def validate_child_within_root(root: Path, child: Path) -> None:
    """Reject a descendant whose resolved path escapes the selected skill."""
    resolved_root = root.resolve()
    try:
        child.resolve().relative_to(resolved_root)
    except ValueError as exc:
        raise ValueError(f"Path escapes skill directory: {child}") from exc


def validate_agents_directory(skill_dir: Path) -> Path | None:
    """Validate the complete existing output parent chain without following it."""
    validate_skill_directory(skill_dir)
    agents = skill_dir / "agents"
    try:
        info = agents.lstat()
    except FileNotFoundError:
        return None
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        raise ValueError(f"agents must be a non-symlink directory: {agents}")
    validate_child_within_root(skill_dir, agents)
    return agents


def read_output(skill_dir: Path) -> str:
    agents = validate_agents_directory(skill_dir)
    if agents is None:
        return ""
    output = agents / "openai.yaml"
    try:
        info = output.lstat()
    except FileNotFoundError:
        return ""
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise ValueError(f"openai.yaml must be a non-symlink regular file: {output}")
    validate_child_within_root(skill_dir, output)
    return output.read_text(encoding="utf-8")


def write_output(skill_dir: Path, rendered: str) -> None:
    """Atomically publish in agents without following a descendant symlink."""
    validate_skill_directory(skill_dir)
    anchored = (
        hasattr(os, "O_NOFOLLOW")
        and hasattr(os, "O_DIRECTORY")
        and os.open in os.supports_dir_fd
        and os.mkdir in os.supports_dir_fd
        and os.lstat in os.supports_dir_fd
        and os.replace in os.supports_dir_fd
    )
    if anchored:
        skill_fd = os.open(
            skill_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
        )
        try:
            try:
                os.mkdir("agents", 0o755, dir_fd=skill_fd)
            except FileExistsError:
                pass
            agents_fd = os.open(
                "agents", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=skill_fd
            )
        finally:
            os.close(skill_fd)
        try:
            try:
                output_info = os.lstat("openai.yaml", dir_fd=agents_fd)
            except FileNotFoundError:
                output_info = None
            if output_info is not None and (
                stat.S_ISLNK(output_info.st_mode)
                or not stat.S_ISREG(output_info.st_mode)
            ):
                raise ValueError("openai.yaml must be a non-symlink regular file")
            for index in range(128):
                temporary = f".openai.yaml.{index}.tmp"
                try:
                    fd = os.open(
                        temporary,
                        os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                        0o644,
                        dir_fd=agents_fd,
                    )
                except FileExistsError:
                    continue
                try:
                    with os.fdopen(fd, "w", encoding="utf-8") as output_file:
                        output_file.write(rendered)
                        output_file.flush()
                        os.fsync(output_file.fileno())
                    try:
                        output_info = os.lstat("openai.yaml", dir_fd=agents_fd)
                    except FileNotFoundError:
                        output_info = None
                    if output_info is not None and (
                        stat.S_ISLNK(output_info.st_mode)
                        or not stat.S_ISREG(output_info.st_mode)
                    ):
                        raise ValueError("openai.yaml must be a non-symlink regular file")
                    os.replace(
                        temporary,
                        "openai.yaml",
                        src_dir_fd=agents_fd,
                        dst_dir_fd=agents_fd,
                    )
                except Exception:
                    try:
                        os.unlink(temporary, dir_fd=agents_fd)
                    except FileNotFoundError:
                        pass
                    raise
                return
            raise ValueError("Could not allocate an exclusive metadata temporary file.")
        finally:
            os.close(agents_fd)

    # Windows and other platforms without descriptor-relative APIs use repeated
    # lstat and resolved-within-root checks rather than unavailable POSIX flags.
    agents = validate_agents_directory(skill_dir)
    if agents is None:
        skill_dir.mkdir(exist_ok=True)
        agents = skill_dir / "agents"
        agents.mkdir(mode=0o755)
    agents = validate_agents_directory(skill_dir)
    assert agents is not None
    output = agents / "openai.yaml"
    try:
        output_info = output.lstat()
    except FileNotFoundError:
        output_info = None
    if output_info is not None and (output.is_symlink() or not stat.S_ISREG(output_info.st_mode)):
        raise ValueError("openai.yaml must be a non-symlink regular file")
    validate_child_within_root(skill_dir, agents)
    with tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", dir=agents, prefix=".openai.yaml.", suffix=".tmp", delete=False
    ) as output_file:
        temporary = Path(output_file.name)
        output_file.write(rendered)
        output_file.flush()
        os.fsync(output_file.fileno())
    try:
        validate_agents_directory(skill_dir)
        try:
            output_info = output.lstat()
        except FileNotFoundError:
            output_info = None
        if output_info is not None and (output.is_symlink() or not stat.S_ISREG(output_info.st_mode)):
            raise ValueError("openai.yaml must be a non-symlink regular file")
        validate_child_within_root(skill_dir, output)
        os.replace(temporary, output)
    except Exception:
        temporary.unlink(missing_ok=True)
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("skill_dir", type=Path)
    parser.add_argument("--display-name")
    parser.add_argument("--short-description")
    parser.add_argument("--default-prompt")
    parser.add_argument(
        "--allow-implicit",
        action=argparse.BooleanOptionalAction,
        default=None,
        help="Enable or disable implicit invocation.",
    )
    parser.add_argument(
        "--mcp",
        action="append",
        nargs=3,
        metavar=("VALUE", "URL", "DESCRIPTION"),
        help="Declare an MCP dependency; repeat for multiple dependencies.",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Check whether the existing file matches generated metadata.",
    )
    parser.add_argument("--json", action="store_true", help="Print a JSON summary.")
    args = parser.parse_args()

    skill_dir = Path(os.path.abspath(args.skill_dir.expanduser()))
    try:
        validate_skill_directory(skill_dir)
        validate_agents_directory(skill_dir)
        document = build_document(
            skill_dir,
            display_name=args.display_name,
            short_description=args.short_description,
            default_prompt=args.default_prompt,
            allow_implicit=args.allow_implicit,
            mcp_dependencies=[tuple(item) for item in args.mcp] if args.mcp else None,
        )
    except (OSError, ValueError, YamlSubsetError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    rendered = render_yaml(document)
    output_path = skill_dir / "agents" / "openai.yaml"
    try:
        current = read_output(skill_dir)
    except (OSError, ValueError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    if args.check:
        matched = current == rendered
        payload = {"path": str(output_path), "matched": matched}
        print(json.dumps(payload, indent=2) if args.json else (
            "agents/openai.yaml is current." if matched else "agents/openai.yaml is stale."
        ))
        return 0 if matched else 1

    try:
        write_output(skill_dir, rendered)
    except (OSError, ValueError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    payload = {"path": str(output_path), "written": True}
    print(json.dumps(payload, indent=2) if args.json else f"Wrote {output_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
