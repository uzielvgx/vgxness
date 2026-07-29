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
- manager v31, una sustitución `explore` de solo lectura, cinco revisores, seis perfiles SDD, plugin v5 y manifiesto de modelos;
- setup CLI/TUI de seis pasos con handshake delimitado de OpenCode 1.18.4+;
- reconocimiento exacto de artefactos anteriores y upgrades conservadores;
- archivos de release, checksums y workflows deterministas.

Los comandos y subsistemas de ejecución por compatibilidad no forman parte del producto entregado.

## Autoridad

El manager superior es la única autoridad para routing, síntesis, escrituras del workspace, validación, aceptación de revisiones, proyección y transiciones. Los seis perfiles SDD y los cinco revisores son de solo lectura. El plugin solo persiste memoria y SDD delimitados.

El plugin expone 18 herramientas: cinco de memoria semántica y 13 de SDD. Cada mutación SDD exige el contexto confiable de la sesión superior rastreada. El plugin no ejecuta, enruta, edita, delega, selecciona modelos, accede a archivos del workspace ni avanza el ciclo por sí mismo.

## Artefactos gestionados

La proyección contiene exactamente 15 artefactos:

| Grupo | Cantidad | Contrato |
| --- | ---: | --- |
| Manager v31 | 1 | Único escritor de workspace y ciclo. |
| Sustitución Explore | 1 | Descubrimiento CodeGraph-first, de solo lectura y denegado por defecto. |
| Revisores | 5 | Ocultos y de solo lectura. |
| Perfiles SDD | 6 | Ocultos, de solo lectura y ligados a modelos. |
| Plugin v5 | 1 | Solo almacenamiento de memoria y SDD. |
| Manifiesto de modelos | 1 | Bindings exactos no secretos. |

El plan `medium` usa slots Luna Fast, Terra y Sol. Cambiar plan o slot requiere reiniciar OpenCode. `--model` permanece como opción obsoleta sin efecto.

## Almacenamiento y SDD

La base por defecto es `~/.vgxness/memory.db`. La identidad canónica del workspace aísla proyectos. Las observaciones semánticas y FTS nunca se mezclan con cambios, revisiones, bindings, idempotencia o proyecciones SDD.

El ciclo es `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Las revisiones aceptadas son inmutables. Apply compone un patch ligado por hash; el manager lo aplica y valida. Hybrid mantiene SQLite como fuente canónica. OpenSpec queda limitado a `openspec/changes/<safe-change-id>/` y nunca importa divergencia automáticamente.

## Setup y salud

Setup explica los cambios, requiere confirmación, instala launcher y artefactos exactos, los relee y ejecuta `opencode --version` con límites dentro de un workspace absoluto existente. Saludable exige OpenCode major 1 y versión mínima 1.18.4. Setup no descarga paquetes, modifica `PATH` u `opencode.json`, inicializa CodeGraph ni afirma disponibilidad de modelos.

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
- No hay instalación automática por red.
- No existe dependencia Engram ni importación automática de bases legacy.
- El plugin no obtiene autoridad de filesystem, ejecución, routing, delegación o ciclo.
