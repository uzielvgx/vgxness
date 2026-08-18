# Plan maestro de producto

## Límite actual del producto

VGXNESS es un manager nativo de OpenCode con memoria local SQLite/FTS5 y almacenamiento SDD estructurado. OpenCode conserva la ejecución de ingeniería. La proyección actual solo MCP contiene exactamente 18 artefactos del proveedor: 15 agentes administrados, un manifiesto de plan de modelos, la selección predeterminada en `opencode.json` y metadatos de restauración. No se instala plugin.

OpenCode administrado v49 y Codex generado v9 usan `vgxness mcp --full`, que expone cinco herramientas de memoria y 13 de SDD. Su contrato de prompt compartido selecciona silenciosamente la ruta de menor costo sin herramientas de clasificación: conversación, escritura, traducción, resumen, brainstorming y planificación sin efectos usan una ruta rápida sin herramientas de ejecución; las lecturas exactas delimitadas permiten como máximo tres intentos sin delegación ni listas de tareas; la investigación compleja de evidencia puede usar una delegación de solo lectura. Los intentos fallidos y reintentos cuentan y la ejecución se detiene antes de agotar el presupuesto. Es política de prompt, no enforcement de runtime.

El recall sigue activado por intención: busca primero todos los términos, reintenta con cualquier término solo cuando hace falta, obtiene IDs exactos después del preview y usa memoria reciente únicamente para solicitudes explícitas de trabajo reciente, sesión o recuperación de compactación. De forma ortogonal, después de cualquier ruta el prompt permite como máximo un guardado autónomo solo para decisiones, preferencias, restricciones o aprendizajes del proyecto que sean duraderos, respaldados por evidencia y evaluados como seguros; excluye estado transitorio, logs, secretos y datos personales, no añade ceremonia de ingeniería ni sincronización automática con la nube. MCP no tiene identidad del llamador. No se afirma evidencia externa, NLP ni de holdout.

La política actual de entrega usa manager v49 con `stacked-pr` v3 global. El manager exige su pre-write gate limpio antes de crear ramas, escribir código o anunciar entregas rutinarias. `general` administrado v6, verifier v4 y los perfiles reviewer v3 son actuales.

## Proyección administrada

| Artefacto | Cantidad | Responsabilidad |
| --- | ---: | --- |
| Manager v49 | 1 | Routing general adaptativo y autoridad de ingeniería y ciclo cuando se activa. |
| General v6 y verificador v4 | 2 | Implementación delegada y validación independiente no mutante. |
| Perfiles reviewer v3 | 5 | Revisión especializada de solo lectura. |
| Perfiles explore y SDD | 7 | Investigación y fases SDD de solo lectura. |
| Manifiesto de plan de modelos | 1 | Bindings de modelos resueltos. |
| Selección de agente predeterminado | 1 | Selección del manager predeterminado. |
| Metadatos de restauración | 1 | Estado de restauración del agente previo. |

El catálogo portable global separado contiene 47 archivos y 19 skills, incluidos `memory-sync` y `sdd-lifecycle`; no es artefacto de OpenCode ni destino de desinstalación.

## Memoria y SDD

La base SQLite administrada aísla la memoria semántica de cambios SDD, artefactos, revisiones, bindings, idempotencia y proyecciones. SDD admite backends `memory`, `openspec` e `hybrid` y modos `automatic` e `interactive`. La proyección OpenSpec usa paths delimitados y nunca importa automáticamente bytes divergentes del repositorio.

Las operaciones MCP no enrutan trabajo, invocan agentes, acceden archivos del workspace, ejecutan shell, seleccionan modelos, editan, delegan ni avanzan un ciclo de forma independiente. La memoria es contexto no confiable y nunca prueba un candidato.

## Setup y retiro

Setup previsualiza cambios, exige confirmación, instala el launcher y 18 artefactos exactos de OpenCode, configura `vgxness mcp --full` y publica el catálogo global. El Manager v48 de OpenCode y el manager v8 de Codex se conservan únicamente como predecesores históricos exactos reconocidos, junto con identidades anteriores compatibles. Reconoce y retira únicamente bytes históricos exactos del plugin `vgxness.ts` v1-v10 y del provider skill `vgxness-autonomous-stacked-pr` v1/v2/v3. Bytes modificados, malformados, extranjeros, desconocidos o más nuevos bloquean sin eliminación. La desinstalación de OpenCode no elimina skills globales.

## No objetivos

- No hay plugin instalado, superficie de hooks, inyección automática de memoria, compactación, observabilidad ni identidad de sesión de plugin.
- No se instalan hooks shell o Git.
- MCP no obtiene autoridad de filesystem, ejecución, routing, delegación o ciclo.
- No hay instalación automática de red/paquetes ni importación de bases de datos antiguas.
