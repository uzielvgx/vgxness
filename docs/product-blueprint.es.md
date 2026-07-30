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
- manager v35, otros 14 agentes ligados a modelos, plugin v5, manifiesto de modelos y un skill independiente de PR apiladas autónomas;
- setup CLI/TUI de seis pasos con handshake delimitado de OpenCode 1.18.4+;
- reconocimiento current-only de manager y agentes, reconocimiento exacto de plugins de almacenamiento anteriores y desinstalación conservadora;
- archivos de release, checksums y workflows deterministas.

Los comandos y subsistemas de ejecución por compatibilidad no forman parte del producto entregado.

## Autoridad

Manager, `general` administrado y verifier tienen permisos globales `allow`; sus prompts conservan los roles de orquestación, implementación delegada y verificación no mutante. El manager superior sigue siendo la única autoridad para routing, síntesis, aceptación de candidatos y revisiones, y transiciones. Los seis perfiles SDD y los cinco revisores son de solo lectura. El plugin solo persiste memoria y SDD delimitados.

El plugin expone 18 herramientas: cinco de memoria semántica y 13 de SDD. Cada mutación SDD exige el contexto confiable de la sesión superior rastreada. El plugin no ejecuta, enruta, edita, delega, selecciona modelos, accede a archivos del workspace ni avanza el ciclo por sí mismo.

## Artefactos gestionados

La proyección contiene exactamente 19 artefactos:

| Grupo | Cantidad | Contrato |
| --- | ---: | --- |
| Manager v35 | 1 | Única autoridad de orquestación, ciclo, Git y GitHub. |
| Sustitución Explore | 1 | Descubrimiento CodeGraph-first, de solo lectura y denegado por defecto. |
| General y verificador | 2 | Único escritor del workspace y validación final independiente. |
| Revisores | 5 | Ocultos y de solo lectura. |
| Perfiles SDD | 6 | Ocultos, de solo lectura y ligados a modelos. |
| Plugin v5 | 1 | Solo almacenamiento de memoria y SDD. |
| Manifiesto de modelos | 1 | Bindings exactos no secretos. |
| Overlay de agente predeterminado | 1 | Selector exacto `opencode.jsonc` para `vgxness-manager`; el `opencode.json` del usuario no se modifica. |
| Skill de PR apiladas | 1 | Política nativa estática e independiente del plan de modelos. |

El plan `medium` usa slots Luna Fast, Terra y Sol. Cambiar plan o slot requiere reiniciar OpenCode. `--model` permanece como opción obsoleta sin efecto.

Las implementaciones elegibles usan por defecto un objetivo de 400 líneas efectivas por slice y solo apilan por encima de 800. Tras freeze, verificación y review, manager v35 puede crear branches nuevas normalizadas, commits normales, primeros pushes y PR no draft sin una segunda aprobación rutinaria. Las restricciones explícitas se propagan de forma transitiva. El estado de entrega existente solo se reanuda en modo lectura, nunca hay cleanup automático y siguen sin soporte las mutaciones posteriores de PR, worktrees, historia, merge, force, releases, credenciales o configuración. Los globs de OpenCode expresan política estática; no prueban semántica argv ni comportamiento externo de Git o GitHub.

## Almacenamiento y SDD

La base por defecto es `~/.vgxness/memory.db`. La identidad canónica del workspace aísla proyectos. Las observaciones semánticas y FTS nunca se mezclan con cambios, revisiones, bindings, idempotencia o proyecciones SDD.

El ciclo es `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Las revisiones aceptadas son inmutables. Apply compone un patch ligado por hash; el manager lo aplica y valida. Hybrid mantiene SQLite como fuente canónica. OpenSpec queda limitado a `openspec/changes/<safe-change-id>/` y nunca importa divergencia automáticamente.

## Setup y salud

Setup explica los cambios, requiere confirmación, instala launcher y artefactos exactos, los relee y ejecuta `opencode --version` con límites dentro de un workspace absoluto existente. Saludable exige OpenCode major 1 y versión mínima 1.18.4. Setup no descarga paquetes, modifica `PATH` ni el `opencode.json` del usuario, inicializa CodeGraph ni afirma disponibilidad de modelos. Instala un overlay `opencode.jsonc` exacto para que OpenCode seleccione `vgxness-manager` por defecto y rechaza contenido ajeno en esa ruta administrada.

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
