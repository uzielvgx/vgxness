# Plan maestro de producto

## Límite actual del producto

VGXNESS es un manager nativo de OpenCode con memoria local SQLite/FTS5 y almacenamiento SDD estructurado. OpenCode conserva la ejecución de ingeniería. La proyección actual de OpenCode contiene exactamente 17 artefactos del proveedor: 13 agentes administrados, el plugin de ciclo de vida auto-descubierto `plugins/vgxness-memory-lifecycle.ts`, un manifiesto de plan de modelos, la selección predeterminada en `opencode.json` y metadatos de restauración. El plugin no tiene entrada `plugin` en `opencode.json`. La proyección generada separada de Codex contiene 15 artefactos: `AGENTS.md`, 12 perfiles delegados, un manifiesto de marketplace y `.codex-plugin/plugin.json`.

OpenCode administrado CARE-v2 Manager60 y Codex generado Manager19 (paridad OpenCode-v60) usan `vgxness mcp --full`, que expone ocho herramientas de memoria y 13 de SDD. Su contrato de prompt compartido da al trabajo no trivial un Execution Brief conciso, actualizaciones en hitos significativos y un cierre con resultado, evidencia, limitaciones y un concepto reutilizable, con profundidad guiada por defecto o concisa, mentor o experta según la señal del usuario, sin narrar cada llamada de herramienta ni razonamiento privado. Selecciona silenciosamente la ruta de menor costo sin herramientas de clasificación: conversación, escritura, traducción, resumen, brainstorming y planificación sin efectos usan una ruta rápida sin herramientas de ejecución; las lecturas exactas delimitadas permiten como máximo tres intentos sin delegación ni listas de tareas; la investigación compleja de evidencia puede usar una delegación de solo lectura. Los intentos fallidos y reintentos cuentan y la ejecución se detiene antes de agotar el presupuesto. Es política de prompt, no enforcement de runtime.

El recall sigue activado por intención: busca primero todos los términos, reintenta con cualquier término solo cuando hace falta, obtiene IDs exactos después del preview y usa memoria reciente únicamente para solicitudes explícitas de trabajo reciente, sesión o recuperación de compactación. De forma ortogonal, después de cualquier ruta el prompt permite como máximo un guardado autónomo solo para decisiones, preferencias, restricciones o aprendizajes del proyecto que sean duraderos, respaldados por evidencia y evaluados como seguros; excluye estado transitorio, logs, secretos y datos personales, no añade ceremonia de ingeniería ni sincronización automática con la nube. MCP no tiene identidad del llamador. No se afirma evidencia externa, NLP ni de holdout.

La política actual de entrega usa manager v60 con `git-delivery` v1 global, migración exacta de `stacked-pr` v3. El manager exige su pre-write gate limpio antes de crear ramas, escribir código o anunciar entregas rutinarias. La matriz CARE actual incluye reviewer, specialist y challenger, junto con el plan V3 por agente de 13 filas.

## Inventario de capacidades

