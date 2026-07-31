# Plan maestro de producto VGXNESS

Este documento en español acompaña al [blueprint canónico en inglés](product-blueprint.md). En caso de conflicto, controla la versión inglesa.

## Definición del producto

VGXNESS es un manager nativo de OpenCode con almacenamiento local y herramientas deterministas de instalación. OpenCode conserva la ejecución de ingeniería. El manager elige trabajo directo, delegación nativa de solo lectura o SDD estructurado opcional.

## Producto entregado

- binarios `vgxness` y `vgxness-release`;
- launcher permanente con versiones SHA-256 inmutables, activación atómica y rollback;
- `status` y `doctor` de solo lectura para raíz, base de datos y esquema;
- CLI de memoria y SDD, TUI de memoria y setup;
- SQLite/FTS5 esquema v5 con dominios semántico y SDD aislados;
- backends SDD `memory`, `openspec` e `hybrid`;
- modos SDD `automatic` e `interactive` por cambio;
- manager v36, otros 14 agentes ligados a modelos, plugin v5, manifiesto de modelos y un skill independiente de PR apiladas autónomas;
- setup CLI/TUI de siete pasos con handshake delimitado de OpenCode 1.18.4+ y verificación de skills portables compartidas;
- reconocimiento current-only de manager y agentes, reconocimiento exacto de plugins de almacenamiento anteriores y desinstalación conservadora;
- archivos de release, checksums y workflows deterministas.

Los comandos y subsistemas de ejecución por compatibilidad no forman parte del producto entregado.

## Autoridad

Manager, `general` administrado y verifier tienen permisos globales `allow`; sus prompts conservan los roles de orquestación, implementación delegada y verificación no mutante. El manager superior sigue siendo la única autoridad para routing, síntesis, aceptación de candidatos y revisiones, y transiciones. Los seis perfiles SDD y los cinco revisores son de solo lectura. El plugin solo persiste memoria y SDD delimitados.

El plugin expone 18 herramientas: cinco de memoria semántica y 13 de SDD. Cada mutación SDD exige el contexto confiable de la sesión superior rastreada. El plugin no ejecuta, enruta, edita, delega, selecciona modelos, accede a archivos del workspace ni avanza el ciclo por sí mismo.

## Artefactos gestionados

La proyección contiene exactamente 20 artefactos:

| Grupo | Cantidad | Contrato |
| --- | ---: | --- |
| Manager v36 | 1 | Única autoridad de orquestación, ciclo, Git y GitHub. |
| Sustitución Explore | 1 | Descubrimiento CodeGraph-first, de solo lectura y denegado por defecto. |
| General y verificador | 2 | `general`: implementación delegada y único escritor del workspace; verificador: validación final independiente y no mutante. |
| Revisores | 5 | Ocultos y de solo lectura. |
| Perfiles SDD | 6 | Ocultos, de solo lectura y ligados a modelos. |
| Plugin v5 | 1 | Solo almacenamiento de memoria y SDD. |
| Manifiesto de modelos | 1 | Bindings exactos no secretos. |
| Selección de agente predeterminado | 1 | Una fusión semántica establece `default_agent="vgxness-manager"` en `opencode.json` y conserva valores JSON no relacionados; conserva intactos los bytes de cualquier `opencode.jsonc` existente. |
| Metadatos de restauración | 1 | El registro acotado `<config-dir>/vgxness/default-agent.json` indica si `opencode.json` existía y cualquier valor predeterminado explícito anterior, para restaurarlo o eliminar una configuración creada por setup durante la desinstalación. |
| Skill de PR apiladas | 1 | Política nativa estática e independiente del plan de modelos. |

El plan `medium` usa slots Luna Fast, Terra y Sol. Cambiar plan o slot requiere reiniciar OpenCode. `--model` permanece como opción obsoleta sin efecto.

El paquete portable global separado contiene exactamente 15 archivos de `agent-skill-engineer`. `vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` lo administra por defecto en `~/.agents/skills` y setup lo instala automáticamente. Su propiedad es independiente: la skill de PR apiladas de OpenCode sigue siendo del proveedor y la desinstalación de OpenCode nunca elimina el paquete global.

