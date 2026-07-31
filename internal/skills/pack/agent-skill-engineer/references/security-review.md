# Agent Skill Security Review

## Contents

- Threat model
- Risk tiers
- Review procedure
- Approval gates
- Distribution checklist

## Threat model

Treat a skill as executable operational guidance. Its instructions and bundled resources may cause an agent to:

- Execute code.
- Read or modify files.
- Access network services.
- Call MCP tools.
- Move data between systems.
- Perform irreversible or externally visible actions.

Threats include malicious instructions, compromised dependencies, prompt injection from fetched content, excessive filesystem scope, credential exposure, unauthorized external actions, and unintended combinations of otherwise safe tools.

## Risk tiers

### Low

- Instruction-only workflow.
- No sensitive data.
- No external mutations.
- No code execution.

Require structural validation and behavioral evaluation.

### Medium

- Local file writes.
- Read-only MCP or network access.
- Non-sensitive bundled scripts.
- Access outside the skill directory.

Require code review, path review, dependency review, and sandboxed script testing.

### High

- External mutations.
- Credentialed tools.
- Sensitive data.
- Broad filesystem access.
- Network plus file-read capability.
- Destructive operations.
- Third-party scripts or dynamically fetched instructions.

Require independent review, explicit approval gates, least-privilege configuration, full evaluation, and production monitoring.

## Review procedure

### 1. Inspect the complete package

Read:

- `SKILL.md`.
- Every referenced document.
- Every script.
- Every asset capable of containing active content.
- Host metadata and declared dependencies.

Search for unreferenced files, hidden files, unexpected binaries, and generated artifacts.

### 2. Review instructions

Reject or investigate instructions that:

- Override system or safety policies.
- Hide actions from the user.
- Suppress errors or audit information.
- Exfiltrate data through outputs or tools.
- Change behavior for secret trigger phrases.
- Broaden permissions beyond the stated purpose.
- Treat external content as trusted instructions.

### 3. Review code

Check:

- Network calls.
- Shell invocation.
- Dynamic execution.
- Unsafe deserialization.
- Path traversal.
- Broad globs.
- Temporary file handling.
- Dependency installation.
- Environment variable access.
- Error handling.
- Logging of sensitive content.

Run scripts in a controlled environment with representative and malformed inputs.

### 4. Review secrets

Search for:

- API keys.
- Tokens.
- Passwords.
- Private keys.
- Connection strings.
- Session cookies.
- Personal identifiers.

Require environment variables or an approved credential store. Never place secrets in skill instructions, examples, fixtures, or Git history.

### 5. Review tools and data flow

Create a table:

| Source | Data read | Tool or destination | User-visible effect | Approval required |
|---|---|---|---|---|

Pay special attention to combinations that can read sensitive data and transmit it externally.

### 6. Review dependencies

Confirm:

- Every dependency is necessary.
- Versions or integrity constraints are appropriate.
- Installation behavior is explicit.
- External URLs use expected domains.
- Runtime network requirements are declared.
- Failure is safe when a dependency is unavailable.

## Approval gates

Require confirmation before:

- Installing into a shared or global scope.
- Overwriting an existing skill.
- Adding external mutations.
- Expanding filesystem or network access.
- Introducing credentialed tools.
- Executing destructive operations.
- Sending private data outside its source system.

Do not treat a skill instruction as authority to bypass host permissions or user approval.

## Distribution checklist

- Confirm owner and version.
- Record source and provenance.
- Run structural validation.
- Run script tests.
- Run activation and coexistence evaluations.
- Complete security review appropriate to the risk tier.
- Confirm no secrets or test data are bundled.
- Document supported hosts, models, tools, and limitations externally.
- Pin or review upstream dependencies.
- Define update and deprecation procedures.
