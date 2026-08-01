# Plan maestro de producto VGXNESS

Política de entrega actual: manager v38 carga `stacked-pr` v3 global y exige un preflight limpio antes de crear la branch, escribir código o anunciar entrega. Setup retira sólo bytes exactos v1/v2/v3 del proveedor antes de publicar globalmente; son evidencia de identidad, no skills activables.

Las etiquetas de entrega son solo observables: IMPLEMENTED significa que los cambios previstos del workspace y los checks de desarrollo terminaron, sin verificación independiente; VERIFIED significa que el candidato congelado exacto superó verificación independiente y review; DELIVERED significa que el commit exacto se publicó y se creó y releyó una PR nueva de la tarea actual; MERGED significa que se releyeron el merge de esa PR y la contención en la base; INSTALLED además exige relectura de instalación y handshake. Nunca se infiere un estado posterior.

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
- manager v38, otros 14 agentes ligados a modelos, plugin v5, manifiesto de modelos, selección predeterminada, metadatos de restauración y el catálogo global separado de 16 skills;
- setup CLI/TUI de siete pasos con handshake delimitado de OpenCode 1.18.4+ y verificación de skills portables compartidas;
- reconocimiento current-only de manager y agentes, reconocimiento exacto de plugins de almacenamiento anteriores y desinstalación conservadora;
- archivos de release, checksums y workflows deterministas.

Los comandos y subsistemas de ejecución por compatibilidad no forman parte del producto entregado.

## Autoridad

Manager, `general` administrado y verifier tienen permisos globales `allow`; sus prompts conservan los roles de orquestación, implementación delegada y verificación no mutante. El manager superior sigue siendo la única autoridad para routing, síntesis, aceptación de candidatos y revisiones, y transiciones. Los seis perfiles SDD y los cinco revisores son de solo lectura. El plugin solo persiste memoria y SDD delimitados.

El plugin expone 18 herramientas: cinco de memoria semántica y 13 de SDD. Cada mutación SDD exige el contexto confiable de la sesión superior rastreada. El plugin no ejecuta, enruta, edita, delega, selecciona modelos, accede a archivos del workspace ni avanza el ciclo por sí mismo.

## Artefactos gestionados

La proyección de OpenCode contiene exactamente 19 artefactos del proveedor:

| Grupo | Cantidad | Contrato |
| --- | ---: | --- |
| Manager v38 | 1 | Única autoridad de orquestación, ciclo, Git y GitHub. |
| Sustitución Explore | 1 | Descubrimiento CodeGraph-first, de solo lectura y denegado por defecto. |
| General y verificador | 2 | `general`: implementación delegada y único escritor del workspace; verificador: validación final independiente y no mutante. |
| Revisores | 5 | Ocultos y de solo lectura. |
| Perfiles SDD | 6 | Ocultos, de solo lectura y ligados a modelos. |
| Plugin v5 | 1 | Solo almacenamiento de memoria y SDD. |
| Manifiesto de modelos | 1 | Bindings exactos no secretos. |
| Selección de agente predeterminado | 1 | Una fusión semántica establece `default_agent="vgxness-manager"` en `opencode.json` y conserva valores JSON no relacionados; conserva intactos los bytes de cualquier `opencode.jsonc` existente. |
| Metadatos de restauración | 1 | El registro acotado `<config-dir>/vgxness/default-agent.json` indica si `opencode.json` existía y cualquier valor predeterminado explícito anterior, para restaurarlo o eliminar una configuración creada por setup durante la desinstalación. |

El plan `medium` usa slots Luna Fast, Terra y Sol. Cambiar plan o slot requiere reiniciar OpenCode. `--model` permanece como opción obsoleta sin efecto.

El catálogo portable global separado contiene 40 archivos entre 16 skills: `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs` y `release-lifecycle-docs`. `vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` lo administra por defecto en `~/.agents/skills`; setup retira de forma transaccional sólo bytes exactos v1/v2/v3 de `vgxness-autonomous-stacked-pr` antes de publicar `stacked-pr`. Bytes modificados o desconocidos bloquean sin mutación. La desinstalación de OpenCode no posee ni elimina skills globales.

