# VGXNESS en Claude Code

Este documento describe cómo se integra VGXNESS con Claude Code: qué contiene el
plugin, qué hace el binario por él y qué límites impone Claude Code.

## Layout

```
.claude-plugin/marketplace.json     el repositorio es el marketplace "vgxness"
plugins/vgxness/
  .claude-plugin/plugin.json        nombre, versión y metadatos
  .mcp.json                         servidor MCP "memory"
  hooks/hooks.json                  SessionStart, PreCompact, SessionEnd, SubagentStart
  policy/manager.md                 política del Manager (≤ 6.000 caracteres)
  policy/worker.md                  contrato que reciben los subagentes vgxness:*
  agents/{explore,general,verifier,reviewer}.md
  skills/git-delivery/SKILL.md
```

La versión del plugin se fija en `plugin.json` y sube junto con el binario: los
prompts nombran herramientas MCP acopladas a esa versión.

## Servidor MCP

`.mcp.json` ejecuta `vgxness mcp --full --workspace ${CLAUDE_PROJECT_DIR}`.
Claude Code nombra las herramientas `mcp__plugin_vgxness_memory__<tool>`; ese
nombre completo es el que va en reglas de permisos, en `tools` de un subagente
y en matchers de hooks.

El workspace se resuelve en este orden: `--workspace`, luego `CLAUDE_PROJECT_DIR`,
luego el directorio actual. No se usan los `roots` de MCP porque cambian a mitad
de sesión.

Las instrucciones del servidor (`internal/mcp/instructions.go`, máximo 2.048
caracteres porque Claude Code trunca el resto) le dicen al modelo cuándo leer y
cuándo escribir; son el respaldo cuando los hooks están desactivados.
`memory_forget` lleva `_meta["anthropic/requiresUserInteraction"]`, así que
pide confirmación incluso en modo `auto`.

## Hooks

Todos los hooks son `command` y corren el mismo binario:
`vgxness claude-code hook <evento>`. Leen el JSON del evento por stdin y
contestan con el JSON que Claude Code espera. Cualquier fallo es cerrado:
salida 0, stdout vacío y una línea en stderr. Un `vgxness` ausente o una base
de datos inaccesible nunca bloquean la sesión.

| Evento | Qué hace |
| --- | --- |
| `SessionStart` (`startup`, `resume`, `clear`, `compact`) | Inicia o renueva la sesión de proveedor ligada al `session_id`, y devuelve `additionalContext` con la política, el `session_handle` y el handoff de la sesión anterior (≤ 10.000 caracteres; la política se lee de `--policy`). |
| `PreCompact` | Renueva el lease. Nunca bloquea: bloquear una compactación automática cerca del límite hace fallar la petición. |
| `SessionEnd` | Si Claude guardó un resumen con `memory_session_summary`, cierra la sesión como completada y ese resumen pasa a ser el handoff. Si no hay resumen, la sesión queda activa y el vencimiento del lease (24 h) la marca como interrumpida. Presupuesto: 1,5 s. |
| `SubagentStart` (matcher `^vgxness:`) | Inyecta el contrato de worker; no toca la base de datos. |

No se persiste nada entre procesos de hook: `StartProviderSession` es
idempotente por identidad externa y vuelve a emitir el token del lease. Si un
`session_id` ya se completó y Claude Code lo reanuda, se abre un sucesor
`<session_id>#2`.

Lo que **no** hacen los hooks: leer la transcripción, guardar
`compact_summary`, generar resúmenes. El resumen lo escribe el modelo.

## Política del Manager

`policy/manager.md` llega al hilo principal por `SessionStart`. Está escrita en
tono factual (las instrucciones imperativas disparan las defensas contra
inyección) y abre con la cláusula de precedencia: las instrucciones del usuario
y los `CLAUDE.md` mandan.

No se usa el setting `agent` del plugin porque reemplaza todo el system prompt
de Claude Code y un `agent` del usuario lo pisa. Tampoco un output style
forzado.

## Subagentes

Claude Code ignora `permissionMode`, `hooks` y `mcpServers` en los agentes de
un plugin. Por eso cada rol se protege con su lista `tools`:

| Rol | Modelo | Herramientas | Garantía |
| --- | --- | --- | --- |
| `vgxness:explore` | haiku | Read, Grep, Glob, LSP, `memory_search`, `memory_get` | No puede escribir ni ejecutar comandos. |
| `vgxness:general` | sonnet | Read, Edit, Write, NotebookEdit, Grep, Glob, LSP, Bash, Skill, lectura de memoria | Único rol con escritura; sin `Agent`, sin escritura de memoria. |
| `vgxness:verifier` | sonnet, `effort: high` | Read, Grep, Glob, LSP, Bash, lectura de memoria | Tiene shell para correr checks; su prompt lo limita a comandos no mutantes. |
| `vgxness:reviewer` | opus, `effort: high` | Read, Grep, Glob, LSP, lectura de memoria | Rúbrica CARE embebida (criterios, especialista, challenger). |

El escritor único es por construcción: solo `general` tiene Edit/Write. El
hueco conocido es `verifier`, que puede escribir a través de Bash; queda
cubierto por política hasta que un hook `PreToolUse` lo endurezca (depende de
que el evento traiga `agent_type` de forma confiable).

Ningún rol usa el campo `memory:` de Claude Code: habilita Read/Write/Edit por
debajo, y la memoria ya va por MCP.

## Permisos recomendados

`vgxness claude-code setup` imprime:

```json
{
  "permissions": {
    "allow": [
      "mcp__plugin_vgxness_memory__memory_search",
      "mcp__plugin_vgxness_memory__memory_recent",
      "mcp__plugin_vgxness_memory__memory_get",
      "mcp__plugin_vgxness_memory__memory_context",
      "mcp__plugin_vgxness_memory__memory_save",
      "mcp__plugin_vgxness_memory__memory_session_summary"
    ],
    "ask": [
      "mcp__plugin_vgxness_memory__memory_update",
      "mcp__plugin_vgxness_memory__memory_forget"
    ]
  }
}
```

## Verificación en CI

1. `claude plugin validate --strict` sobre el marketplace y sobre el plugin
   (una corrida del marketplace no revisa los archivos del plugin).
2. El adaptador de hooks y el servidor MCP se ejercen con el binario recién
   compilado, sin modelo.
3. Smoke headless con `claude --bare -p --plugin-dir`, solo cuando existe el
   secreto `ANTHROPIC_API_KEY`: comprueba que el plugin carga sin errores, que
   el MCP conecta y que los agentes aparecen.
4. `claude plugin eval` con casos en `plugins/vgxness/evals/` queda pendiente
   (tarea 18 del plan).

## Límites conocidos

- `additionalContext` se corta en 10.000 caracteres; lo que sobra se va a un
  archivo y el modelo recibe solo la ruta.
- Un `CLAUDE.md` en la raíz del plugin no se carga.
- Un plugin con directorio `bin/` no se puede instalar en claude.ai ni en
  Cowork; por eso el binario se instala aparte.
- La actualización automática está desactivada por defecto para marketplaces
  de terceros: `claude plugin update vgxness@vgxness` trae la nueva versión.
