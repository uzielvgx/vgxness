#!/usr/bin/env python3
"""Opt-in local transport runner for development-only Codex evaluations.

It records evidence; it does not grade model behavior.  `self-test` and help
never execute a target CLI.  `run` must be selected explicitly.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import pathlib
import secrets
import signal
import subprocess
import sys
import time
from typing import Any

VERSION = "agent-eval-runner/1"
ROOT = pathlib.Path(__file__).resolve().parent


class RunnerError(RuntimeError):
    pass


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def write_json(path: pathlib.Path, value: Any) -> None:
    path.write_text(json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n", encoding="utf-8", newline="\n")


def safe_child(root: pathlib.Path, relative: str) -> pathlib.Path:
    candidate = root / relative
    if pathlib.PurePosixPath(relative).is_absolute() or pathlib.PureWindowsPath(relative).is_absolute() or ".." in pathlib.PurePosixPath(relative).parts:
        raise RunnerError("unsafe relative path: " + relative)
    resolved_root = root.resolve()
    resolved = candidate.resolve(strict=False)
    if resolved_root != resolved and resolved_root not in resolved.parents:
        raise RunnerError("path escapes fixture: " + relative)
    return candidate


def reject_symlink_ancestors(path: pathlib.Path) -> None:
    """Reject an existing link anywhere before creating a target below it."""
    current = path
    while True:
        if current.exists() and current.is_symlink():
            raise RunnerError("symlink ancestor: " + str(current))
        if current.parent == current:
            return
        current = current.parent


def file_manifest(directory: pathlib.Path) -> list[dict[str, Any]]:
    rows = []
    for path in sorted(directory.rglob("*"), key=lambda p: p.relative_to(directory).as_posix()):
        if path.is_symlink():
            raise RunnerError("symlink in fixture: " + str(path))
        if path.is_file():
            raw = path.read_bytes()
            rows.append({"path": path.relative_to(directory).as_posix(), "bytes": len(raw), "sha256": sha256(raw)})
    return rows


def write_fixture(directory: pathlib.Path, files: dict[str, str], agents: bytes) -> None:
    if directory.exists() or directory.is_symlink():
        raise RunnerError("refusing existing fixture: " + str(directory))
    reject_symlink_ancestors(directory.parent)
    directory.mkdir(parents=True)
    safe_child(directory, "AGENTS.md").write_bytes(agents)
    for relative, content in files.items():
        target = safe_child(directory, relative)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8", newline="")


def prepare_output(output: pathlib.Path) -> None:
    if output.exists() or output.is_symlink():
        raise RunnerError("refusing existing output path: " + str(output))
    reject_symlink_ancestors(output.parent)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.mkdir()


def kill_owned_tree(process: subprocess.Popen[bytes]) -> str | None:
    if os.name == "nt":
        try:
            result = subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False, timeout=5, creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        except subprocess.TimeoutExpired:
            return "taskkill timed out after 5 seconds"
        if result.returncode:
            return "taskkill exited " + str(result.returncode) + ": " + result.stderr.decode("utf-8", "replace").strip()
    else:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    return None


def run_process(argv: list[str], stdin: bytes, stdout_path: pathlib.Path, stderr_path: pathlib.Path, timeout: float) -> dict[str, Any]:
    flags = getattr(subprocess, "CREATE_NO_WINDOW", 0) if os.name == "nt" else 0
    kwargs: dict[str, Any] = {"stdin": subprocess.PIPE, "stdout": None, "stderr": None, "creationflags": flags}
    if os.name != "nt":
        kwargs["start_new_session"] = True
    started = time.time()
    try:
        with stdout_path.open("wb") as stdout, stderr_path.open("wb") as stderr:
            kwargs["stdout"], kwargs["stderr"] = stdout, stderr
            process = subprocess.Popen(argv, **kwargs)
            timed_out = False
            try:
                process.communicate(stdin, timeout=timeout)
            except subprocess.TimeoutExpired:
                timed_out = True
                cleanup_error = kill_owned_tree(process)
                try:
                    process.communicate(timeout=5)
                except subprocess.TimeoutExpired:
                    return {"exitCode": None, "timedOut": True, "completed": False, "reaped": False, "cleanupError": cleanup_error, "durationMs": round((time.time() - started) * 1000), "stdoutBytes": stdout_path.stat().st_size, "stderrBytes": stderr_path.stat().st_size}
    except OSError as exc:
        return {"spawnError": str(exc), "exitCode": None, "timedOut": False, "completed": False, "durationMs": round((time.time() - started) * 1000), "stdoutBytes": stdout_path.stat().st_size if stdout_path.exists() else 0, "stderrBytes": stderr_path.stat().st_size if stderr_path.exists() else 0}
    return {"exitCode": process.returncode, "timedOut": timed_out, "completed": True, "reaped": True, "cleanupError": cleanup_error if timed_out else None, "durationMs": round((time.time() - started) * 1000), "stdoutBytes": stdout_path.stat().st_size, "stderrBytes": stderr_path.stat().st_size}


def parse_ndjson(path: pathlib.Path) -> tuple[list[dict[str, Any]], str | None]:
    try:
        lines = path.read_bytes().splitlines()
        events = [json.loads(line.decode("utf-8")) for line in lines if line.strip()]
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        return [], "malformed-ndjson: " + str(exc)
    if not all(isinstance(event, dict) for event in events):
        return [], "malformed-ndjson: non-object event"
    return events, None


def trace_completed(events: list[dict[str, Any]]) -> bool:
    """Codex JSON protocol: an error event never becomes success later."""
    if any(event.get("type") in {"turn.failed", "error", "turn.error"} for event in events):
        return False
    return any(event.get("type") == "turn.completed" for event in events)


def preflight_read(events: list[dict[str, Any]], token: str) -> bool:
    read_index = None
    for index, event in enumerate(events):
        if event.get("type") == "turn.completed":
            return read_index is not None and read_index < index
        item = event.get("item")
        if event.get("type") != "item.completed" or not isinstance(item, dict):
            continue
        if item.get("type") == "command_execution" and item.get("status") == "completed" and item.get("exit_code") == 0:
            if "preflight.txt" in str(item.get("command", "")) and token in str(item.get("aggregated_output", "")):
                read_index = index
    return False


def load_case_spec() -> tuple[bytes, dict[str, Any]]:
    spec = ROOT / "case-spec.json"
    if spec.is_symlink() or not spec.is_file():
        raise RunnerError("case spec must be a non-symlink regular file")
    raw = spec.read_bytes()
    try:
        decoded = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise RunnerError("invalid case spec: " + str(exc)) from exc
    if not isinstance(decoded.get("cases"), list):
        raise RunnerError("case spec has no cases list")
    return raw, decoded


def load_cases() -> list[dict[str, Any]]:
    return load_case_spec()[1]["cases"]


CASE_FIELDS = {"id", "suite", "domain", "provider", "execution", "readiness", "prerequisites", "requiredEvidence", "fixtureFiles", "prompt", "assertion"}


def validate_catalogue(spec: dict[str, Any]) -> list[dict[str, Any]]:
    cases = spec.get("cases")
    if not isinstance(cases, list) or not cases:
        raise RunnerError("case spec has no cases list")
    ids: set[str] = set()
    for case in cases:
        if not isinstance(case, dict) or CASE_FIELDS - case.keys():
            raise RunnerError("case is missing catalogue fields")
        if not isinstance(case["id"], str) or case["id"] in ids:
            raise RunnerError("case ids must be unique strings")
        ids.add(case["id"])
        if case["execution"] not in {"runnable", "decision-only", "integration-only"}:
            raise RunnerError("unknown execution mode for " + case["id"])
        if not isinstance(case["prerequisites"], list) or not isinstance(case["requiredEvidence"], list):
            raise RunnerError("case prerequisites and evidence must be lists")
    return cases


def select_cases(spec: dict[str, Any], case_ids: list[str], suites: list[str]) -> list[dict[str, Any]]:
    cases = validate_catalogue(spec)
    by_id = {case["id"]: case for case in cases}
    requested = set(case_ids)
    unknown = requested - by_id.keys()
    if unknown:
        raise RunnerError("unknown case selection: " + ", ".join(sorted(unknown)))
    unknown_suites = set(suites) - {case["suite"] for case in cases}
    if unknown_suites:
        raise RunnerError("unknown suite selection: " + ", ".join(sorted(unknown_suites)))
    selected = [case for case in cases if case["id"] in requested or case["suite"] in suites]
    if not requested and not suites:
        selected = [case for case in cases if case["suite"] == "smoke"]
    if not selected:
        raise RunnerError("selection matched no cases")
    return selected


def expand_attempts(cases: list[dict[str, Any]], repeat: int) -> list[dict[str, Any]]:
    if not 1 <= repeat <= 10:
        raise RunnerError("--repeat must be between 1 and 10")
    return [{"case": case, "trial": trial, "attemptId": f"{case['id']}--attempt-{trial}", "fixtureRelative": f"fixtures/{case['id']}--attempt-{trial}", "traceRelative": f"traces/{case['id']}--attempt-{trial}"} for trial in range(1, repeat + 1) for case in cases]


def require_runnable_cases(cases: list[dict[str, Any]]) -> None:
    for case in cases:
        if case["provider"] != "codex":
            raise RunnerError(f"{case['id']} requires provider {case['provider']}; this runner has no {case['provider']} adapter. Prerequisites: " + "; ".join(case["prerequisites"]))
        if case["execution"] != "runnable" or case["readiness"] != "ready":
            raise RunnerError(f"{case['id']} is {case['execution']} and cannot run locally. Prerequisites: " + "; ".join(case["prerequisites"]))


def catalogue_view(spec: dict[str, Any], cases: list[dict[str, Any]]) -> dict[str, Any]:
    validate_catalogue(spec)
    return {"schema": "agent-evaluation-catalogue/v1", "partition": spec.get("partition"), "cases": [{key: case[key] for key in ("id", "suite", "domain", "provider", "execution", "readiness", "prerequisites", "requiredEvidence", "assertion")} for case in cases]}


def build_argv(args: argparse.Namespace, fixture: pathlib.Path) -> list[str]:
    """The only argv constructor; manifest and process receive these exact bytes."""
    argv = [args.cli, *args.cli_prefix, "exec", "--json", "--ephemeral", "--ignore-user-config", "--sandbox", "read-only"]
    if os.name == "nt":
        argv.extend(["-c", 'windows.sandbox="elevated"'])
    argv.extend(["--skip-git-repo-check", "-m", args.model, "-c", 'approval_policy="never"', "-C", str(fixture), "-"])
    return argv


def cli_version(args: argparse.Namespace) -> tuple[bool, str]:
    stdout, stderr = pathlib.Path(args.output) / "cli-version.stdout.raw", pathlib.Path(args.output) / "cli-version.stderr.raw"
    result = run_process([args.cli, *args.cli_prefix, "--version"], b"", stdout, stderr, min(args.timeout_seconds, 10))
    actual = stdout.read_bytes().decode("utf-8", "replace").strip() if stdout.exists() else ""
    return result["completed"] and not result["timedOut"] and result["exitCode"] == 0 and actual == args.cli_version, actual


def execute(args: argparse.Namespace) -> int:
    spec_bytes, spec = load_case_spec()
    cases = select_cases(spec, args.case, args.suite)
    require_runnable_cases(cases)
    attempts = expand_attempts(cases, args.repeat)
    output = pathlib.Path(args.output).expanduser()
    prepare_output(output)
    agents = pathlib.Path(args.agents).expanduser()
    if agents.is_symlink() or not agents.is_file():
        raise RunnerError("AGENTS file must be a non-symlink regular file")
    agents_bytes = agents.read_bytes()
    cli_path = pathlib.Path(args.cli)
    if cli_path.is_symlink() or not cli_path.is_file():
        raise RunnerError("CLI must be a non-symlink regular file")
    runner_bytes = pathlib.Path(__file__).read_bytes()
    (output / "case-spec.json").write_bytes(spec_bytes)
    (output / "AGENTS.md").write_bytes(agents_bytes)
    (output / "runner.py").write_bytes(runner_bytes)
    manifest: dict[str, Any] = {"schema": "agent-evaluation-execution-manifest/v1", "runnerVersion": VERSION, "status": "running", "target": {"cli": args.cli, "cliSha256": sha256(cli_path.read_bytes()), "cliPrefix": args.cli_prefix, "expectedVersion": args.cli_version, "model": args.model, "agents": str(agents), "agentsSha256": sha256(agents_bytes), "caseSpecSha256": sha256(spec_bytes), "runnerSha256": sha256(runner_bytes)}, "platform": sys.platform, "selection": {"caseIds": args.case, "suites": args.suite, "repeat": args.repeat, "attempts": [{key: attempt[key] for key in ("attemptId", "trial", "fixtureRelative", "traceRelative")} for attempt in attempts]}, "runs": [], "notes": ["Development cases only. Execution completion is not behavioral pass; review rubrics separately."]}
    version_ok, observed = cli_version(args)
    manifest["target"]["observedVersion"] = observed
    if not version_ok:
        manifest.update(status="failed", failure="cli-version-preflight")
        write_json(output / "execution-manifest.json", manifest)
        return 1
    token = "eval-preflight-" + secrets.token_hex(16)
    preflight = output / "fixtures" / "preflight"
    trace = output / "traces" / "preflight"; trace.mkdir(parents=True)
    write_fixture(preflight, {"preflight.txt": token + "\n"}, agents_bytes)
    argv = build_argv(args, preflight)
    (trace / "prompt.utf8.raw").write_bytes(b"Read preflight.txt and report its content.\n")
    result = run_process(argv, (trace / "prompt.utf8.raw").read_bytes(), trace / "stdout.raw", trace / "stderr.raw", args.timeout_seconds)
    events, parse_error = parse_ndjson(trace / "stdout.raw")
    passed = result["completed"] and not result["timedOut"] and result["exitCode"] == 0 and parse_error is None and trace_completed(events) and preflight_read(events, token)
    manifest["preflight"] = {"status": "passed" if passed else "failed", "argv": argv, "result": result, "parseError": parse_error, "fixtureManifest": file_manifest(preflight), "requiredEvidence": "successful file-read event containing fresh preflight token"}
    write_json(output / "execution-manifest.json", manifest)
    if not passed:
        manifest.update(status="failed", failure="preflight")
        write_json(output / "execution-manifest.json", manifest)
        return 1
    for attempt in attempts:
        case, fixture, trace = attempt["case"], output / attempt["fixtureRelative"], output / attempt["traceRelative"]
        trace.mkdir(parents=True)
        write_fixture(fixture, case["fixtureFiles"], agents_bytes)
        before = file_manifest(fixture)
        prompt = case["prompt"].encode("utf-8")
        (trace / "prompt.utf8.raw").write_bytes(prompt)
        argv = build_argv(args, fixture)
        result = run_process(argv, prompt, trace / "stdout.raw", trace / "stderr.raw", args.timeout_seconds)
        events, parse_error = parse_ndjson(trace / "stdout.raw")
        after = file_manifest(fixture)
        run = {"id": case["id"], "attemptId": attempt["attemptId"], "trial": attempt["trial"], "fixtureRelative": attempt["fixtureRelative"], "traceRelative": attempt["traceRelative"], "argv": argv, "result": result, "parseError": parse_error, "completion": trace_completed(events), "beforeManifest": before, "afterManifest": after, "beforeManifestSha256": sha256(json.dumps(before, sort_keys=True).encode()), "afterManifestSha256": sha256(json.dumps(after, sort_keys=True).encode()), "rubric": case["assertion"], "behavioralStatus": "ungraded"}
        manifest["runs"].append(run); write_json(output / "execution-manifest.json", manifest)
        if not result["completed"] or result["timedOut"] or result["exitCode"] != 0 or parse_error or not run["completion"]:
            manifest.update(status="failed", failure="case-transport:" + attempt["attemptId"]); write_json(output / "execution-manifest.json", manifest); return 1
    manifest["status"] = "completed-ungraded"; write_json(output / "execution-manifest.json", manifest)
    return 0


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Local, opt-in development evaluator; run invokes the supplied CLI.")
    parser.add_argument("--version", action="version", version=VERSION)
    subs = parser.add_subparsers(dest="command", required=True)
    subs.add_parser("self-test", help="run offline runner tests; never invokes a model")
    listed = subs.add_parser("list", help="show the offline catalogue")
    listed.add_argument("--case", action="append", default=[])
    listed.add_argument("--suite", action="append", default=[])
    plan = subs.add_parser("plan", help="show the selected offline execution plan")
    plan.add_argument("--case", action="append", default=[])
    plan.add_argument("--suite", action="append", default=[])
    plan.add_argument("--repeat", type=int, default=1)
    run = subs.add_parser("run", help="explicitly invoke a CLI for development cases")
    run.add_argument("--cli", required=True); run.add_argument("--cli-version", required=True); run.add_argument("--model", required=True); run.add_argument("--agents", required=True); run.add_argument("--output", required=True)
    run.add_argument("--cli-prefix", action="append", default=[], help="argument placed before exec, useful for a wrapper interpreter")
    run.add_argument("--timeout-seconds", type=float, default=90)
    run.add_argument("--allow-live", action="store_true", help="required acknowledgement before the runner even probes the target CLI")
    run.add_argument("--case", action="append", default=[], help="case ID; may be repeated")
    run.add_argument("--suite", action="append", default=[], help="suite name; may be repeated")
    run.add_argument("--repeat", type=int, default=1, help="independent trials per selected case (1-10)")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.command == "self-test":
        import unittest
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromName("test_runner"))
        return int(not result.wasSuccessful())
    if args.command in {"list", "plan"}:
        try:
            spec = load_case_spec()[1]
            cases = select_cases(spec, args.case, args.suite) if args.command == "plan" or args.case or args.suite else validate_catalogue(spec)
            view = catalogue_view(spec, cases)
            if args.command == "plan":
                view["attempts"] = [{key: attempt[key] for key in ("attemptId", "trial", "fixtureRelative", "traceRelative")} for attempt in expand_attempts(cases, args.repeat)]
            print(json.dumps(view, ensure_ascii=False, sort_keys=True, indent=2))
            return 0
        except RunnerError as exc:
            print("agent-eval: " + str(exc), file=sys.stderr)
            return 2
    if not args.allow_live:
        print("agent-eval: run requires --allow-live; target CLI was not executed", file=sys.stderr)
        return 2
    try:
        return execute(args)
    except RunnerError as exc:
        print("agent-eval: " + str(exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
