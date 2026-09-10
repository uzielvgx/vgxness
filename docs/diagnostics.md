# Diagnose a local installation

For operators checking the current workspace, `vgxness doctor` checks SQLite storage. `vgxness doctor --all` additionally inspects the shared launcher/skills and OpenCode, Codex and Pi installations. This command is available in source builds containing `internal/cli/doctor.go`; an older installed executable may reject `--all`.

Run from your project, or select it explicitly:

```sh
vgxness doctor --all --workspace /absolute/project
```

The checks use the same default provider roots as setup status, including Pi's `PI_CODING_AGENT_DIR` override. `--storage-root` and `--project-local` affect the storage check only. For installations in custom roots, use the existing `setup <provider> --status` directory flags. No acquisition, repair, installation, model invocation, or cloud synchronization is performed. The existing OpenCode handshake probe may execute; its result does not establish model access.

Read each component independently:

| Output | Meaning and next step |
| --- | --- |
| `storage=healthy` | Storage inspection succeeded; inspect the reported database and schema. |
| `launcher=installed`, `skills=installed` | Managed shared installation passed status inspection. |
| `<provider>.artifacts=installed` | Managed provider artifacts passed status inspection. |
| `opencode.runtime=healthy` | The existing OpenCode handshake returned healthy. |
| `codex.runtime=unobserved`, `pi.runtime=unobserved` | This diagnostic does not exercise those host runtimes. Installation health is not a runtime handshake. |
| `model_access=unobserved` | Provider authentication and actual model availability were not tested. |
| `pi.workers=unsupported` | Windows process-tree ownership is unavailable. Elsewhere workers remain `unobserved` until exercised. |
| `*=unavailable` or an artifact state other than installed | Read the component detail, then inspect its specific status command. Other independent checks continue. |

`doctor=incomplete` with exit **0** means all available installation/storage checks passed, with the displayed runtime/model evidence still unobserved. It never means full system verification. `doctor=attention` with exit **1** means at least one check failed, an adapter was unavailable, or a requested installation was absent; `--all` checks all three providers, even if you intentionally installed only one. Invalid flags return **2**; cancellation returns **130**.

For a component requiring attention, run `vgxness self status`, `vgxness skills status`, or `vgxness setup opencode|codex|pi --status` for its detailed inspection. Follow the recovery information returned there before making changes. Rerun the diagnostic after a repair; it does not repair automatically. Existing `status` and plain `doctor` output and exit codes remain unchanged.

The VGXNESS Manager maintains this guide alongside the CLI implementation and regression tests. Review it whenever diagnostic fields, provider status behavior, root selection, or runtime capabilities change. Output is plain text; no graphical interface or color perception is required.
