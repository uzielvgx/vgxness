# Cómo convertir VGXNESS en plugin nativo

Uziel, la respuesta corta: **Claude Code (v2.1.287 al 1 de octubre de 2026) ya trae casi toda la plomería que VGXNESS construyó para sí mismo**. Instala y versiona plugins desde un marketplace en git, aísla escritores en worktrees, corre subagentes en background con modelo y effort por rol, mide costo, emite trazas OTel y tiene un evaluador nativo de plugins (`claude plugin eval`). Lo que **no** trae es justo lo que hace valioso a VGXNESS: memoria de proyecto durable, consultable y sincronizable (SQLite/FTS5 + cloud sync), un handoff entre sesiones que no captura transcripts, y una política de Manager con disciplina de escritor único, verificación independiente de un candidato congelado y etiquetas de entrega estrictas. Mi recomendación es que VGXNESS sea un plugin `vgxness` publicado desde un subdirectorio `plugins/vgxness/` del mismo repo (que también hace de marketplace). El plugin declara `vgxness mcp --full --workspace ${CLAUDE_PROJECT_DIR}` como servidor stdio, entrega la política del Manager con un hook `SessionStart` (matchers `startup|resume|clear|compact`) que llama a un nuevo adaptador `vgxness claude-code hook`, y define los roles Explore/General/Verifier/Reviewer como **agentes del plugin**, con el read-only garantizado por allowlists de `tools` y no por `permissionMode`. Esto implica borrar bastante código: instalador con receipts/drift/backups, catálogo `~/.agents/skills`, runner de evals en Python y recorder de trazas, todo para este host. A cambio hay cuatro cambios concretos en Go: flag `--workspace` en `vgxness mcp`, `instructions` del servidor MCP, descripciones de tools del tipo qué + cuándo, y el adaptador de hooks. Hay varias suposiciones que deben validarse empíricamente antes de implementar (PATH en la app de escritorio, si `agent_type` llega a los hooks `PreToolUse`, comportamiento tras `/cd`). La versión mínima que recomiendo es **v2.1.284**, con piso duro en v2.1.269.

## Claude Code cubre ya de punta a punta la orquestación genérica

### Plugins y marketplaces: un directorio, un manifiesto opcional, un repo git

