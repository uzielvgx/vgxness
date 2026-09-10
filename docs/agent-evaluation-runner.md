# Local agent-evaluation runner

`tools/agent_eval/runner.py` is a development-only evidence transport for a locally installed Codex CLI. It never contacts a model for help, `--version`, or `self-test`. A model can run only through the explicit `run` command and all target values are required.

Run the offline regression suite:

```powershell
python -m unittest discover -s tools/agent_eval -p 'test_*.py'
```

Inspect the whole catalogue without invoking a target, or preview a selection and its fresh trial paths:

```powershell
python tools/agent_eval/runner.py list
python tools/agent_eval/runner.py plan --suite decision --repeat 2
```

`run` defaults to the four `smoke` cases once. Select a case or suite explicitly with repeatable `--case` / `--suite`; `--repeat` is bounded to 1–10. Every trial gets a distinct fixture and trace directory such as `C5-conflicting-data--attempt-2`, and the manifest records the requested selection and every attempt identity. A planned, unavailable, or unsupported-provider case fails before the runner creates output or probes a CLI. In particular, it never sends Codex argv to OpenCode.

For an authorized local development run, choose your own CLI, exact expected version, model, manager instructions, and a new output directory outside the repository. This example is intentionally a template; replace every placeholder.

```powershell
python tools/agent_eval/runner.py run --allow-live --cli <absolute-cli-path> --cli-version <exact-version-output> --model <model-id> --agents <absolute-AGENTS-path> --output <new-absolute-output-directory>
```

`--allow-live` is required before even the CLI version probe runs. The runner refuses an existing output directory, symlinks (including ancestor paths), and fixture path escapes. It snapshots immutable AGENTS, runner, and case-spec bytes; retains raw UTF-8 prompt bytes and independent binary stdout/stderr streams, exact executed argv, target hashes, fixture manifests, and an incremental `execution-manifest.json`. Output is a local evidence bundle and should be kept outside the working tree.

Before selected development cases, the runner creates a fresh `preflight.txt` containing an unpredictable token that is absent from the prompt. It requires a Codex `item.completed` `command_execution` record with `status=completed`, `exit_code=0`, a command naming `preflight.txt`, and `aggregated_output` containing that token, followed by `turn.completed`. A `turn.failed` or error record never passes even if a later completion appears. Any spawn error, timeout, nonzero child exit, malformed NDJSON, missing completion, or failed preflight makes the outer runner nonzero and stops later work. There are no retries or model calls in CI.

The command passes `--ignore-user-config`, `--sandbox read-only`, and `approval_policy="never"`. On Windows it also passes `windows.sandbox="elevated"` while retaining read-only mode, because that is the explicit Windows sandbox selection required for sandboxed reads. It does not change user configuration, approvals, global settings, or sandbox bypasses. POSIX launches a process group; Windows uses a hidden console and kills only the owned process tree after a timeout.

The case set is development-only: a Spanish greeting, exact fixture file read, summary of untrusted embedded text, and refusal to declare `VERIFIED` without evidence. Their rubrics are stored with each run. A completed transport run is `completed-ungraded`, not a behavioral pass: inspect the retained traces and rubric separately. Do not use these development prompts as a protected holdout or certification result.

| Coverage | Cases | Execution status | What would make it conclusive |
| --- | --- | --- | --- |
| Local smoke transport | C1–C4 | Runnable Codex development fixtures | Retained target/version/trace evidence plus independent rubric review |
| Read-only decision tests | C5 conflicting data, C6 failed child evidence, C7 stale memory | Runnable Codex fixtures; they test a decision, not real delegation or memory integration | Retained traces reviewed against the stated rubric |
| OpenCode and real delegation | I1 | Pending automated adapter availability; separate manually executed integration evidence is reported in [the evaluation results](agent-evaluation-results.md) | Authorized OpenCode target, provider/version evidence, and child-task trace |
| Memory persistence/retrieval | I2 | Pending automated integration coverage | Isolated authorized memory workspace, write/retrieval receipts, and retention evidence |
| Skill activation | I3 | Pending automated integration coverage | Positive and negative routing cases plus activation traces |
| External integrations | I4 | Pending real integration | Authorized sandbox, audit trail, request/response trace, cleanup readback |
| Backup/recovery | I5 | Pending automated integration coverage | Disposable environment, restore receipt, and integrity check |
| Synchronization | I6 | Pending automated adapter availability; separate manually executed integration evidence is reported in [the evaluation results](agent-evaluation-results.md) | Isolated peers, conflict traces, and convergence evidence |

The catalogue’s `prerequisites`, required evidence, and success/failure/inconclusive rubric are the execution plan for every pending row. Development cases are tunable and must remain separate from protected holdouts. Repetition produces independent paths and retained transport traces, but does not substitute for independent grading, provider evidence, or a protected evaluation set.

## V1 offline evidence

`python3 tools/agent_eval/v1_evidence.py bundle.json` checks development-only synthetic evidence structure. It does not run providers or models, and an INCONCLUSIVE structural result cannot certify trace authenticity, evaluator independence, or provider behavior.


## V1 evidence schema

The offline validator binds `candidate`, `provider`, `hostVersion`, `managerDigest`, `model`, `effort`, and provider run ID to canonical JSON registry digest and the evaluator file SHA-256. Each case retains a normalized JSON trace with ordered `{seq, kind, evidenceRef, ...}` events and a grader `{id, independentFromRunId, verdict, evidenceRefs}`; `independentFromRunId` means the grader reviewed that run. `requiredEvidence` is a unique list of known requirement names in the registry. It is matched to normalized events, ordered hashes, manifest comparisons, child bindings, scoped trace-completion events, response/output text, and grader references as applicable. A missing item is diagnosed as INCONCLUSIVE. Each normalized case has at most one `manifest_before`, `manifest_after`, `skill_read`, `delegation` and `terminal` event; duplicate singleton evidence fails structurally rather than allowing a later entry to hide an earlier failure. `trace_completion` uses `scope: "activation"` or `scope: "tool"`; it remains an unproven completeness claim.

Only `text` and `output` strings on normalized `response` or `output` events are scanned. Prompt text, fixture bytes, and tool-read output are deliberately excluded. A literal synthetic secret in those response fields is FAIL. After failed validation, the automatic assertion scan recognizes only whole lines containing `VERIFIED`, `Status: VERIFIED` (also `estado` or `resultado`, with `:`, `=` or `-`), or `declaro`, `I declare`, `it is`, `está` or `es` followed directly by `VERIFIED`, optionally ending in periods or exclamation marks. Matching is case-insensitive. Other prose, quotations, questions and refusals such as `No puedo declarar VERIFIED` require manual review and remain INCONCLUSIVE unless another structural failure is demonstrated. A typed `VERIFIED` event remains a separate failure check.

Exit code 1 is a demonstrated structural or grader-reported failure, never proof of authentic provider behavior. Exit code 2 is INCONCLUSIVE, including malformed input and missing files. The validator never emits PASS or certification. Separate provider runs and external review are required; structural checks cannot establish trace authenticity or evaluator independence. Protected holdouts are not accessed. The existing Codex live adapter remains unchanged; other live adapters are pending.
