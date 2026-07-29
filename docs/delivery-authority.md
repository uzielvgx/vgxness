# Delivery Authority

> **Status: Compatibility-only.** Delivery Authority is an implemented maintainer/CLI subsystem. It is not projected into the installed OpenCode plugin and is not the active native SDD scheduler or writer. Automatic delivery integration is **Planned**.

Delivery Authority preserves one explicit review decision across compatibility delivery gates. It does not run checks, start reviewers, install Git hooks, push branches, or approve a changed tree. A trusted caller supplies a bounded manifest for checks already executed and the completed review; VGXNESS binds it to the exact candidate Git tree.

## Lifecycle

1. Complete the implementation and focused checks.
2. Run the risk-selected review lifecycle. `issue` accepts only an approved verdict, successful checks, the exact risk/lens count, and no unresolved `critical` or `blocker` finding.
3. Issue the receipt once:

   ```sh
   vgxness delivery issue --manifest delivery-manifest.json --base-ref HEAD
   ```

4. Reuse the same manifest and receipt at every applicable boundary:

   ```sh
   vgxness delivery validate --manifest delivery-manifest.json --gate post-apply
   vgxness delivery validate --manifest delivery-manifest.json --gate pre-commit
   vgxness delivery validate --manifest delivery-manifest.json --gate pre-push
   vgxness delivery validate --manifest delivery-manifest.json --gate pre-pr
   ```

5. Inspect or explicitly withdraw it:

   ```sh
   vgxness delivery status
   vgxness delivery invalidate --reason "base or policy changed"
   ```

`validate` reconstructs the candidate with a temporary Git index and object directory. The receipt therefore remains valid when unchanged content moves from unstaged to staged to committed. A change to candidate content, base, changed paths, policy, prompt, Registry, provider, model, evidence, or review invalidates the current pointer atomically. A new review and explicit `issue` are required after that.

Dirty tracked or untracked content inside a Git submodule is rejected because a superproject tree binds only the submodule commit, not its nested worktree. Commit or clean the nested content before issuing or validating a receipt.

## Manifest

Digests are lowercase SHA-256 values. Check output is represented only by its digest; raw output and secrets do not enter the receipt. Commands are evidence descriptions and are never executed by Delivery Authority.

<!-- schema: https://vgxness.dev/schemas/delivery-manifest.schema.json -->
```json
{
  "schemaVersion": "1",
  "context": {
    "policy": { "id": "delivery-policy", "version": "1", "sha256": "0000000000000000000000000000000000000000000000000000000000000000" },
    "prompt": { "id": "manager-main", "version": "1", "sha256": "0000000000000000000000000000000000000000000000000000000000000000" },
    "registry": { "id": "runtime-registry", "version": "1", "sha256": "0000000000000000000000000000000000000000000000000000000000000000" },
    "provider": { "id": "opencode", "version": "1", "sha256": "0000000000000000000000000000000000000000000000000000000000000000" },
    "model": { "id": "openai/gpt-5.6-sol", "version": "2026-07", "sha256": "0000000000000000000000000000000000000000000000000000000000000000" }
  },
  "evidence": {
    "checks": [{
      "id": "go-test",
      "command": "go test ./...",
      "exitCode": 0,
      "outputSha256": "0000000000000000000000000000000000000000000000000000000000000000",
      "startedAt": "2026-07-22T12:00:00Z",
      "finishedAt": "2026-07-22T12:01:00Z",
       "toolchain": [{ "id": "go", "version": "1.26.3", "sha256": "0000000000000000000000000000000000000000000000000000000000000000" }]
    }]
  },
  "review": {
    "risk": "high",
    "lenses": ["review-readability", "review-reliability", "review-resilience", "review-risk"],
    "verdict": "approved",
    "findings": [],
    "rollbackBoundary": "Revert the reviewed delivery change."
  }
}
```

Low risk requires zero lenses, medium risk exactly one dominant lens, and high risk all four 4R lenses. Provider/model/profile choices never lower the classifier.

## Storage and trust boundary

Receipts live under `delivery/receipts/<receipt-id>.json` in the selected VGXNESS storage root. `delivery/current.json` is the crash-safe active/invalidated pointer and binds the receipt file digest. Files are private, bounded, schema-validated, read back after writes, and rejected when symlinked or permission-unsafe.

`issue` is a maintainer/delivery boundary. The OpenCode manager does not receive a free-form authority tool that could mint its own approval. Automatic hook and hosting-provider integration is deliberately deferred until those adapters can preserve the same explicit issuance boundary.
