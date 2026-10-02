# VGXNESS

VGXNESS le da memoria durable a tus proyectos en Claude Code. Es un plugin de
Claude Code más un binario en Go (`vgxness`) que guarda la memoria de cada
proyecto en SQLite/FTS5 (schema v23), entrega un handoff entre sesiones, aplica
una política de trabajo para el hilo principal y cuatro subagentes, y opcionalmente
sincroniza la memoria entre tus dispositivos a través de `vgxness-syncd`.

No copia transcripciones, no guarda secretos y no toma decisiones destructivas
por ti: la memoria la escribe Claude a través de MCP, y tú decides qué se borra.

## Requisitos

- Claude Code **2.1.284 o superior** (en CI se prueba con 2.1.287).
- El binario `vgxness` en el `PATH` del usuario. Claude Code lo lanza como
  servidor MCP y como comando de hooks; si no lo encuentra, el plugin carga
  pero sin memoria.
- Para compilar desde el código fuente: Go 1.26.

## Instalación

### 1. El binario

macOS y Linux, con el tap oficial de Homebrew:

```sh
brew install uzielvgx/tap/vgxness
```

Windows, con el bucket oficial de Scoop:

```powershell
scoop bucket add vgxness https://github.com/uzielvgx/scoop-bucket
scoop install vgxness/vgxness
```

También puedes bajar el archivo de tu plataforma desde
[Releases](https://github.com/uzielvgx/vgxness/releases) (`vgxness_<versión>_<os>_<arch>.tar.gz`
o `.zip`), verificarlo contra `SHA256SUMS` y dejar el ejecutable en tu `PATH`,
o compilarlo tú mismo:

```sh
go install github.com/uzielvgx/vgxness/cmd/vgxness@latest
```

Comprueba que Claude Code lo va a encontrar:

```sh
vgxness version
```

### 2. El plugin

El repositorio es a la vez el marketplace del plugin:

```sh
claude plugin marketplace add uzielvgx/vgxness
claude plugin install vgxness@vgxness
```

Para desarrollar sobre un clon local, carga el plugin sin instalarlo:

```sh
claude --plugin-dir ./plugins/vgxness
```

### 3. Permisos

Un plugin no puede traer reglas de permisos, así que `vgxness` te imprime las
recomendadas (lectura y guardado permitidos; actualizar y olvidar preguntan):

```sh
vgxness claude-code setup
```

Copia el bloque `permissions` a tu `~/.claude/settings.json` o al
`.claude/settings.json` del proyecto.

## Qué hace el plugin

| Pieza | Qué aporta |
| --- | --- |
| Servidor MCP `memory` | Ocho herramientas (`memory_search`, `memory_get`, `memory_recent`, `memory_context`, `memory_save`, `memory_update`, `memory_session_summary`, `memory_forget`) sobre la memoria del proyecto. Las instrucciones del servidor le dicen a Claude cuándo buscar y cuándo guardar. |
| Hook `SessionStart` | Inyecta la política del Manager y el handoff que dejó la sesión anterior del mismo proyecto, como datos no confiables. Se repite tras cada compactación. |
| Hooks `PreCompact` y `SessionEnd` | Renuevan la sesión y, si Claude escribió un resumen, lo cierran como handoff para la siguiente sesión. Nunca bloquean. |
| Subagentes `vgxness:explore`, `vgxness:general`, `vgxness:verifier`, `vgxness:reviewer` | Exploración de solo lectura, el único rol que edita archivos, verificación independiente de un candidato congelado y revisión CARE. Los roles de solo lectura no tienen herramientas de escritura. |
| Skill `/vgxness:git-delivery` | Entrega de PRs con `git` y `gh`. Solo la invocas tú; Claude no la dispara sola. |

La política completa está en [`plugins/vgxness/policy/manager.md`](plugins/vgxness/policy/manager.md)
y el detalle de cada pieza en [docs/claude-code.md](docs/claude-code.md).

### Convivencia con la memoria automática de Claude Code

Claude Code tiene su propia memoria automática (`MEMORY.md` por proyecto). VGXNESS
guarda el estado del proyecto (decisiones, restricciones, causas raíz, handoffs)
y la política le pide a Claude no duplicarlo ahí. Si prefieres un solo
almacén, desactiva la automática en tu `settings.json`:

```json
{ "autoMemoryEnabled": false }
```

## Memoria y sincronización

La base de datos vive en `~/.vgxness/memory.db` (nunca dentro de los datos del
plugin, que Claude Code borra al desinstalarlo). Cada workspace se identifica de
forma canónica, así que dos proyectos con el mismo nombre no se mezclan.

- [Memoria nativa y almacenamiento](docs/memory.md): dominios del schema v23,
  sesiones y handoffs, migraciones y cómo actualizar sin perder datos.
- [Servicio de sincronización](docs/sync.md): `vgxness-syncd`, enrolamiento de
  dispositivos, `reseed` y `rejoin`, límites de admisión y despliegue.

## Diagnóstico

```sh
vgxness status
vgxness doctor
```

`vgxness status` y `vgxness doctor` son de solo lectura e informan la raíz de
almacenamiento, la base de datos y la versión del schema. Ver [diagnóstico](docs/diagnostics.md) para los
códigos de salida.

## Consola

`vgxness tui` abre la consola en la terminal. Hoy es un cascarón: el diseño de
sus módulos (Inicio, Setup del plugin, Memoria, Sync, Doctor) está en el canvas
enlazado desde [`DESIGN.md`](DESIGN.md) y se construye en la tarea 17 del plan.

## Desarrollo

```sh
make fast          # gofmt + go test -short ./...
make verify        # suite completa, race, vet, tidy, builds de Windows, e2e
make plugin-check  # claude plugin validate del marketplace y del plugin
make vuln          # govulncheck (necesita red)
```

El plan activo es [`docs/plans/claude-only.md`](docs/plans/claude-only.md); la
investigación que lo respalda está en [`docs/research/`](docs/research/). Las
decisiones que importan después se registran en [`docs/decisions.md`](docs/decisions.md).

## Licencia

MIT. Ver [LICENSE](LICENSE).
