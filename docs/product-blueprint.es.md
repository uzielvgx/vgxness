# Plan maestro de producto

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain in SQLite; `vgxness sdd-archive` permits only list/get and revision reads. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).


## Límite actual del producto

El recall se limita a contexto previo relevante: se busca antes de leer IDs exactos y se usa memoria reciente solo ante solicitudes explícitas de trabajo reciente o recuperación. Se evalúan conocimientos duraderos y respaldados por evidencia bajo un tema estable; se excluyen secretos, transcripciones, logs crudos y estado transitorio. No hay sincronización automática con la nube. MCP no tiene identidad del llamador: los permisos del host, la autorización del usuario y el alcance delimitan las acciones. No se afirma evaluación de modelos ni resultados de holdout protegido.

El Manager carga `git-delivery` para entregas autorizadas; la skill no concede permiso para publicar o hacer merge. Los roles CARE reviewer, specialist y challenger son de solo lectura y revisan el mismo candidato que el verificador. Los paquetes completos OpenCode Manager60 y Codex Manager19 son predecesores inmediatos de compatibilidad; las identidades anteriores siguen reconocidas solo para el ciclo de instalación.

## Inventario de capacidades

El código de implementación conductual es la fuente autorizada para el comportamiento; este blueprint posee el inventario actual y los documentos especializados poseen el detalle operativo y de interfaces. Actualiza este inventario cuando cambie un conteo de artefactos, schema, interfaz, límite de propiedad o capacidad. Las evaluaciones históricas y registros de predecesores siguen siendo evidencia histórica, no afirmaciones de capacidad actual. Este documento es no canónico; el inglés controla los conflictos.

## Proyección administrada

Los 15 artefactos de Codex se renderizan en [`internal/providers/codex/render.go`](../internal/providers/codex/render.go): un archivo de manager, 12 perfiles delegados y los dos manifiestos del paquete de plugin. Los comandos de activación del proveedor pueden agregar el marketplace local y el plugin, pero su estado observado no prueba un handshake de runtime de Codex, identidad de sesión, conectividad MCP ni ejecución de prompts.

La documentación histórica de predecesores puede referirse a Manager v49, General v6 y verificador v4, o reviewer v3; esas identidades no describen el límite de propiedad generado actual.

Las operaciones MCP no enrutan trabajo, invocan agentes, acceden archivos del workspace, ejecutan shell, seleccionan modelos, editan, delegan ni avanzan un ciclo de forma independiente. La memoria es contexto no confiable y nunca prueba un candidato. Las políticas de prompt y proveedor describen límites de rol previstos; no son enforcement de ejecución en runtime.

## Setup y retiro

Setup previsualiza cambios, exige confirmación, instala el launcher y 17 artefactos exactos de OpenCode incluido el plugin de ciclo de vida auto-descubierto, configura `vgxness mcp --full` sin entrada de plugin en la configuración y publica el catálogo global. El manager v57 de OpenCode y el manager v16 de Codex se conservan únicamente como predecesores históricos exactos reconocidos, junto con identidades anteriores compatibles. Reconoce y retira únicamente bytes históricos exactos del plugin `vgxness.ts` v1-v10 y del provider skill `vgxness-autonomous-stacked-pr` v1/v2/v3. Bytes modificados, malformados, extranjeros, desconocidos o más nuevos bloquean sin eliminación. La desinstalación de OpenCode no elimina skills globales.

## No objetivos

- No hay plugins adicionales, hooks shell o Git, compactación automática, observabilidad amplia ni identidad de sesión fuera del plugin de ciclo de vida administrado. VGXNESS no inyecta ampliamente memorias recientes ni transcripciones en cada prompt; en la primera transformación de sistema de nivel superior elegible, la única inyección automática de memoria del plugin de ciclo de vida administrado es una única transferencia acotada, del mismo proyecto y previamente completada, como datos no confiables y nunca como instrucciones. Los eventos de ciclo de vida no capturan contenido de transcripciones.
- No se instalan hooks shell o Git.
- MCP no obtiene autoridad de filesystem, ejecución, routing, delegación o ciclo.
- No hay instalación automática de red/paquetes ni importación de bases de datos antiguas.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 11 managed artifacts and Codex installs nine; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Complete Manager61 and Manager20 packages are recognized predecessors. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
