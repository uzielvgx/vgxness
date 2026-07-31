#!/usr/bin/env python3
"""Validate, execute through an adapter, or grade Agent Skill evaluation cases."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


REQUIRED_CASE_FIELDS = {
    "id",
    "category",
    "prompt",
    "expected_activation",
    "required_behaviors",
    "forbidden_behaviors",
    "risk",
}


@dataclass
class CaseGrade:
    id: str
    passed: bool
    failures: list[str]


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"Could not load {path}: {exc}") from exc


def validate_suite(data: Any) -> tuple[list[dict[str, Any]], list[str]]:
    errors: list[str] = []
    if not isinstance(data, dict):
        return [], ["Evaluation suite must be a JSON object."]
    cases = data.get("cases")
    if not isinstance(cases, list) or not cases:
        return [], ["Evaluation suite must contain a non-empty 'cases' list."]

    seen: set[str] = set()
    valid_cases: list[dict[str, Any]] = []
    for index, case in enumerate(cases):
        prefix = f"cases[{index}]"
        if not isinstance(case, dict):
            errors.append(f"{prefix} must be an object.")
            continue
        missing = sorted(REQUIRED_CASE_FIELDS - set(case))
        if missing:
            errors.append(f"{prefix} is missing: {', '.join(missing)}")
            continue
        case_id = case["id"]
        if not isinstance(case_id, str) or not case_id:
            errors.append(f"{prefix}.id must be a non-empty string.")
        elif case_id in seen:
            errors.append(f"Duplicate case id: {case_id}")
        else:
            seen.add(case_id)
        if not isinstance(case["prompt"], str) or not case["prompt"].strip():
            errors.append(f"{prefix}.prompt must be a non-empty string.")
        if not isinstance(case["expected_activation"], bool):
            errors.append(f"{prefix}.expected_activation must be boolean.")
        for field in ("required_behaviors", "forbidden_behaviors"):
            value = case[field]
            if not isinstance(value, list) or any(
                not isinstance(item, str) for item in value
            ):
                errors.append(f"{prefix}.{field} must be a list of strings.")
        valid_cases.append(case)
    return valid_cases, errors


def run_adapter(
    cases: list[dict[str, Any]], command: list[str]
) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for case in cases:
        completed = subprocess.run(
            command,
            input=json.dumps(case),
            text=True,
            capture_output=True,
            check=False,
            timeout=300,
        )
        if completed.returncode != 0:
            results.append(
                {
                    "id": case["id"],
                    "adapter_error": completed.stderr.strip()
                    or f"Adapter exited {completed.returncode}",
                }
            )
            continue
        try:
            result = json.loads(completed.stdout)
        except json.JSONDecodeError as exc:
            results.append(
                {
                    "id": case["id"],
                    "adapter_error": f"Adapter returned invalid JSON: {exc}",
                }
            )
            continue
        if not isinstance(result, dict):
            result = {"adapter_error": "Adapter result must be a JSON object."}
        result.setdefault("id", case["id"])
        results.append(result)
    return results


def grade_case(case: dict[str, Any], result: dict[str, Any]) -> CaseGrade:
    failures: list[str] = []
    if result.get("adapter_error"):
        failures.append(str(result["adapter_error"]))
        return CaseGrade(case["id"], False, failures)

    activated = result.get("activated")
    if activated is not case["expected_activation"]:
        failures.append(
            f"Activation expected {case['expected_activation']}, observed {activated}."
        )

    satisfied = result.get("satisfied_behaviors", [])
    violations = result.get("violations", [])
    if not isinstance(satisfied, list):
        failures.append("satisfied_behaviors must be a list.")
        satisfied = []
    if not isinstance(violations, list):
        failures.append("violations must be a list.")
        violations = []

    for behavior in case["required_behaviors"]:
        if behavior not in satisfied:
            failures.append(f"Required behavior not satisfied: {behavior}")
    for behavior in case["forbidden_behaviors"]:
        if behavior in violations:
            failures.append(f"Forbidden behavior observed: {behavior}")
    if result.get("validator_passed") is False:
        failures.append("Artifact validator failed.")

    return CaseGrade(case["id"], not failures, failures)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("cases", type=Path)
    parser.add_argument("--results", type=Path, help="Existing results JSON to grade")
    parser.add_argument("--output", type=Path, help="Write execution or grading output")
    parser.add_argument(
        "--runner",
        nargs=argparse.REMAINDER,
        help="Adapter command. Receives one case JSON on stdin and returns result JSON.",
    )
    args = parser.parse_args()

    try:
        suite = load_json(args.cases.expanduser().resolve())
        cases, suite_errors = validate_suite(suite)
    except ValueError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    if suite_errors:
        for error in suite_errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1

    if args.results is None and args.runner is None:
        print(json.dumps({"valid": True, "case_count": len(cases)}, indent=2))
        return 0
    if args.results is not None and args.runner is not None:
        print("ERROR: Use either --results or --runner, not both.", file=sys.stderr)
        return 1

    if args.runner is not None:
        if not args.runner:
            print("ERROR: --runner requires a command.", file=sys.stderr)
            return 1
        results = run_adapter(cases, args.runner)
    else:
        try:
            raw_results = load_json(args.results.expanduser().resolve())
        except ValueError as exc:
            print(f"ERROR: {exc}", file=sys.stderr)
            return 1
        if not isinstance(raw_results, dict) or not isinstance(
            raw_results.get("results"), list
        ):
            print("ERROR: Results file must contain a 'results' list.", file=sys.stderr)
            return 1
        results = raw_results["results"]

    result_by_id = {
        result.get("id"): result for result in results if isinstance(result, dict)
    }
    grades: list[CaseGrade] = []
    for case in cases:
        result = result_by_id.get(case["id"])
        if result is None:
            grades.append(CaseGrade(case["id"], False, ["Missing result."]))
        else:
            grades.append(grade_case(case, result))

    payload = {
        "passed": all(grade.passed for grade in grades),
        "summary": {
            "total": len(grades),
            "passed": sum(grade.passed for grade in grades),
            "failed": sum(not grade.passed for grade in grades),
        },
        "grades": [
            {"id": grade.id, "passed": grade.passed, "failures": grade.failures}
            for grade in grades
        ],
        "results": results,
    }
    rendered = json.dumps(payload, indent=2) + "\n"
    if args.output:
        args.output.expanduser().resolve().write_text(rendered, encoding="utf-8")
    print(rendered, end="")
    return 0 if payload["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
