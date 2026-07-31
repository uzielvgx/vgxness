#!/usr/bin/env python3
"""Create an Agent Skill scaffold atomically from the bundled template."""

from __future__ import annotations

import argparse
import errno
import json
import os
import shutil
import stat
import sys
import tempfile
from datetime import date
from pathlib import Path

from generate_openai_yaml import build_document, render_yaml
from skill_utils import title_from_name, validate_skill_name


SKILL_ROOT = Path(__file__).resolve().parent.parent
SUPPORTED_RESOURCES = {"references", "scripts", "assets"}


def parse_resources(raw: str) -> list[str]:
    resources = [item.strip() for item in raw.split(",") if item.strip()]
    unknown = sorted(set(resources) - SUPPORTED_RESOURCES)
    if unknown:
        raise ValueError("Unknown resources: " + ", ".join(unknown))
    return list(dict.fromkeys(resources))


def license_text(status: str) -> str:
    if status == "no-license-granted":
        return (
            "No license is granted for this skill.\n\n"
            "Add explicit license terms before distributing or publishing it.\n"
        )
    return f"License status: {status}\n"


def lexists(path: Path) -> bool:
    """True for every directory entry, including a dangling symlink."""
    try:
        path.lstat()
    except FileNotFoundError:
        return False
    return True


def require_safe_eval_publication_support() -> None:
    """Reject --evals-dir where stdlib cannot safely anchor its directory."""
    required = (os.open, os.mkdir, os.stat, os.unlink)
    if (
        not all(operation in os.supports_dir_fd for operation in required)
        or os.stat not in os.supports_follow_symlinks
        or not hasattr(os, "O_DIRECTORY")
        or not hasattr(os, "O_NOFOLLOW")
    ):
        raise ValueError(
            "Safe --evals-dir publication requires descriptor-relative os.open, "
            "os.mkdir, os.stat, and os.unlink; os.stat follow_symlinks support; "
            "and O_DIRECTORY and O_NOFOLLOW."
        )


def open_non_symlink_directory(
    path: str | Path, *, dir_fd: int | None, display_path: Path
) -> int:
    """Open one directory without following its final path component."""
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    try:
        if dir_fd is None:
            return os.open(path, flags)
        return os.open(path, flags, dir_fd=dir_fd)
    except OSError as exc:
        if exc.errno in (errno.ELOOP, errno.ENOTDIR):
            raise ValueError(
                f"Evaluation directory must be a non-symlink directory: {display_path}"
            ) from exc
        raise


def prepare_eval_directory(root: Path, name: str) -> tuple[Path, int]:
    """Create and anchor evals/name, returning its lexical path and held fd."""
    require_safe_eval_publication_support()
    root = root.expanduser()
    try:
        current_fd = open_non_symlink_directory(
            root, dir_fd=None, display_path=root
        )
    except FileNotFoundError as exc:
        raise ValueError(f"Evaluation root does not exist: {root}") from exc

    current_path = root
    try:
        for component in ("evals", name):
            next_path = current_path / component
            try:
                next_fd = open_non_symlink_directory(
                    component, dir_fd=current_fd, display_path=next_path
                )
            except FileNotFoundError:
                try:
                    os.mkdir(component, dir_fd=current_fd)
                except FileExistsError:
                    pass
                next_fd = open_non_symlink_directory(
                    component, dir_fd=current_fd, display_path=next_path
                )
            os.close(current_fd)
            current_fd = next_fd
            current_path = next_path
    except Exception:
        os.close(current_fd)
        raise
    return current_path / "cases.json", current_fd


def eval_target_exists(directory_fd: int) -> bool:
    """Check cases.json through the held evaluation directory only."""
    try:
        os.stat("cases.json", dir_fd=directory_fd, follow_symlinks=False)
    except FileNotFoundError:
        return False
    return True


def write_eval_cases(directory_fd: int, contents: str) -> None:
    """Exclusively create cases.json through the held evaluation directory."""
    created = False
    try:
        descriptor = os.open(
            "cases.json",
            os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
            0o644,
            dir_fd=directory_fd,
        )
        created = True
        with os.fdopen(descriptor, "w", encoding="utf-8") as eval_file:
            eval_file.write(contents)
    except Exception:
        if created:
            try:
                os.unlink("cases.json", dir_fd=directory_fd)
            except FileNotFoundError:
                pass
        raise