Las implementaciones elegibles usan por defecto un objetivo de 400 líneas efectivas por slice y solo apilan por encima de 800. Cada PR apilada apunta a la misma base original inspeccionada, mantiene ascendencia de commits de padre inmediato y registra `Depends-On`; los merge commits conservan los commits predecesores para que los diffs posteriores se reduzcan al aterrizar los slices anteriores. Manager v38 completa su pre-write gate limpio (identidad del repositorio, paths previstos, estimación/plan de slice y branch fresca) antes de escrituras o del anuncio rutinario de entrega. Tras freeze, verificación y review, puede crear branches nuevas normalizadas, commits normales, primeros pushes y PR no draft sin una segunda aprobación rutinaria. Solo puede hacer merge de PR creadas por la misma tarea elegible actual, en orden ordinal y mediante el método de merge commit permitido por el repositorio, con binding del `owner/repo` verificado y el OID completo exacto de la cabeza. Cada slice tiene un OID esperado de punta de base obtenido de una relectura fresca de la base original antes de los checks; después de cada merge predecesor avanza con una relectura fresca, y la base de la PR y la base remota viva deben coincidir antes de los checks e inmediatamente antes del merge. `no merge` se propaga de forma transitiva; `local-only`, `no commit`, `no push` y `no PR` también prohíben merge. El estado sucio detiene mutaciones, salvo la recuperación exacta, limitada y reautorizada explícitamente de un slice local no publicado y verificado. Las branches remotas y PR existentes permanecen de solo lectura y nunca obtienen autoridad retroactiva de merge o cleanup; solo se permite esa recuperación limitada del slice local no publicado. Después de merges verificados y un worktree limpio, puede avanzar la base original exclusivamente con fast-forward desde la base remote-tracking verificada. Salvo `no cleanup`, solo puede eliminar branches locales exactas de la entrega actual ya fusionadas y sin una PR abierta dependiente; las branches remotas de entrega se conservan intactas. Cualquier check, merge, host, auth, protección, topología, remoto o worktree fallido o ambiguo detiene nuevas mutaciones. Los globs de OpenCode expresan política estática; no prueban semántica argv ni comportamiento externo de Git o GitHub.

## Almacenamiento y SDD

La base por defecto es `~/.vgxness/memory.db`. La identidad canónica del workspace aísla proyectos. Las observaciones semánticas y FTS nunca se mezclan con cambios, revisiones, bindings, idempotencia o proyecciones SDD.

El ciclo es `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Las revisiones aceptadas son inmutables. Apply compone un patch ligado por hash; el manager lo aplica y valida. Hybrid mantiene SQLite como fuente canónica. OpenSpec queda limitado a `openspec/changes/<safe-change-id>/` y nunca importa divergencia automáticamente.

## Setup y salud

Setup explica los cambios, requiere confirmación, instala launcher y 19 artefactos exactos de OpenCode, retira bytes exactos v1/v2/v3 del proveedor y luego publica el catálogo global de 40 archivos y 16 skills: `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs` y `release-lifecycle-docs`. Bytes desconocidos o modificados bloquean. La desinstalación de OpenCode no elimina skills globales.

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

VGXNESS administra una sola copia global del catálogo de 40 archivos y 16 skills (`skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`) en `~/.agents/skills` mediante `vgxness skills`; `--skills-dir` permite un destino absoluto aislado para pruebas. Las skills portables se comparten entre hosts compatibles, mientras que las skills específicas permanecen en cada integración. Desinstalar la integración de OpenCode no elimina el paquete global. Un paquete parcial con sólo bytes exactos deseados o predecesores se puede reanudar o desinstalar de forma segura; bytes desconocidos bloquean por drift. En Windows se usan rename atómico, relectura y backups, pero no existe fsync de directorios y la durabilidad ante caídas es menor.