| Capacidad | Implementación y estado | Límite o vacío de evidencia | Fuente y detalle |
| --- | --- | --- | --- |
| CLI, TUI e inspección | Comandos locales implementados, UI orientada al teclado e inspección de estado/doctor de solo lectura. | La inspección no repara ni migra almacenamiento. | [`internal/cli`](../internal/cli), [`internal/tui`](../internal/tui), [`internal/inspection`](../internal/inspection); [arquitectura Go](go-implementation.md). |
| Memoria local y aislamiento de proyecto | SQLite/FTS5 schema v23 implementado con identidad canónica de workspace y dominios separados de memoria semántica y SDD. | Las bases de datos antiguas se retienen; el inicio normal no las importa. | [`internal/memory`](../internal/memory); [memoria](memory.md). |
| Cliente de sincronización | Cliente por proyecto y flujo de enrollment implementados. | El cliente requiere un endpoint HTTPS; la conectividad real y el despliegue remoto no quedan probados por esta documentación local. | [`internal/syncclient`](../internal/syncclient), [`internal/app/runtime`](../internal/app/runtime); [sync](sync.md). |
| Daemon de sync, PostgreSQL y administración | `vgxness-syncd` opcional, HTTP/API, repositorio/migraciones PostgreSQL y superficie administrativa implementados. | El daemon escucha en loopback y no tiene TLS nativo; la operación remota necesita un terminador TLS externo. | [`cmd/vgxness-syncd`](../cmd/vgxness-syncd), [`internal/syncapi`](../internal/syncapi), [`internal/syncpg`](../internal/syncpg), [`internal/syncadmin`](../internal/syncadmin), [`internal/syncservice`](../internal/syncservice); [sync](sync.md). |
| Almacenamiento SDD y OpenSpec | Revisiones SDD estructuradas, bindings, idempotencia y render/compare determinista de OpenSpec implementados para `memory`, `openspec` e `hybrid`. | Los bytes divergentes del repositorio se informan; nunca se importan automáticamente. | [`internal/sdd`](../internal/sdd); [flujo de orquestación](orchestration-flow.md). |
| Integración OpenCode | Proyección administrada de 17 artefactos, handshake de setup, plugin de ciclo de vida y generación de plan de modelos implementados. | Los contratos de prompt y allowlists del host son política/evidencia, no enforcement de ejecución en runtime. | [`internal/providers/opencode`](../internal/providers/opencode); [integración OpenCode](opencode-integration.md). |
| Integración Codex | Proyección de 15 artefactos, paquete local de marketplace/plugin y render de planes implementados, preservando `config.toml` del usuario. | La activación no prueba un handshake de runtime, identidad de sesión ni conectividad MCP. | [`internal/providers/codex`](../internal/providers/codex); [integración Codex](codex-integration.md). |
| Routing de agentes y CARE | Política de manager/perfiles renderizada por proveedor más tipos puros de orquestación, CARE y evidencia de readiness implementados. | Estos tipos y prompts no ejecutan trabajo ni hacen cumplir autorización en runtime; no se afirma evaluación externa. | [`internal/orchestration`](../internal/orchestration); [CARE](care.md), [flujo de orquestación](orchestration-flow.md). |
| Setup, self-installation y backup | Setup con confirmación, activación/rollback de launcher y self-install, y soporte de backup/recovery de OpenCode implementados. | El soporte de plataformas y las salvedades de firma/notarización son los documentados para releases; no se afirma garantía universal de releases. | [`internal/setup`](../internal/setup), [`internal/selfinstall`](../internal/selfinstall), [`internal/opencodebackup`](../internal/opencodebackup); [self-install](self-install.md), [releases](release.md). |
| Skills portables | Ciclo de vida independiente del catálogo global de 19 skills y 47 archivos implementado. | Es independiente de artefactos del proveedor y OpenCode uninstall no lo elimina. | [`internal/skills`](../internal/skills); [integración OpenCode](opencode-integration.md). |
| Tests y releases | Tests del repositorio, herramientas de archivos/checksums deterministas y soporte del flujo de release implementados. | Las comprobaciones del repositorio son evidencia estática/local; los documentos de release indican límites de plataformas y firmas. | [`internal/e2e`](../internal/e2e), [`internal/release`](../internal/release), [`cmd/vgxness-release`](../cmd/vgxness-release); [releases](release.md). |

El código de implementación conductual es la fuente autorizada para el comportamiento; este blueprint posee el inventario actual y los documentos especializados poseen el detalle operativo y de interfaces. Actualiza este inventario cuando cambie un conteo de artefactos, schema, interfaz, límite de propiedad o capacidad. Las evaluaciones históricas y registros de predecesores siguen siendo evidencia histórica, no afirmaciones de capacidad actual. Este documento es no canónico; el inglés controla los conflictos.

## Proyección administrada