Las implementaciones elegibles usan por defecto un objetivo de 400 líneas efectivas por slice y solo apilan por encima de 800. Cada PR apilada apunta a la misma base original inspeccionada, mantiene ascendencia de commits de padre inmediato y registra `Depends-On`; los merge commits conservan los commits predecesores para que los diffs posteriores se reduzcan al aterrizar los slices anteriores. Tras freeze, verificación y review, manager v36 puede crear branches nuevas normalizadas, commits normales, primeros pushes y PR no draft sin una segunda aprobación rutinaria. Solo puede hacer merge de PR creadas por la misma tarea elegible actual, en orden ordinal y mediante el método de merge commit permitido por el repositorio, con binding del `owner/repo` verificado y el OID completo exacto de la cabeza. Cada slice tiene un OID esperado de punta de base obtenido de una relectura fresca de la base original antes de los checks; después de cada merge predecesor avanza con una relectura fresca, y la base de la PR y la base remota viva deben coincidir antes de los checks e inmediatamente antes del merge. `no merge` se propaga de forma transitiva; `local-only`, `no commit`, `no push` y `no PR` también prohíben merge. Después de merges verificados y un worktree limpio, puede avanzar la base original exclusivamente con fast-forward desde la base remote-tracking verificada. Salvo `no cleanup`, solo puede eliminar branches locales exactas de la entrega actual ya fusionadas y sin una PR abierta dependiente; las branches remotas de entrega se conservan intactas. El estado de entrega existente permanece en modo lectura; cualquier check, merge, host, auth, protección, topología, remoto o worktree fallido o ambiguo detiene nuevas mutaciones. Los globs de OpenCode expresan política estática; no prueban semántica argv ni comportamiento externo de Git o GitHub.

## Almacenamiento y SDD

La base por defecto es `~/.vgxness/memory.db`. La identidad canónica del workspace aísla proyectos. Las observaciones semánticas y FTS nunca se mezclan con cambios, revisiones, bindings, idempotencia o proyecciones SDD.

El ciclo es `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Las revisiones aceptadas son inmutables. Apply compone un patch ligado por hash; el manager lo aplica y valida. Hybrid mantiene SQLite como fuente canónica. OpenSpec queda limitado a `openspec/changes/<safe-change-id>/` y nunca importa divergencia automáticamente.

## Setup y salud

Setup explica los cambios, requiere confirmación, instala launcher, paquete portable global y artefactos exactos de OpenCode, los relee y ejecuta `opencode --version` con límites dentro de un workspace absoluto existente. Saludable exige OpenCode major 1 y versión mínima 1.18.4, los 20 artefactos del proveedor y el paquete global independiente de 15 archivos. `--skills-dir` selecciona un destino absoluto de skills portables. Setup no descarga paquetes, modifica `PATH`, inicializa CodeGraph ni afirma disponibilidad de modelos. Usa una fusión semántica para seleccionar `vgxness-manager` mediante `opencode.json` y conservar valores JSON no relacionados; conserva intactos los bytes de cualquier `opencode.jsonc` existente. El registro acotado `<config-dir>/vgxness/default-agent.json` indica si `opencode.json` existía y cualquier valor predeterminado explícito anterior, para restaurarlo o eliminar una configuración creada por setup durante la desinstalación.

## Principios

1. Mantener una sola autoridad de ejecución.
2. Mantener hijos de solo lectura y sin delegación.
3. Aislar persistencia por proyecto.
4. Tratar memoria como contexto no confiable, nunca como prueba.
5. Exigir autoridad explícita para efectos riesgosos.
6. Preservar identidad exacta de artefactos y datos del usuario.
7. Cerrarse ante drift, estado obsoleto, evidencia faltante o contexto inválido.
8. Mantener CLI y TUI como adaptadores sobre servicios compartidos.

## No objetivos

- No existe puente de ejecución alternativo ni scheduler Go.
- No se instalan hooks shell o Git.
- No existe stack engine Go, writer de worktrees, estado persistente de entrega, herramienta Git/GitHub personalizada ni prueba de entrega por red.
- No hay instalación automática por red.
- No existe dependencia Engram ni importación automática de bases legacy.
- El plugin no obtiene autoridad de filesystem, ejecución, routing, delegación o ciclo.
# Skills portables globales

VGXNESS administra `agent-skill-engineer` una sola vez en `~/.agents/skills` mediante `vgxness skills`; `--skills-dir` permite un destino absoluto aislado para pruebas. Las skills portables se comparten entre hosts compatibles, mientras que las skills específicas permanecen en cada integración. Desinstalar la integración de OpenCode no elimina el paquete global. Un paquete parcial con sólo bytes exactos deseados o predecesores se puede reanudar o desinstalar de forma segura; bytes desconocidos bloquean por drift. En Windows se usan rename atómico, relectura y backups, pero no existe fsync de directorios y la durabilidad ante caídas es menor.