def publish_without_overwrite(staging: Path, target: Path) -> None:
    """Create each staged entry exclusively; SKILL.md is last."""
    entries = sorted(staging.rglob("*"), key=lambda path: path.relative_to(staging).as_posix())
    directories = [path for path in entries if path.is_dir()]
    files = [path for path in entries if path.is_file() and path.name != "SKILL.md"]
    skill = staging / "SKILL.md"
    for source in directories:
        (target / source.relative_to(staging)).mkdir()
    for source in files + [skill]:
        destination = target / source.relative_to(staging)
        with source.open("rb") as input_file, destination.open("xb") as output_file:
            shutil.copyfileobj(input_file, output_file)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("name")
    parser.add_argument("--path", type=Path, required=True, help="Parent output directory")
    parser.add_argument("--description", required=True)
    parser.add_argument("--resources", default="")
    parser.add_argument("--display-name")
    parser.add_argument("--short-description")
    parser.add_argument("--default-prompt")
    parser.add_argument("--owner", default="local-user")
    parser.add_argument("--version", default="0.1.0")
    parser.add_argument("--license-status", default="no-license-granted")
    parser.add_argument(
        "--evals-dir",
        type=Path,
        help="Optional parent directory where evals/<skill>/cases.json is created.",
    )
    args = parser.parse_args()

    errors = validate_skill_name(args.name)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    if not args.description.strip():
        print("ERROR: --description cannot be empty.", file=sys.stderr)
        return 1

    try:
        resources = parse_resources(args.resources)
    except ValueError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    output_parent = args.path.expanduser().resolve()
    output_parent.mkdir(parents=True, exist_ok=True)
    target = output_parent / args.name
    if lexists(target):
        print(f"ERROR: Target already exists: {target}", file=sys.stderr)
        return 1
    eval_path: Path | None = None
    eval_directory_fd: int | None = None
    if args.evals_dir:
        try:
            eval_path, eval_directory_fd = prepare_eval_directory(args.evals_dir, args.name)
        except (OSError, ValueError) as exc:
            print(f"ERROR: {exc}", file=sys.stderr)
            return 1
    staging: Path | None = None
    published = False
    try:
        if eval_directory_fd is not None and eval_target_exists(eval_directory_fd):
            print(f"ERROR: Evaluation target already exists: {eval_path}", file=sys.stderr)
            return 1

        staging = Path(tempfile.mkdtemp(prefix=f".{args.name}-", dir=output_parent))
        template = (SKILL_ROOT / "assets" / "SKILL.template.md").read_text(
            encoding="utf-8"
        )
        skill_text = (
            template.replace("replace-with-skill-name", args.name)
            .replace(
                "Replace with a precise activation description.",
                args.description.strip(),
            )
            .replace("Replace With Skill Title", title_from_name(args.name))
        )
        (staging / "SKILL.md").write_text(skill_text, encoding="utf-8")

        for resource in resources:
            (staging / resource).mkdir(parents=True, exist_ok=True)

        document = build_document(
            staging,
            display_name=args.display_name or title_from_name(args.name),
            short_description=args.short_description,
            default_prompt=args.default_prompt
            or f"Use ${args.name} to complete this workflow reliably.",
            allow_implicit=True,
        )
        (staging / "agents").mkdir(parents=True, exist_ok=True)
        (staging / "agents" / "openai.yaml").write_text(
            render_yaml(document), encoding="utf-8"
        )

        manifest = {
            "name": args.name,
            "version": args.version,
            "owner": args.owner,
            "provenance": "Scaffolded by skills-creator",
            "license_status": args.license_status,
            "created": date.today().isoformat(),
            "standard": "agentskills.io",
        }
        (staging / "skill-manifest.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )
        (staging / "LICENSE.txt").write_text(
            license_text(args.license_status), encoding="utf-8"
        )
        try:
            target.mkdir()
        except FileExistsError:
            print(f"ERROR: Target already exists: {target}", file=sys.stderr)
            return 1
        published = True
        publish_without_overwrite(staging, target)

        if eval_path:
            eval_template = json.loads(
                (SKILL_ROOT / "assets" / "eval-cases.template.json").read_text(
                    encoding="utf-8"
                )
            )
            eval_template["skill"] = args.name
            write_eval_cases(
                eval_directory_fd, json.dumps(eval_template, indent=2) + "\n"
            )

        print(
            json.dumps(
                {
                    "skill": str(target),
                    "resources": resources,
                    "evaluations": str(eval_path) if eval_path else None,
                },
                indent=2,
            )
        )
        return 0
    except Exception as exc:
        # Staging is unique to this invocation. Once the skill has been
        # published, it is valid user-visible output and must survive a later
        # eval failure; the error tells the caller exactly what remains.
        if staging is not None and lexists(staging):
            shutil.rmtree(staging)
        if published:
            print(
                f"ERROR: Skill claimed at {target}, but creation did not complete: {exc}",
                file=sys.stderr,
            )
        else:
            print(f"ERROR: Could not create skill: {exc}", file=sys.stderr)
        return 1
    finally:
        if eval_directory_fd is not None:
            os.close(eval_directory_fd)


if __name__ == "__main__":
    sys.exit(main())