| Artefacto | Cantidad | Responsabilidad |
| --- | ---: | --- |
| Manager v60 | 1 | Routing general adaptativo y autoridad del ciclo SDD cuando se activa; no escribe el workspace SDD. |
| Explore, General, SDD apply y verifier | 4 | Explore es solo lectura, General implementa trabajo autorizado no SDD, SDD apply escribe exclusivamente workspace/proyecciones SDD aceptados y verifier no muta. |
| CARE reviewer, specialist y challenger | 3 | Revisión de aseguramiento de solo lectura; no hay aliases fixed-lens actuales. |
| Cinco perfiles de fase SDD de solo lectura | 5 | Research, proposal, spec, design y tasks. |
| Plugin de ciclo de vida | 1 | Inicio superior, un handoff aislado acotado, checkpoint de compactación sin transcripción, observación exacta del resumen y fin completado/interrumpido. |
| Manifiesto de plan de modelos | 1 | Bindings de modelos resueltos. |
| Selección de agente predeterminado | 1 | Selección del manager predeterminado. |
| Metadatos de restauración | 1 | Estado de restauración del agente previo. |

Los 15 artefactos de Codex se renderizan en [`internal/providers/codex/render.go`](../internal/providers/codex/render.go): un archivo de manager, 12 perfiles delegados y los dos manifiestos del paquete de plugin. Los comandos de activación del proveedor pueden agregar el marketplace local y el plugin, pero su estado observado no prueba un handshake de runtime de Codex, identidad de sesión, conectividad MCP ni ejecución de prompts.

La documentación histórica de predecesores puede referirse a Manager v49, General v6 y verificador v4, o reviewer v3; esas identidades no describen el límite de propiedad generado actual.

El catálogo portable global separado contiene 47 archivos y 19 skills, incluidos `memory-sync` y `sdd-lifecycle`; no es artefacto de OpenCode ni destino de desinstalación.

## Memoria y SDD

La base SQLite administrada aísla la memoria semántica de cambios SDD, artefactos, revisiones, bindings, idempotencia y proyecciones. SDD admite backends `memory`, `openspec` e `hybrid` y modos `automatic` e `interactive`. La proyección OpenSpec usa paths delimitados y nunca importa automáticamente bytes divergentes del repositorio.

Las operaciones MCP no enrutan trabajo, invocan agentes, acceden archivos del workspace, ejecutan shell, seleccionan modelos, editan, delegan ni avanzan un ciclo de forma independiente. La memoria es contexto no confiable y nunca prueba un candidato. Las políticas de prompt y proveedor describen límites de rol previstos; no son enforcement de ejecución en runtime.

## Setup y retiro

Setup previsualiza cambios, exige confirmación, instala el launcher y 17 artefactos exactos de OpenCode incluido el plugin de ciclo de vida auto-descubierto, configura `vgxness mcp --full` sin entrada de plugin en la configuración y publica el catálogo global. El manager v57 de OpenCode y el manager v16 de Codex se conservan únicamente como predecesores históricos exactos reconocidos, junto con identidades anteriores compatibles. Reconoce y retira únicamente bytes históricos exactos del plugin `vgxness.ts` v1-v10 y del provider skill `vgxness-autonomous-stacked-pr` v1/v2/v3. Bytes modificados, malformados, extranjeros, desconocidos o más nuevos bloquean sin eliminación. La desinstalación de OpenCode no elimina skills globales.

## No objetivos

- No hay plugins adicionales, hooks shell o Git, compactación automática, observabilidad amplia ni identidad de sesión fuera del plugin de ciclo de vida administrado. VGXNESS no inyecta ampliamente memorias recientes ni transcripciones en cada prompt; en la primera transformación de sistema de nivel superior elegible, la única inyección automática de memoria del plugin de ciclo de vida administrado es una única transferencia acotada, del mismo proyecto y previamente completada, como datos no confiables y nunca como instrucciones. Los eventos de ciclo de vida no capturan contenido de transcripciones.
- No se instalan hooks shell o Git.
- MCP no obtiene autoridad de filesystem, ejecución, routing, delegación o ciclo.
- No hay instalación automática de red/paquetes ni importación de bases de datos antiguas.
