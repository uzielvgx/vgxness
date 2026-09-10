# Pi development evaluation transport

`tools/agent_eval/pi_runner.py` prepares isolated public development cases and records opt-in Pi execution evidence. It is optional Python standard-library development tooling. The installed Pi package remains TypeScript/Node and does not depend on this runner, Go, the VGXNESS CLI, or a VGXNESS MCP server.

A completed transport returns `completed-ungraded`. It never certifies the Manager's response, skill choice, or evaluation design. Independent semantic grading remains necessary. The existing public six-case baseline remains **5/6**, with its recorded failure retained. Additional GPT-5.4/GPT-5.5 evaluation coverage is excluded; runtime model support is unchanged.

## Prepare without executing Pi

The supported evaluation host is Linux with Node 24 and the repository's pinned Pi SDK, currently 0.84.4. Install repository dependencies using the normal project setup first. All inputs must be absolute, non-symlink paths. The output must be a new directory outside the repository.

A registry uses `schema: 1`, a `partition` of `public-development` (or `public development, not protected holdout`), and a nonempty `cases` list. Each case has a unique lowercase ID and a prompt; `fixtureFiles` maps safe relative paths to UTF-8 text. Protected/holdout cases and paths containing traversal or reserved `.pi`, `.agents`, or `.git` components are rejected. Prompts beginning with an option, `@` file argument, or slash command are rejected.

Example registry:

```json
{
  "schema": 1,
  "partition": "public-development",
  "cases": [{
    "id": "read-note",
    "prompt": "Read note.txt and report its contents.",
    "fixtureFiles": {"note.txt": "Synthetic evaluation note.\n"}
  }]
}
```

Prepare a reviewable plan, substituting actual absolute paths:

```bash
python3 tools/agent_eval/pi_runner.py plan \
  --repo /absolute/vgxness \
  --registry /absolute/cases.json \
  --node /absolute/node \
  --cli /absolute/vgxness/node_modules/@earendil-works/pi-coding-agent/dist/cli.js \
  --expected-version 0.84.4 \
  --provider openai-codex --model gpt-5.6-luna --thinking high \
  --auth-dir /absolute/normal-pi-account-directory \
  --skills-root /absolute/managed-skills \
  --output /absolute/new-evaluation-plan \
  --case read-note --timeout 120
```

Repeat `--case` to select multiple cases. Planning starts no subprocess, does not probe Node/Pi, and does not read or copy account credentials. Inspect `plan.json` and `plan.sha256` before authorizing execution. They bind the prompts, exact argument lists, executable and CLI files, dependency metadata, extension, harness, repository source inventory, registry, complete compatible managed skill catalog, copied resources, and fixture bytes/modes. Known ignored state directories and Git metadata are excluded explicitly; fixture inventories do not inherit those exclusions.

Every case receives a separate private HOME, workspace, and storage directory. The full compatible catalog is copied into that HOME; `expectedSkill` and grading rubrics do not select the skills exposed to the model. Files use mode 0600 and private directories use 0700. Internal extension session bookkeeping may write only to that case's temporary storage; this does not exercise or modify the user's shared memory database.

## Execute one authorized case

A live invocation uses the supplied normal Pi account and may incur provider charges. The CLI flag is a technical gate, not evidence of user approval. Authorize the exact plan digest, selected cases, model, and budget before running:

```bash
python3 tools/agent_eval/pi_runner.py run \
  --plan /absolute/new-evaluation-plan/plan.json \
  --plan-sha256 REVIEWED_64_CHARACTER_SHA256 \
  --case read-note --allow-live
```

Each case is permanently reserved on its first attempt, including a failed preflight. A retry requires a fresh reviewed plan. Source, catalog, fixture, and runtime bindings are checked before and after execution, including newly added files. Drift before launch prevents the target process from starting; post-run drift invalidates an otherwise completed transport.

The account directory is supplied through `PI_CODING_AGENT_DIR`; HOME is isolated independently. Account contents and environment values are not copied into the plan or receipt. The extension receives an explicit isolated, initially absent VGXNESS credential-file path.

The generated wrapper loads the real Pi extension and Manager prompt. Its application guard permits only fixture/copied-skill reads and `vgx_skill` list/read with declared resources. It checks the prompt's complete skill names and manifest locations and rechecks the copied bytes. Prohibited calls, catalog mismatches, and guard failures terminate the owned CLI. Read paging is supported. The guard limits the case to 64 tool attempts and audit records to 8192 bytes each.

The fixture contains evaluator-owned `.pi/settings.json` with agent/provider retries disabled. The exact argv includes `--approve` to trust **only that generated fixture**, along with disabled ambient extensions, context files, templates, and themes. It does not change trust settings for the user's repository.

The execution budget is at most 120 seconds per case, shared by version probes and the model process. Independent stdout/stderr streams are capped at 2 MiB each. Owned Linux process groups are terminated on timeout or overflow, including children holding pipes after their parent exits. Cleanup may take up to five additional seconds. There are no model retries or delegated workers.

## Evidence and limits

Receipts and private traces are retained below `runs/<case>/`. A successful result requires a zero exit code, a matching observed assistant provider/model, successful terminal assistant text, a non-retrying agent end followed by settlement, observed catalog validation, and unchanged bindings. JSON-mode assistant errors remain failures even if Pi exits zero.

Requested effort is recorded separately from observed fields; effective effort remains unproven. SDK usage and cost fields are estimates, not billing records. Preserve failed attempts and give the frozen plan, traces, tool observations, and rubric to an independent grader.

This is a trusted-host application guard, **not an OS sandbox**. Installed dependencies, account configuration, and the CLI remain trusted. Process-group cleanup cannot contain processes that deliberately escape the group. The runner does not prove Windows worker support, delegated implementation, large dirty Git reconstruction, protected holdout performance, or semantic correctness. The three simple development journeys must not be presented as those broader proofs.

Offline checks:

```bash
python3 tools/agent_eval/pi_runner.py --help
python3 tools/agent_eval/pi_runner.py --version
python3 tools/agent_eval/pi_runner.py self-test
python3 -m unittest discover -s tools/agent_eval -p 'test_*.py'
```

The unit suite uses fake child processes and a real Node import of the extension with a simulated Pi event host. It makes no model/account calls. Passing it proves the tested transport boundaries, not live Manager behavior.