Un plugin es un directorio. El manifiesto `.claude-plugin/plugin.json` es opcional y solo exige `name`. Todo lo demás (`skills/`, `agents/`, `hooks/hooks.json`, `.mcp.json`, `output-styles/`, `bin/`, `settings.json`) vive en la raíz del plugin, nunca dentro de `.claude-plugin/`. Las rutas deben empezar con `./` y no salir de la raíz ([Manifest reference](https://code.claude.com/docs/en/plugins/manifest-reference)). Cada componente queda namespaced: el agente `reviewer` del plugin `deploy-tools` es `deploy-tools:reviewer`. Las tools de un servidor MCP del plugin se llaman `mcp__plugin_<plugin>_<server>__<tool>`, y un matcher escrito contra el nombre pelón del servidor **nunca dispara** ([Plugin components](https://code.claude.com/docs/en/plugins/components)). Hay dos restricciones que condicionan todo el diseño. Primera: **un `CLAUDE.md` en la raíz del plugin no se carga** (`claude plugin validate` lo advierte). Segunda: **el `settings.json` del plugin solo honra `agent` y `subagentStatusLine`**, así que un plugin no puede traer reglas de permisos, `env` ni `autoMemoryEnabled` ([Plugin components](https://code.claude.com/docs/en/plugins/components)).

Un marketplace es `.claude-plugin/marketplace.json` (requiere `name`, `owner.name` y `plugins[]`) dentro de cualquier repo git. Las fuentes relativas como `./plugins/foo` se resuelven desde la raíz del repo ([Marketplace reference](https://code.claude.com/docs/en/plugins/marketplace-reference)). El usuario corre `claude plugin marketplace add owner/repo` (acepta `--sparse <paths…>` para no clonar todo el árbol Go) y luego `claude plugin install <plugin>@<marketplace>` ([CLI reference](https://code.claude.com/docs/en/plugins/cli-reference)). Claude Code calcula la versión con esta prioridad: `version` del manifiesto, luego `version` de la entrada del marketplace, luego el SHA del commit. Un `"version"` fijo **deja a todos los usuarios en la copia cacheada** hasta que lo subas. Y el **auto-update viene apagado para marketplaces de terceros**, sin campo en `marketplace.json` para prenderlo ([Loading](https://code.claude.com/docs/en/plugins/loading); [Host a marketplace](https://code.claude.com/docs/en/plugins/host-marketplace)). La caché vive en `~/.claude/plugins/cache/<mkt>/<plugin>/<version>/`, que es `${CLAUDE_PLUGIN_ROOT}` y cambia con cada update. El estado persistente va en `${CLAUDE_PLUGIN_DATA}` (`~/.claude/plugins/data/<id>/`), pero **ese directorio se borra al desinstalar desde el último scope**, salvo que se use `--keep-data` ([CLI reference](https://code.claude.com/docs/en/plugins/cli-reference)). No existe un campo de binario por OS/arquitectura ni de "requiere CLI". El patrón documentado, igual al de los plugins LSP, es depender del PATH del usuario. Si el plugin trae un `bin/`, ese directorio solo entra al PATH de la tool Bash y además **bloquea la instalación en claude.ai/Cowork** ([Plugin components](https://code.claude.com/docs/en/plugins/components)). Para un producto que ya trae su propio CLI, la documentación recomienda que el instalador del CLI corra o imprima los dos comandos de `marketplace add` e `install` ([Publish](https://code.claude.com/docs/en/plugins/publish)).

### Subagentes y modelos: contexto fresco, reporte final, herencia por defecto

Un subagente es un Markdown con frontmatter cuyo cuerpo **reemplaza por completo** el system prompt de Claude Code. Solo recibe eso, detalles del entorno, el mensaje de delegación, la jerarquía de CLAUDE.md (salvo Explore, Plan o `omitClaudeMd`), un snapshot de git status y las skills precargadas. **No ve el historial, ni el output style, ni la auto memory del hilo principal**, y el padre solo recibe su reporte final ([Sub-agents](https://code.claude.com/docs/en/sub-agents); [Tools reference](https://code.claude.com/docs/en/tools-reference)). Los campos desconocidos se ignoran sin error, así que un typo como `disallowed_tools` deja escribible a un agente que creías "read-only". La precedencia es managed > `--agents` > proyecto > usuario > plugin. **Los agentes de plugin ignoran `hooks`, `mcpServers`, `permissionMode` e `initialPrompt`** ([Sub-agents](https://code.claude.com/docs/en/sub-agents)). El anidamiento llega a 3 niveles por defecto, con 20 subagentes concurrentes. En sesiones interactivas el fork mode está activo y todo corre en background. Un subagente terminado se puede retomar con `SendMessage`, y ningún mensaje de otro agente cuenta como aprobación de permisos. Las agent teams siguen siendo experimentales, solo funcionan en modo interactivo, y con `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` **cualquier spawn con `name` se convierte en teammate sin aislamiento** ([Agent teams](https://code.claude.com/docs/en/agent-teams)).

En la API de Anthropic los alias resuelven así: `opus` es Opus 5.5, `sonnet` es Sonnet 5.5, `haiku` es Haiku 4.5 y `fable` es Fable 5.1. Opus 5.5 requiere v2.1.280+ y Sonnet 5.5 v2.1.284+ ([Model config](https://code.claude.com/docs/en/model-config)). Para subagentes, el modelo se elige en este orden: parámetro por invocación, `model` del frontmatter, `CLAUDE_CODE_SUBAGENT_MODEL` y por último el modelo principal. `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` pisa todo. **El effort se puede fijar por agente; el thinking no** (se hereda, y en Opus 5.5, Sonnet 5.5 y Fable no se puede apagar) ([Sub-agents](https://code.claude.com/docs/en/sub-agents)). El built-in Explore ya no usa Haiku: hereda el modelo principal desde v2.1.198. Como todo hereda, **un Manager en Opus arrastra a Opus a cualquier rol sin `model:` explícito**. Hay otra trampa. Los modos `acceptEdits`, `auto` y `bypassPermissions` del hilo principal se imponen sobre el `permissionMode` del subagente ([Sub-agents](https://code.claude.com/docs/en/sub-agents)), y desde v2.1.283 **auto mode es el modo inicial por defecto en terminal y VS Code** ([Permission modes](https://code.claude.com/docs/en/permission-modes)). En la práctica, `permissionMode` en el frontmatter ya casi nunca es lo que protege a un rol.

### Hooks: ~30 eventos, contrato JSON estricto y un tope de 10.000 caracteres

Los hooks se configuran en tres niveles (evento → grupo con matcher → handlers) y se suman entre user, project, local, managed, el `hooks/hooks.json` del plugin y el frontmatter de skills/agentes. Los hooks de un plugin quedan registrados apenas carga el plugin ([Hooks reference](https://code.claude.com/docs/en/hooks)). Para VGXNESS importan `SessionStart` (sources `startup`, `resume`, `clear`, `compact`, `fork`), `PreCompact`, `SessionEnd`, `PreToolUse` y `SubagentStart/Stop`. Dentro de un subagente, el input trae `agent_id` y `agent_type`. **`SessionStart` solo admite handlers `command` y `mcp_tool`**, y `mcp_tool` se salta al lanzar la sesión, así que el handoff tiene que ser un comando. El stdout se parsea como JSON solo si empieza con `{` y termina con `}`. `hookSpecificOutput.additionalContext` llega como system reminder antes del primer prompt y **tiene un tope duro de 10.000 caracteres**. Lo que lo excede se guarda en un archivo, Claude recibe solo la ruta más un preview de 2.000 caracteres, y nadie le pide leer el resto ([Hooks reference](https://code.claude.com/docs/en/hooks)). La misma documentación pide redactar ese texto como afirmaciones factuales y no como órdenes de sistema, porque el framing imperativo "fuera de banda" puede disparar las defensas contra prompt injection. El wrapper actual de OpenCode ("Before your terminal response, use…") cae justo en ese patrón.

Hay tres detalles de latencia y costo que obligan a diseñar el adaptador con cuidado. La primera respuesta de Claude espera a que terminen los hooks `SessionStart`. **`SessionEnd` comparte un presupuesto de 1,5 s, y los `timeout` de hooks de plugin no lo amplían.** Además, los hooks `async` mueren al cerrar un `claude -p` ([Hooks reference](https://code.claude.com/docs/en/hooks)). Si falta el binario, el resultado es un error no bloqueante visible en cada sesión y en cada compactación. El contexto que inyectó un hook **no sobrevive verbatim a la compactación**: se resume junto con el resto, y la guía recomienda explícitamente un `SessionStart` con matcher `compact` para reinyectar lo crítico ([Hooks guide](https://code.claude.com/docs/en/hooks-guide); [Context window](https://code.claude.com/docs/en/context-window)). La forma exec (`command` + `args`, desde v2.1.139) evita el shell, los problemas de quoting y la corrupción por perfiles que hacen `echo` ([Hooks reference](https://code.claude.com/docs/en/hooks)).

### MCP y skills: el descubrimiento depende de las instructions del servidor

Con tool search activo por defecto, **al arrancar la sesión solo se cargan los nombres de las tools y las `instructions` de cada servidor**. Las definiciones completas se cargan bajo demanda, y descripciones e instructions se truncan a 2.048 caracteres ([MCP](https://code.claude.com/docs/en/mcp)). Hoy VGXNESS llama a `sdk.NewServer(..., nil)` sin instructions (`internal/mcp/server.go`), y sus descripciones dicen qué hace cada tool pero no cuándo usarla. Bajo tool search, eso es casi invisible para el modelo. Claude Code expone `CLAUDE_PROJECT_DIR` en el entorno del servidor stdio desde v2.1.139, y en configuraciones de plugin `${CLAUDE_PROJECT_DIR}` se sustituye directo en `command`/`args`. La documentación pide **no depender del cwd del proceso** ([MCP](https://code.claude.com/docs/en/mcp)). VGXNESS hoy resuelve el workspace con `os.Getwd()` (`mustWorkspace` en `internal/app/app.go`, y `vgxness mcp` no acepta `--workspace`). Bajo Claude Code eso puede apuntar a la raíz del plugin o a un subdirectorio, y es un bug de identidad de proyecto esperando a pasar. Los servidores stdio no se reconectan solos, y su salida avisa a los 10k tokens y se corta en 25k. Además, `_meta["anthropic/requiresUserInteraction"]: true` fuerza confirmación en todo modo, incluso `auto` y `bypassPermissions` ([MCP](https://code.claude.com/docs/en/mcp)).

Una skill es `SKILL.md` con frontmatter. En contexto solo vive la descripción (truncada a 1.536 caracteres junto con `when_to_use`). El cuerpo entra al invocarse y se queda hasta el final de la sesión, y tras compactar se re-adjunta hasta 5.000 tokens por skill ([Skills](https://code.claude.com/docs/en/skills)). Claude Code **no escanea `~/.agents/skills`**. `disable-model-invocation: true` saca la skill del listado, lo cual es correcto para flujos con efectos secundarios como git-delivery. Para portabilidad con el estándar agentskills.io conviene quedarse en los seis campos del spec ([Agent Skills spec](https://agentskills.io/specification)).

### Instrucciones, memoria, settings y permisos: dos niveles de autoridad

CLAUDE.md y `.claude/rules/` se concatenan y llegan como **mensaje de usuario después del system prompt**, "sin garantía de cumplimiento estricto" ([Memory](https://code.claude.com/docs/en/memory)). Output styles, `--append-system-prompt`, el setting `agent` y las instructions MCP viven a nivel de system prompt. Un output style propio **descarta las instrucciones de ingeniería de Claude Code** salvo que declare `keep-coding-instructions: true`, y `force-for-plugin: true` pisa el `outputStyle` del usuario ([Output styles](https://code.claude.com/docs/en/output-styles)). La auto memory viene activa: guarda notas por repo en `~/.claude/projects/<project>/memory/` y carga las primeras 200 líneas de `MEMORY.md` en cada sesión. **Su tipo `project` se pisa directamente con la memoria de VGXNESS**, y solo deduplica contra CLAUDE.md, no contra MCP ([Memory](https://code.claude.com/docs/en/memory)). Las reglas de permisos se evalúan deny → ask → allow y se combinan entre scopes. Los globs de allow solo valen después de un prefijo literal `mcp__<server>__` ([Permissions](https://code.claude.com/docs/en/permissions)). `.claude/` es ruta protegida: ninguna escritura ahí se auto-aprueba fuera de bypass ([Permission modes](https://code.claude.com/docs/en/permission-modes)).

### Headless, Agent SDK y evals: CI sin harness propio

`claude -p` es la cara CLI del Agent SDK. **`--bare` omite hooks, skills, plugins instalados, MCP, auto memory y CLAUDE.md**, requiere `ANTHROPIC_API_KEY` y, según la documentación, será el default de `-p` en el futuro ([Headless](https://code.claude.com/docs/en/headless)). En `--output-format stream-json --verbose`, el evento `system/init` trae `plugins`, `plugin_errors`, `mcp_servers`, `mcp_server_errors`, `skills` y `agents`, y la documentación cubre explícitamente "fallar CI si un plugin o servidor MCP no cargó" ([Headless](https://code.claude.com/docs/en/headless); [TS SDK reference](https://code.claude.com/docs/en/agent-sdk/typescript)). El SDK carga plugins con `plugins: [{type: "local", path}]` y aísla configuración con `settingSources: []` ([SDK plugins](https://code.claude.com/docs/en/agent-sdk/plugins); [SDK features](https://code.claude.com/docs/en/agent-sdk/claude-code-features)). Desde v2.1.269, `claude plugin eval` corre casos en `evals/<case>/prompt.md` con graders `regex`, `tool_used`, `tool_order`, `file_exists`, `llm` y `baseline`. Usa 3 corridas por brazo, ablation con/sin plugin, mocks MCP con record/replay, aislamiento con HOME temporal y JSON versionado con exit codes para CI. **No admite graders de código propio** ([Plugin evals](https://code.claude.com/docs/en/plugin-evals)).

### Orquestación nativa: worktrees, background, tasks y plan mode

`isolation: worktree` le da al subagente un worktree temporal y **bloquea Edit/Write y Bash dirigidos al checkout principal**. Pero el worktree **sale de la rama por defecto, no del HEAD**, salvo que `worktree.baseRef: "head"` esté en settings ([Worktrees](https://code.claude.com/docs/en/worktrees)). Ctrl+B, `/tasks`, agent view, dynamic workflows y `/batch` cubren el paralelismo ([Run agents in parallel](https://code.claude.com/docs/en/agents); [Workflows](https://code.claude.com/docs/en/workflows)). Las Task tools (`TaskCreate/Update/List`) **vienen apagadas por defecto en Opus 5.5 y Sonnet 5.5** desde v2.1.268, y además viven en `~/.claude/tasks/`, fuera del repo ([Tools reference](https://code.claude.com/docs/en/tools-reference)). Plan mode es un modo de permisos con aprobación vía `ExitPlanMode`. El plan se guarda en `~/.claude/plans` salvo que se configure `plansDirectory`, y no impone checklist ni verificación por tarea ([Permission modes](https://code.claude.com/docs/en/permission-modes); [Settings reference](https://code.claude.com/docs/en/settings-reference)). Checkpoints y `/rewind` cubren rollback de ediciones, aunque no de cambios hechos por Bash ([Checkpointing](https://code.claude.com/docs/en/checkpointing)). OTel exporta `skill_activated`, `subagent_completed`, `tool_decision` y spans por tool y por hook ([Monitoring](https://code.claude.com/docs/en/monitoring-usage)).

## Tres desacuerdos de los investigadores, resueltos

### (a) La política del Manager entra por SessionStart, no por el setting `agent`

Las notas proponen cuatro canales. La comparación queda así:

| Canal | Nivel | Sobrevive compactación | Costo para el usuario | Veredicto |
|---|---|---|---|---|
| Hook `SessionStart` → `additionalContext` | system reminder en historial | sí, si matchea `compact` | ninguno; se suma a su CLAUDE.md y su style | **Primario** |
| `instructions` del servidor MCP | system prompt | sí | ninguno; ≤2.048 chars | **Piso mínimo** (resiste `disableAllHooks`) |
| Output style con `keep-coding-instructions: true` | system prompt | sí | reemplaza el style elegido por el usuario | Opt-in para power users, nunca `force-for-plugin` |
| `agent` en `settings.json` del plugin o del proyecto | system prompt | sí | **reemplaza todo el prompt de Claude Code**; el `agent` del usuario lo pisa | Solo opt-in vía `claude --agent vgxness:manager` |

La nota de subagentes recomendaba correr el Manager como agente de hilo principal (`"agent": "vgx-manager"` en `.claude/settings.json`) porque es el único contexto donde funciona la allowlist `Agent(vgx-explore, …)` para spawns ([Sub-agents](https://code.claude.com/docs/en/sub-agents)). Es un argumento fuerte, pero lo descarto como default por tres razones. Primera: el cuerpo del agente **reemplaza el system prompt completo** de Claude Code, que es justo la guía de uso de tools, seguridad en git y verificación en la que un Manager de código se apoya. Reescribirla dentro de VGXNESS es deuda permanente que además envejece con cada release. Segunda: el `agent` de un plugin es la capa de settings más baja ("el `agent` del usuario lo pisa", y si dos plugins lo fijan gana el último cargado), así que no da la garantía que promete ([Plugin components](https://code.claude.com/docs/en/plugins/components)). Tercera: la variante de proyecto exige escribir en `.claude/` del repo del usuario, que es ruta protegida, y nos devuelve al problema de receipts/drift que queremos eliminar. **Recomiendo con confianza media-alta:** el hook `SessionStart` (matcher `startup|resume|clear|compact`) imprime la política del Manager en 3–5k caracteres (muy por debajo del tope de 10k), redactada en forma factual y abierta con una cláusula de precedencia ("las instrucciones explícitas del usuario y su CLAUDE.md prevalecen; esta política llena huecos"). A eso le sigue un digest de estado de 1–3 líneas: plan activo, session handle de VGXNESS y quién es el escritor. Las `instructions` MCP cargan una versión de ~500 caracteres útiles al inicio, para que la memoria siga funcionando aunque los hooks estén deshabilitados. La debilidad, que reconozco, es la autoridad de nivel mensaje y que la restricción de spawns queda como política y no como mecanismo. Lo compensa un hook `PreToolUse` (sección de mapeo) más un snippet opcional de `permissions.deny` para `Agent(Explore)`/`Agent(general-purpose)`. Si las evals muestran que el modelo ignora la política inyectada, el siguiente paso es el output style opt-in, no el setting `agent`.

### (b) Los roles se distribuyen como agentes del plugin, con read-only por `tools`

La nota de subagentes recomendaba agentes de proyecto en `.claude/agents/` para conservar `permissionMode`, `hooks` y `mcpServers`. La de plugins recomendaba agentes del plugin. **Me quedo con agentes del plugin, con confianza alta.** El argumento decisivo sale de la misma nota de subagentes: cuando el hilo principal está en `acceptEdits`, `auto` o `bypassPermissions`, el `permissionMode` del subagente se ignora ([Sub-agents](https://code.claude.com/docs/en/sub-agents)), y desde v2.1.283 `auto` es el modo inicial por defecto ([Permission modes](https://code.claude.com/docs/en/permission-modes)). O sea, el campo que "perdemos" ya no protegía en el caso común. La garantía real de read-only siempre fue la allowlist `tools` sin Edit/Write/NotebookEdit/Agent, y eso **sí funciona en agentes de plugin**. `mcpServers` no hace falta, porque el servidor del plugin se hereda y se acota nombrando `mcp__plugin_vgxness_memory__<tool>` en `tools` ([MCP](https://code.claude.com/docs/en/mcp)). Los `hooks` por agente se mueven al `hooks/hooks.json` del plugin y filtran por `agent_type` (`vgxness:general`, `vgxness:verifier`) dentro del adaptador. Lo que se cede de verdad es poco: no se puede sobrescribir el built-in `Explore` por nombre (los agentes de plugin quedan namespaced y en la prioridad más baja), así que la política tiene que pedir explícitamente `vgxness:explore`. Lo que se gana es grande: cero archivos escritos en el repo del usuario, un solo artefacto versionado y desinstalación limpia. La salida de emergencia documentada (copiar los `.md` a `~/.claude/agents/`) queda para el caso en que una prueba empírica demuestre que hace falta un hook por agente imposible de emular a nivel plugin.

### (c) El conflicto de fast mode: hay que creerle a la página dedicada

La página de configuración de modelos dice que "`/fast on` cambia al modelo rápido (normalmente Sonnet)". La página de fast mode dice que es una research preview **solo para Opus** (5.5, 5 y 4.8), "hasta 2,5x más rápido a mayor costo por token", **sin ser otro modelo**, a $8/$40 por MTok en Opus 5.5, cobrada solo de usage credits en suscripciones y no disponible en Bedrock/Vertex/Foundry ([Fast mode](https://code.claude.com/docs/en/fast-mode)). Le creo a la página dedicada. Es más específica, trae precios y lista de modelos, y la línea en conflicto viene de un fetch resumido por un modelo, no del texto verbatim ([Model config](https://code.claude.com/docs/en/model-config)). La consecuencia de diseño es simple: **VGXNESS no debe referenciar fast mode en ningún rol** (no hay campo de frontmatter para eso, y en teammates queda fijo al hacer spawn) ni asumir que abarata algo. Es decisión de costo del usuario y no de la política.

### Otras reconciliaciones menores

Las notas discrepan sobre dónde guardar estado. Una sugiere `${CLAUDE_PLUGIN_DATA}` para estado durable y otra lo prohíbe. **La base SQLite se queda en el directorio de datos propio de VGXNESS**, porque `${CLAUDE_PLUGIN_DATA}` se borra al desinstalar ([CLI reference](https://code.claude.com/docs/en/plugins/cli-reference)) y perder la memoria del proyecto por un `uninstall` sería inaceptable. `session_handle` y `lease_token` van en esa misma storage, con `session_id` de Claude como clave. Sobre el tope de 2.048 caracteres de las instructions MCP, una nota lo apoyaba en issues de terceros, pero la nota de MCP lo encontró en el texto oficial, que además documenta el override `CLAUDE_CODE_MAX_MCP_DESCRIPTION_LENGTH` ([MCP](https://code.claude.com/docs/en/mcp)). Lo trato como confirmado. Sobre el nombre del servidor, uso `memory` dentro del plugin `vgxness`, lo que da tools `mcp__plugin_vgxness_memory__memory_search` (con guiones preservados, contra lo que mostraba un resumen erróneo).

## Mapeo para VGXNESS

### Layout del repo y del plugin

El repo `uzielvgx/vgxness` es a la vez producto y marketplace. Solo `plugins/vgxness/` se copia a la caché, así que el código Go, el `CLAUDE.md` del repo y `docs/` nunca le llegan al usuario.

```text
vgxness/                              # raíz del repo Go = raíz del marketplace
├── .claude-plugin/marketplace.json
├── plugins/vgxness/                  # raíz del plugin (lo único que se cachea)
│   ├── .claude-plugin/plugin.json
│   ├── .mcp.json
│   ├── hooks/hooks.json
│   ├── agents/{explore,general,verifier,reviewer,manager}.md
│   ├── skills/{git-delivery,plan,care-review}/SKILL.md (+ references/)
│   ├── output-styles/manager.md      # opt-in, sin force-for-plugin
│   └── evals/<case>/prompt.md, graders/, mocks/memory/
├── cmd/ internal/ ...                # Go, fuera del plugin
```

Instalación: `claude plugin marketplace add uzielvgx/vgxness --sparse .claude-plugin plugins` y después `claude plugin install vgxness@vgxness`. Esos dos comandos los ejecuta `vgxness setup claude-code` ([Publish](https://code.claude.com/docs/en/plugins/publish)). No incluyas `bin/`.

### Sketches de manifiestos

```json
// .claude-plugin/marketplace.json
{
  "$schema": "https://anthropic.com/claude-code/marketplace.schema.json",
  "name": "vgxness",
  "description": "VGXNESS durable project memory and orchestration policy for Claude Code",
  "owner": { "name": "Uziel Vega", "url": "https://github.com/uzielvgx" },
  "plugins": [{ "name": "vgxness", "source": "./plugins/vgxness", "category": "productivity" }]
}
```

```json
// plugins/vgxness/.claude-plugin/plugin.json
{
  "name": "vgxness",
  "displayName": "VGXNESS",
  "version": "1.1.0",
  "description": "Durable SQLite/FTS5 project memory over MCP, cloud sync, and a Manager/worker orchestration policy",
  "author": { "name": "Uziel Vega", "url": "https://github.com/uzielvgx" },
  "homepage": "https://github.com/uzielvgx/vgxness",
  "repository": "https://github.com/uzielvgx/vgxness",
  "license": "<SPDX>",
  "keywords": ["memory", "mcp", "orchestration"]
}
```

`version` va **solo** en `plugin.json`, se fija y se sube en lockstep con el binario mediante `claude plugin tag plugins/vgxness --push`, que crea `vgxness--vX.Y.Z` ([CLI reference](https://code.claude.com/docs/en/plugins/cli-reference)). La razón es que los prompts nombran tools MCP acopladas a una versión concreta del binario.

```json
// plugins/vgxness/.mcp.json
{
  "mcpServers": {
    "memory": {
      "command": "vgxness",
      "args": ["mcp", "--full", "--workspace", "${CLAUDE_PROJECT_DIR}"],
      "env": { "VGXNESS_CLIENT": "claude-code" }
    }
  }
}
```

Sin `alwaysLoad`: el camino documentado es deferral más buenas instructions. Si la prueba de PATH en la app de escritorio falla, `command` pasa a `${CLAUDE_PLUGIN_ROOT}/scripts/vgxness-mcp.sh`, un launcher que busca el binario (PATH, `~/go/bin`, `/opt/homebrew/bin`) y hace `exec`.

```json
// plugins/vgxness/hooks/hooks.json
{
  "description": "VGXNESS: Manager policy + same-project handoff, lease lifecycle, single-writer guard",
  "hooks": {
    "SessionStart": [{ "matcher": "startup|resume|clear|compact", "hooks": [
      { "type": "command", "command": "vgxness", "args": ["claude-code", "hook", "session-start"],
        "timeout": 5, "statusMessage": "Loading VGXNESS context" } ] }],
    "PreCompact": [{ "matcher": "manual|auto", "hooks": [
      { "type": "command", "command": "vgxness", "args": ["claude-code", "hook", "pre-compact"], "timeout": 5 } ] }],
    "SessionEnd": [{ "hooks": [
      { "type": "command", "command": "vgxness", "args": ["claude-code", "hook", "session-end"], "timeout": 2 } ] }],
    "PreToolUse": [{ "matcher": "Edit|Write|NotebookEdit|Bash", "hooks": [
      { "type": "command", "command": "vgxness", "args": ["claude-code", "hook", "pre-tool-use"], "timeout": 3 } ] }],
    "SubagentStart": [{ "matcher": "^vgxness:", "hooks": [
      { "type": "command", "command": "vgxness", "args": ["claude-code", "hook", "subagent-start"], "timeout": 3 } ] }]
  }
}
```

Esto es lo que hace cada handler. `session-start` toma `cwd` (o `CLAUDE_PROJECT_DIR`) como workspace, `claude-code` como provider y `session_id` como `external_id`. Ejecuta `start` (idempotente) y `context`, y emite un único objeto JSON con la política y el handoff envuelto como `<UNTRUSTED DATA>` (≤6k caracteres en total). En `resume`, si el handoff no cambió, lo omite. `pre-compact` solo renueva el lease y **nunca bloquea**, porque bloquear una auto-compactación cerca del límite hace fallar el request ([Hooks reference](https://code.claude.com/docs/en/hooks)). `session-end` es una escritura SQLite mínima, muy por debajo de 1,5 s y sin cloud sync en el camino. Si un crash impide que corra, la expiración del lease marca `interrupted`. `pre-tool-use` devuelve `permissionDecision: "deny"`, que se respeta incluso en bypass ([Hooks guide](https://code.claude.com/docs/en/hooks-guide)), en tres casos: Edit/Write desde un `agent_type` distinto de `vgxness:general`, cualquier escritura o `sqlite3` contra la ruta de la DB de VGXNESS, y comandos Bash que mutan git (`commit`, `reset`, `checkout`, `push`) cuando vienen de `vgxness:verifier` o `vgxness:reviewer`. Para cualquier otro caso responde `exit 0` en milisegundos. El matcher del `SubagentStart` es regex (lleva `:` y `^`) e inyecta un contrato de worker corto, porque los subagentes no ven la política del hilo principal. El resumen de handoff **lo sigue escribiendo el modelo** vía `memory_session_summary`. No se persiste `compact_summary` ni `last_assistant_message`, porque sería captura de transcript.

### Frontmatter por rol

```yaml
# agents/explore.md  →  vgxness:explore
---
name: explore
description: Read-only codebase and project-memory search. Use proactively to locate code, symbols, call paths and prior decisions. Never edits.
tools: Read, Grep, Glob, LSP, mcp__plugin_vgxness_memory__memory_search, mcp__plugin_vgxness_memory__memory_recent, mcp__plugin_vgxness_memory__memory_get
model: haiku
effort: low
omitClaudeMd: true
color: cyan
---
```

```yaml
# agents/general.md  →  vgxness:general (único escritor)
---
name: general
description: Implements one bounded, Manager-approved change. The only role allowed to edit the workspace. Does not delegate.
tools: Read, Grep, Glob, LSP, Edit, Write, Bash, Skill, mcp__plugin_vgxness_memory__memory_search, mcp__plugin_vgxness_memory__memory_get, mcp__plugin_vgxness_memory__memory_save
model: sonnet
effort: medium
maxTurns: 60
color: green
---
```

```yaml
# agents/verifier.md  →  vgxness:verifier
---
name: verifier
description: Independently verifies a frozen candidate (commit SHA or diff hash given by the Manager). Runs builds and tests; never edits; reports evidence.
tools: Read, Grep, Glob, LSP, Bash, mcp__plugin_vgxness_memory__memory_get
model: sonnet
effort: high
color: yellow
---
```

```yaml
# agents/reviewer.md  →  vgxness:reviewer (CARE fusionado)
---
name: reviewer
description: Reviews a frozen candidate for correctness, architecture, risk/security and ergonomics. Read-only; returns findings ranked by severity.
tools: Read, Grep, Glob, LSP, mcp__plugin_vgxness_memory__memory_search, mcp__plugin_vgxness_memory__memory_get
model: opus
effort: high
skills: [care-review]
color: purple
---
```

Las reglas que valen para todos los roles son estas. Usa allowlist `tools`, nunca denylist, para que tools o servidores nuevos no se hereden por accidente. Ninguno lleva Agent, así que nadie anida. **Ninguno lleva `memory:`**, porque ese campo habilita Write/Edit automáticamente y rompería el read-only ([Sub-agents](https://code.claude.com/docs/en/sub-agents)). Todos fijan `model` explícito para no heredar Opus. Y nada de `isolation: worktree` en el Verifier: el worktree sale de la rama por defecto y verificaría el código equivocado, salvo que el usuario configure `worktree.baseRef: "head"` ([Worktrees](https://code.claude.com/docs/en/worktrees)). El `manager.md` opcional lleva el cuerpo completo de la política para quien quiera `claude --agent vgxness:manager`, con `tools: Agent(vgxness:explore, vgxness:general, vgxness:verifier, vgxness:reviewer), Read, Grep, Glob, Bash, Edit, Write, Skill, SendMessage, mcp__plugin_vgxness_memory__*`. Ojo: la allowlist `Agent(...)` con nombres namespaced desde un agente de plugin está **sin verificar**.

### Lo que VGXNESS deja de hacer porque Claude Code ya lo hace

| Hoy en VGXNESS | Reemplazo nativo |
|---|---|
| Instalador con receipts, drift, backups | `installed_plugins.json`, caché versionada, limpieza a 14 días, `uninstall` ([Loading](https://code.claude.com/docs/en/plugins/loading)) |
| Catálogo portable `~/.agents/skills` y su registry/locks | `skills/` del plugin, namespaced `/vgxness:*` ([Skills](https://code.claude.com/docs/en/skills)) |
| Runner offline de evals en Python | `claude plugin eval` + SDK harness solo para aserciones de código ([Plugin evals](https://code.claude.com/docs/en/plugin-evals)) |
| Recorder de trazas y contabilidad de costo | stream-json, `modelUsage`, OTel ([Monitoring](https://code.claude.com/docs/en/monitoring-usage)) |
| Plomería de background, fan-out paralelo, worktrees | subagentes en background, `/tasks`, `isolation: worktree` ([Worktrees](https://code.claude.com/docs/en/worktrees)) |
| Topes de turnos y presupuesto | `maxTurns`, `--max-turns`, `--max-budget-usd` ([CLI reference](https://code.claude.com/docs/en/cli-reference)) |
| Providers OpenCode/Codex/Pi (`internal/providers/*`) y `setupflow` | Fuera de alcance en un producto solo-Claude Code |

El último renglón es una decisión grande y no reversible barata. Recomiendo borrar esos providers **en una fase posterior**, después de que el plugin pase las evals, y no en el mismo cambio.

### Lo que sigue siendo trabajo de VGXNESS

Siguen siendo de VGXNESS la memoria durable por proyecto (SQLite/FTS5, scoping por proyecto canónico, cloud sync con su propia auth) y el handoff entre sesiones sin transcripts. También los planes Markdown en `docs/plans/` con checklist y retomar desde la primera tarea sin marcar, porque las tasks nativas son efímeras y vienen apagadas en los modelos actuales ([Tools reference](https://code.claude.com/docs/en/tools-reference)). Opcionalmente, un snippet `plansDirectory: "docs/plans"` alinea plan mode. Queda además la política del Manager con su lógica: clasificación de requests, a lo más un General vivo, congelar el candidato antes de verificar y revisar, no usar forks para verificación (heredan el razonamiento del implementador) y las etiquetas de entrega. Por último, la división de trabajo con la auto memory. La política tiene que decir explícitamente que las decisiones de proyecto van a VGXNESS y no se copian a `MEMORY.md`, mientras que las preferencias personales (`user`/`feedback`) se quedan en auto memory. VGXNESS no puede ni debe apagar la auto memory. A lo sumo documenta `{"autoMemoryEnabled": false}` como opt-in ([Memory](https://code.claude.com/docs/en/memory)). Como el plugin no puede traer permisos, `vgxness setup claude-code` imprime, y escribe solo con flag explícito, este snippet:

```json
{
  "permissions": {
    "allow": [
      "mcp__plugin_vgxness_memory__memory_search", "mcp__plugin_vgxness_memory__memory_recent",
      "mcp__plugin_vgxness_memory__memory_get", "mcp__plugin_vgxness_memory__memory_context",
      "mcp__plugin_vgxness_memory__memory_save", "mcp__plugin_vgxness_memory__memory_session_summary"
    ],
    "ask": ["mcp__plugin_vgxness_memory__memory_update", "mcp__plugin_vgxness_memory__memory_forget"]
  }
}
```

### Cambios requeridos en el código Go

Primero, **workspace explícito**: `runMCP` en `internal/cli/cli.go` gana el flag `--workspace` con el orden `--workspace` > `CLAUDE_PROJECT_DIR` > `os.Getwd()`. Hay que tocar también `mustWorkspace` en `internal/app/app.go` y conservar la canonicalización actual (`Abs` + `EvalSymlinks` en `canonicalInvocationWorkspace`). No conviene usar `roots/list` para identidad, porque cambia a mitad de sesión ([MCP](https://code.claude.com/docs/en/mcp)). Segundo, **instructions del servidor**: reemplazar el `nil` de `sdk.NewServer` en `internal/mcp/server.go` por opciones con `Instructions` de ≤2.048 caracteres, con las reglas críticas en los primeros ~500 (el nombre exacto del campo en el go-sdk está sin verificar). Tercero, **descripciones del tipo qué + cuándo**. Por ejemplo, `memory_search`: "Search this project's saved decisions, conventions and past fixes. Use before non-trivial work or when the user references earlier sessions". Además, `_meta["anthropic/requiresUserInteraction"]` en `memory_forget`, `limit` por defecto en search/recent para quedar muy por debajo de 10k tokens, y texto compacto en `content` junto al `structuredContent`. Cuarto, el **adaptador `vgxness claude-code hook <event>`**. Hoy `memory hook --stdin` (`internal/cli/memory.go`) rechaza cualquier llave desconocida y emite JSON propio, así que no se le puede pasar el stdin de Claude. El adaptador debe llamar al runtime directamente, guardar handle y lease por `session_id`, fallar cerrado (cualquier error interno es `exit 0` con stdout vacío y una línea en stderr que solo va al debug log), serializar con `encoding/json` y tardar menos de 1 s. Si alguna vez lanza un proceso hijo, debe cerrar stdio heredado, porque v2.1.285 tuvo que arreglar hooks colgados por hijos que mantenían stdout abierto ([Changelog](https://code.claude.com/docs/en/changelog)). Quinto, **una sola fuente de verdad para la política**: el texto del Manager vive embebido en Go (`go:embed`), se emite desde `session-start` y se expone como `vgxness claude-code policy` para generar `agents/manager.md` y `output-styles/manager.md` en build. El adaptador lee `${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json` (la variable se exporta a los hooks) y avisa con `systemMessage` si la versión del plugin y la del binario no son compatibles. Sexto, **setup y update**: `vgxness setup claude-code` corre `marketplace add` + `install`, y `vgxness self update` corre además `claude plugin update vgxness@vgxness`, porque el auto-update de terceros viene apagado ([Host a marketplace](https://code.claude.com/docs/en/plugins/host-marketplace)).

### Estrategia de CI y testing

Cada PR corre `go test ./...` y luego `claude plugin validate . --strict` y `claude plugin validate ./plugins/vgxness --strict`. Las dos validaciones van por separado porque validar el marketplace no abre los componentes de cada plugin, y desde v2.1.281 se revisan también las entradas de `.mcp.json` ([CLI reference](https://code.claude.com/docs/en/plugins/cli-reference)). Agrega un chequeo propio de llaves de frontmatter, porque los typos fallan en silencio. Después va un smoke test determinista y barato con un binario `vgxness` recién compilado en el PATH:

```bash
claude --bare -p "/vgxness:plan smoke" --plugin-dir ./plugins/vgxness \
  --output-format stream-json --verbose --permission-mode dontAsk \
  --permission-prompts none --max-turns 8 --max-budget-usd 1 \
  --model claude-sonnet-5-5 --no-session-persistence > run.ndjson
```

Con `jq` hay que verificar varias cosas: que `system/init` traiga `vgxness` en `plugins`, que no existan `plugin_errors` ni `mcp_server_errors`, que `mcp_servers` muestre `plugin:vgxness:memory` conectado, que aparezca un `tool_use` de `Skill` y que `result.subtype == "success"` ([Headless](https://code.claude.com/docs/en/headless)). Ojo con un matiz: `--bare` omite hooks y MCP descubiertos, pero `--plugin-dir` vuelve a cargarlos de forma explícita, y eso hay que confirmarlo en el primer run. El gate de comportamiento es `claude plugin eval ./plugins/vgxness --trust-plugin --json results.json --threshold 0.8 --model <pinned> --judge-model <pinned> --no-publish --max-cost-usd 20 --ablation none`, con `with-without` en el nightly para medir Δ ([Plugin evals](https://code.claude.com/docs/en/plugin-evals)). Los graders útiles son `tool_used` con `min:0,max:0` sobre Edit para roles read-only, `tool_order` (plan escrito antes de implementar, verificación antes de "DELIVERED"), `regex` sobre las etiquetas de entrega y `file_exists` sobre `docs/plans/*.md`. Los casos se siembran con `scaffold_script` (que haga `git init`) y se mockea el servidor `memory`. Lo que esos graders no pueden expresar ("solo `vgxness:general` editó archivos") va en un harness nocturno con el TS Agent SDK, usando `settingSources: []` y callbacks `PreToolUse`/`SubagentStart` que registren `agent_id` ([SDK hooks](https://code.claude.com/docs/en/agent-sdk/hooks)). La versión de Claude Code se fija en CI y se registra `claudeVersion`.

## Preguntas abiertas que hay que medir antes de implementar

Ninguna de estas tiene respuesta en la documentación, y cada una puede cambiar una decisión de arriba. Por eso recomiendo un spike de un día con un plugin mínimo cargado vía `--plugin-dir` antes de escribir el adaptador completo.

| # | Pregunta | Qué decide |
|---|---|---|
| 1 | ¿La app de escritorio (lanzada desde GUI) hereda el PATH del shell para el `command` MCP y los hooks en forma exec? | `vgxness` pelón vs. launcher en `scripts/` |
| 2 | ¿Qué cwd recibe el proceso stdio y qué pasa con el servidor tras `/cd` a otro proyecto (¿reinicia con el nuevo `CLAUDE_PROJECT_DIR`?)? | Si basta `--workspace` o hace falta re-resolver por llamada |
| 3 | ¿`PreToolUse` recibe `agent_type` = `vgxness:general` de forma confiable para agentes de plugin, también en background? | Si el escritor único se puede imponer mecánicamente |
| 4 | ¿Los hooks de plugin quedan retenidos hasta aceptar el trust dialog? | Comportamiento en repos recién clonados |
| 5 | ¿Claude sigue la política inyectada como system reminder con framing factual, o la trata como posible inyección? ¿Cuánto se degrada tras `compact`? | Hook vs. output style opt-in (A/B con `plugin eval`) |
| 6 | ¿Funciona `Agent(vgxness:explore, …)` en un agente de plugin usado con `--agent`? ¿Su `permissionMode` fija el modo de la sesión? | Viabilidad del Manager opt-in |
| 7 | ¿El modelo ve `structuredContent`, `content` o ambos? | Formato de respuesta de las tools |
| 8 | ¿Cuál es el nombre real del campo de instructions en el go-sdk actual? | Implementación del cambio 2 |
| 9 | ¿Qué pasa si el usuario ya tiene un `vgxness` MCP manual de una instalación previa? (¿tools duplicadas, dos procesos sobre la misma DB?) | Migración desde el setup actual |
| 10 | ¿`session-end` cabe en 1,5 s en el p99 real con SQLite en disco lento? | Si `end` debe ser solo un marcador y el resto se difiere |
| 11 | ¿`plugin eval` corre en Linux CI con solo `ANTHROPIC_API_KEY` y backend de sandbox para Bash? | Si el Verifier es evaluable en CI |
| 12 | ¿La caché preserva bits ejecutables de scripts commiteados con `+x`? | Solo si se usa el launcher |

**Versión mínima.** El piso duro es **v2.1.269**. Ahí ya existen `args` en hooks forma exec (2.1.139), `CLAUDE_PROJECT_DIR` en servidores stdio (2.1.139), nombres de agente sin `:` (2.1.218), profundidad de anidamiento 3 (2.1.219), el arreglo de timeouts de `SessionEnd` (2.1.268) y `claude plugin eval` (2.1.269) ([Hooks reference](https://code.claude.com/docs/en/hooks); [MCP](https://code.claude.com/docs/en/mcp); [Plugin evals](https://code.claude.com/docs/en/plugin-evals)). **Recomiendo declarar v2.1.284** como mínimo soportado, porque es donde `sonnet` resuelve a Sonnet 5.5 y `opus` a Opus 5.5 (los modelos con los que se calibran los roles), y porque desde v2.1.283 auto mode es el default, que es justo el entorno para el que se diseñó el read-only por `tools` ([Model config](https://code.claude.com/docs/en/model-config); [Permission modes](https://code.claude.com/docs/en/permission-modes)). Tu máquina hoy tiene 2.1.283, así que el spike debería correr después de actualizar. No hay campo `minClaudeCodeVersion` en el manifiesto, así que el chequeo vive en `vgxness setup claude-code` y en la documentación. CI fija la versión exacta probada (hoy 2.1.287). Evita campos más nuevos que el mínimo declarado (por ejemplo `userConfig.options`, que exige 2.1.271 y hace que clientes viejos no carguen el plugin) mientras no hagan falta ([Manifest reference](https://code.claude.com/docs/en/plugins/manifest-reference)).

## Conclusión

Lo que cambia en el entendimiento es dónde está el valor de VGXNESS. Durante el multi-host, buena parte del código existía para compensar huecos de cada host: instalar, versionar, aislar, trazar, evaluar. En Claude Code esos huecos ya no existen, y el producto se reduce a su núcleo defendible: **memoria de proyecto durable y sincronizable, más una política de entrega que ningún mecanismo nativo implementa**. Eso también cambia el riesgo principal. Ya no es técnico sino de autoridad: la política entra a nivel de mensaje, compite con el CLAUDE.md del usuario y con la auto memory, y su cumplimiento solo se puede demostrar con evals. Por eso `claude plugin eval` deja de ser opcional y pasa a ser la especificación ejecutable de la política.

La segunda implicación es de acoplamiento. Con prompts que nombran `mcp__plugin_vgxness_memory__*` y un adaptador que traduce el contrato de hooks de Claude Code, el plugin y el binario son una sola unidad versionada que se mueve al ritmo de un upstream que saca releases casi a diario y con muchos comportamientos atados a versión. La disciplina que más te va a ahorrar es fijar la versión en CI, subir `version` en lockstep con `claude plugin tag` y hacer que el adaptador falle siempre cerrado y en silencio. Así un cambio upstream degrada la memoria, pero nunca rompe la sesión del usuario.
