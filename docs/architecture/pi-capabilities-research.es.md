# Investigación de Pi y oportunidades para VGXNESS

## 1. Alcance y evidencia

**Estado:** investigación y recomendaciones; no es una decisión aceptada ni un cambio SDD.

**Pregunta:** ¿qué capacidades ofrece Pi y cuáles conviene aprovechar, en lugar de duplicar, para evolucionar VGXNESS?

Fuentes y versiones:

- Repositorio oficial: <https://github.com/earendil-works/pi>.
- Snapshot consultado: `b2602be77cb7b0de45dd616407fd210daa48aa75`, obtenido directamente del repositorio. Su último commit está fechado 2026-09-07.
- Paquete local inspeccionado: `@earendil-works/pi-coding-agent` **0.85.1**. Su `package.json` identifica el repositorio anterior como origen.
- VGXNESS: `5be29282d2fa967be3f0fb8c44d76911dae075b2`, merge del PR #379.
- Método: lectura de documentación, ejemplos y puntos de implementación; contraste estático con la extensión y backend de VGXNESS. No se instalaron extensiones ni se ejecutaron ejemplos, benchmarks o pruebas de proveedores.

La cobertura es amplia por familias de capacidades; **no constituye una auditoría exhaustiva de cada línea, proveedor o plataforma**. Los comportamientos documentados por upstream no equivalen a compatibilidad verificada con VGXNESS. `rpc.md` coincide byte a byte entre el snapshot y el paquete local; `extensions.md` y `sdk.md` difieren. Por tanto, las oportunidades de upstream requieren verificar disponibilidad en la versión que decidamos soportar.

Destinatario y decisión pendiente: mantenedor de VGXNESS, para elegir el siguiente alcance. Revisar este informe al actualizar Pi o cambiar los contratos de workers, autenticación, sesiones o packaging.

## 2. Conclusión ejecutiva

Pi no es solamente un CLI con cuatro herramientas. Es un entorno extensible compuesto por API multimodelo, runtime de agentes, sesiones, herramientas, UI de terminal y mecanismos de integración.

La separación recomendada es:

```text
Usuario
  └─ Pi: conversación, UI, modelos, autenticación, sesiones y ejecución
      └─ Extensión VGXNESS: adaptación, comandos, presentación y workers
          └─ Backend Go: memoria durable, autorización y ciclo SDD
```

Los workers pueden seguir siendo procesos Pi separados, con una superficie explícita y acotada. No necesitan recibir la memoria completa ni la autoridad del Manager.

**Prioridades:** primero corregir discrepancias de integración; después hacer visibles las capacidades que ya tenemos; luego mejorar exploración, resultados y coste de workers. No empezar por un fork de Pi ni por sustituir nuestro backend con su servidor experimental.

## 3. Inventario de capacidades

Leyenda: **N** = superficie documentada del producto o SDK; **E** = ejemplo que requiere adaptación; **X** = experimental. N no implica estabilidad indefinida ni verificación local.

| Familia | Capacidades de Pi | Uso potencial en VGXNESS | Tipo / fuente |
|---|---|---|---|
| Modos | TUI interactiva, print, eventos JSON y RPC por stdin/stdout | Trabajo humano, automatización y procesos workers | N [1][3] |
| SDK | `createAgentSession`, `AgentSessionRuntime`, configuración y recursos inyectables | Integraciones TypeScript y pruebas con sesiones reales | N [4] |
| Herramientas | `read`, `bash`, `edit`, `write` por defecto; además `grep`, `find`, `ls`, PowerShell en Windows | Exploración especializada sin exponer shell genérico | N [1][4] |
| Herramientas propias | Schemas TypeBox, validación, preparación de argumentos y reemplazo de built-ins | Contratos nativos para memoria, SDD y acciones acotadas | N [2] |
| Concurrencia | Ejecución paralela por defecto; `executionMode: sequential`; cola por archivo | Evitar carreras entre operaciones de escritura | N [2][11] |
| Resultados | Streaming con `onUpdate`, `content`, `details`, `usage`, terminación del lote | Progreso visible y resultados estructurados de workers | N [2][11] |
| Herramientas dinámicas | Registrar y activar herramientas durante la sesión | Cargar capacidades cuando hacen falta | N [2] |
| Deferred loading | Definiciones diferidas nativas en modelos compatibles y fallback general | Reducir carga inicial de schemas; medir efecto real en caché | N [2] |
| Eventos | Ciclo de sesión, turno, mensaje, herramienta, modelos, compactación e input | Coordinación y estado observable sin analizar texto del terminal | N [2] |
| Intercepción | Bloquear tool calls; modificar argumentos/resultados/contexto/payload | Controles específicos y adaptación; no sustituye aislamiento | N [2][14] |
| Comandos | Slash commands, flags, atajos y autocompletado | `/vgx-status`, `/vgx-workers`, consultas de memoria y SDD | N [2][7] |
| Preguntas | Selección, confirmación, input, editor y cancelación | Aprobaciones vinculadas a acciones concretas | N [2][3] |
| UI | Widgets, estado, header/footer, renderers de tools/mensajes/entradas | Panel del Manager, progreso y evidencia expandible | N [2][5] |
| TUI avanzada | Componentes, Markdown, imágenes, editor personalizado; overlays y fullscreen | Experiencia VGXNESS sin construir otra TUI completa | N/X [5][8] |
| Sesiones | JSONL en árbol, resume, fork, clone, labels y navegación | Continuidad de trabajo y referencias a hitos | N [1][9] |
| Estado de extensión | Entradas propias y details, reconstrucción por rama | Estado de presentación; no duplicar memoria durable | N [2][9] |
| Compactación | Manual, automática, recuperación de overflow y resúmenes de ramas | Conservar un resumen operativo acotado y referencias durables | N [10] |
| Cola de mensajes | Steering, follow-up, abort y clear queue | Intervención humana y cancelación predecible | N [3][4] |
| Modelos | Catálogos, selección, scopes, capacidades y niveles de reasoning | Resolver planes a modelos disponibles sin inventar soporte | N [12][13] |
| Proveedores | OpenAI, Codex, Anthropic, Google, Copilot, Bedrock, Vertex y muchos gateways | Reutilizar integración de proveedores y autenticación de Pi | N [12][13] |
| Proveedores propios | API compatibles, streaming propio, OAuth/SSO y refresh de catálogo | Endpoints corporativos o privados bajo configuración explícita | N [13] |
| Modelos locales | Ollama/vLLM/LM Studio por API compatible; integración llama.cpp router | Exploración o clasificación local, si demuestra calidad suficiente | N [12][21] |
| Multimodalidad | Imágenes en mensajes y resultados, capacidades por modelo | Revisar capturas y evidencia visual | N [12] |
| Generación de imágenes | API separada `ImagesModels`, actualmente OpenRouter | Posible capacidad opcional; baja prioridad para el núcleo | N [12] |
| Skills | Agent Skills, descubrimiento global/proyecto, carga progresiva | Catálogo portable de capacidades de VGXNESS | N [6] |
| Prompts | Templates parametrizados y comandos de skills | Atajos de trabajo, nunca autoridad de ejecución | N [7] |
| Packages | npm, git y rutas locales; manifests, filtros, scopes y pinning | Distribución gestionada sin fork | N [8] |
| Temas | JSON, tokens de color, hot reload y colores de exportación | Identidad visual opcional, respetando preferencias | N [5][8] |
| Git | Ejemplos de checkpoints, guards y auto-commit | Adaptar solo acciones autorizadas; no instalar automatismos destructivos | E [15] |
| Subagentes | Ejemplo single/parallel/chain, contextos separados y UI de progreso | Inspiración para mejorar nuestro worker runner | E [16] |
| Plan mode | Ejemplo de exploración, plan y seguimiento | UI de consulta; no sustituir SDD por marcadores textuales | E [15] |
| Ejecución remota | Operations interfaces para SSH/contenedores y spawn hooks | Mantener contrato de tools con ejecución aislada | N/E [2][14] |
| Seguridad | Project trust controla carga; no hay sandbox incorporado | VGXNESS debe conservar controles propios y definir aislamiento | N [14] |
| Aislamiento | Patrones Docker, Gondolin, OpenShell y Docker Sandboxes | Workers no supervisados con recursos mínimos | E [14] |
| Exportación | HTML/JSONL, importación y compartir sesión | Evidencia revisable; compartir requiere sanitización y autorización | N [1][3] |
| Diagnóstico | Uso/coste/caché/contexto, errores, reintentos y metadatos de sesión | Saber qué hace el Manager y cuánto cuesta | N [2][3][20] |
| Pruebas sin modelo | Faux provider con respuestas y tool calls guionizados | Contratos deterministas sin API keys | N [12] |
| Evals | Harness sobre `AgentSession`, vitest-evals y artefactos nativos | Medir cambios de prompts/skills/modelos sin asumir mejoras | Infraestructura del repo [19] |
| Arquitectura distribuida | Pi server/client/protocol, Session durable y múltiples presentaciones | Seguimiento tecnológico, no dependencia inmediata | X [17] |
| Chord | Facets, servicios tipados, estado replicado y recarga de generaciones | Posible capa futura de UI/backend desacoplados | Paquete separado [18] |
| SQLite de sesiones | Backend separado sobre `node:sqlite` | Evaluar solo si cambia nuestra estrategia de sesiones | Superficie distinta del JSONL habitual [17] |

### Ausencias deliberadas

Pi no incluye en su núcleo una política universal de subagentes, SDD, plan mode, to-dos o MCP. Los ejemplos/extensiones permiten construirlos. Tampoco ofrece un sandbox implícito por ejecutar en RPC o por desactivar herramientas.

La automatización de navegador o escritorio no es una herramienta básica de Pi: necesita una integración adicional. Las capacidades de visión no equivalen a control de navegador.

## 4. Qué ya aprovecha VGXNESS

Observado en el snapshot local:

- `packages/pi/src/extension.ts`: registro de herramientas propias, descubrimiento de skills/prompts, incorporación de instrucciones del Manager y eventos de sesión.
- `internal/providers/pi/`: backend Go, protocolo, setup e instalación gestionada.
- `packages/pi/src/session/adapter.ts`: sesiones de memoria con handles privados, checkpoint, renovación, borrador y handoff acotado marcado `UNTRUSTED`.
- `packages/pi/src/workers/mission.ts`: misión con digest, nonce, targets con hashes, lista exacta de comandos y revalidación antes de arrancar.
- `packages/pi/src/workers/runner.ts`: procesos Pi por RPC con perfil temporal, sin recursos ambientales de Pi ni herramientas built-in; lectura y checks propios, y patch para roles escritores.
- `packages/pi/src/tools/apply_patch.ts`: preflight, staging, comprobación de drift y conservación de evidencia cuando la recuperación queda pendiente.
- `packages/pi/src/extension.ts`: resolución contra catálogo disponible y validación de autenticación antes del worker.
- `packages/pi/package.json`: seis backends por plataforma/arquitectura; paquete privado `@vgxness/pi` 0.1.0.

Esto ya es una integración nativa, no únicamente un wrapper de CLI. La existencia de código y tests no demuestra que todos los caminos funcionen con cada distribución/proveedor.

### Corrección sobre explorer

`packages/pi/src/workers/roles.ts` **sí declara `explore`**. También declara `general`, `verifier`, roles CARE y roles SDD. `workerCanWrite` permite escritura solo a `general` y `sdd-apply`.

No hay un rol literalmente llamado `explorer`, pero sí existe el equivalente `explore`. Además, upstream tiene un ejemplo `scout`. La respuesta previa que limitaba exploración a `general` fue incorrecta. No se ejecutó un worker en esta investigación; rol declarado no equivale a transporte disponible.

## 5. Discrepancias y riesgos concretos

Son hallazgos estáticos; las consecuencias necesitan reproducción dirigida.

### A. Terminación prematura de workers

**Hecho:** `PiRpcRunner` resuelve su resultado al recibir `agent_end`. La documentación RPC distingue ese evento de `agent_settled`: puede haber reintento, compactación o continuación después.

**Riesgo inferido:** tratar un tramo intermedio como resultado final y cerrar el proceso antes de que termine la operación completa.

**Propuesta:** esperar settlement, conservar los resultados necesarios y clasificar fallo/cancelación antes de declarar completado. No basta sustituir el nombre del evento: `agent_settled` no transporta el mismo resultado.

### B. Error de patch que puede parecer éxito al runtime

**Hecho:** el camino `recovery_pending` de nuestro patch retorna un objeto con `isError: true`. Pi documenta que `execute()` debe lanzar para señalar error; en `packages/agent/src/agent-loop.ts`, un retorno normal se envuelve con `isError: false`.

**Riesgo inferido:** la recuperación pendiente se muestra como texto, pero no queda clasificada como error por el runtime.

**Propuesta:** adaptar el resultado al contrato real de Pi sin perder evidencia de recuperación ni sugerir reintento seguro. Verificar con carga de extensión real, no solo invocación directa de `execute()`.

### C. Concurrencia de escritura con built-ins

**Hecho:** Pi ejecuta herramientas en paralelo por defecto y ofrece `withFileMutationQueue`. Nuestro `apply_patch` no usa esa cola ni declara `executionMode: sequential`.

**Riesgo inferido:** conflictos entre patch y `edit`/`write`, o entre patches. Los checks de drift ayudan, pero no reemplazan coordinación de la ventana read-modify-write.

**Propuesta:** estrategia explícita de exclusión. Para múltiples archivos, evaluar orden de adquisición y deadlocks; una cola local no garantiza exclusión entre procesos.

### D. Exploración demasiado limitada por diseño de misión

**Hecho:** `worker_read` lee solo targets enumerados y devuelve el archivo completo. El schema admite hasta 64 targets. No hay herramienta de descubrimiento/búsqueda propia en la superficie del worker.

**Consecuencia:** el Manager necesita conocer de antemano buena parte de lo que pretende delegar; archivos grandes también pueden agotar el límite RPC.

**Propuesta:** perfil `explore` con listado, búsqueda y lectura paginada dentro de raíces autorizadas, límites de bytes/resultados y exclusión de rutas sensibles. No sustituirlo por shell sin restricciones.

### E. Presupuestos y diagnóstico

**Hecho:** el prompt del worker tiene timeout por defecto de 30 segundos; cada check dispone de 2 segundos. La interfaz pública de `task` no expone esos tiempos. El runner descarta stderr y el tool retorna texto sin `usage` ni progreso `onUpdate`.

**Propuesta:** presupuestos explícitos por misión, resultados tipados, diagnósticos sanitizados, progreso y uso agregado. No ampliar todos los límites indiscriminadamente.

### F. Soporte de modelos y plataformas es más estrecho que el de Pi

**Hecho:** la adaptación admite esfuerzos low/medium/high y requiere reasoning. El catálogo de planes se limita al proveedor actual y a un máximo de tres candidatos; la preparación de auth rechaza `auth.env` y exige una API key string. Los workers rechazan Windows por ownership del árbol de procesos.

**Propuesta:** exponer una matriz real de capacidades. Soporte de un proveedor o SO en Pi no demuestra soporte en workers VGXNESS. Mantener rechazo explícito de variantes no probadas.

### G. Compatibilidad y schemas

**Hecho:** varias superficies usan `any` y un tipo local reducido de `ExtensionApi`; el package fuente no declara peer dependencies de Pi. Nuestros schemas incluyen unions de literales, mientras upstream recomienda `StringEnum` para compatibilidad con Google.

**Propuesta:** compilar contra tipos públicos, verificar metadata del artefacto final y probar schemas por proveedor. No reportar incompatibilidad con Google como reproducida. El empaquetado final podría diferir del manifest fuente.

### H. Estado de rama y duración de sesión

**Hecho:** enlazamos start, compact, settled y shutdown; no hay adaptación explícita a `session_tree` en la extensión. La renovación se hace al settlement, no periódicamente durante una operación larga.

**Preguntas pendientes:** ¿qué debe ocurrir con el handoff y la autoridad al cambiar de rama? ¿una operación larga puede exceder el lease del backend? No se establece aquí la duración de ese lease.

**Propuesta:** cubrir tree/fork/resume/reload y trabajos prolongados en pruebas de contrato antes de enriquecer la continuidad automática.

## 6. Oportunidades priorizadas

Las prioridades son recomendaciones, no compromisos ni estimaciones de calendario.

### P0 — Ajustar la integración al contrato de Pi

1. Settlement completo, clasificación de errores y recuperación pendiente.
2. Coordinación de mutaciones y cancelación de procesos/checks.
3. Tipos públicos, compatibilidad por versión y pruebas contra el paquete real.
4. Diagnóstico explícito cuando `task` carece de transporte, auth o plataforma soportada.

**Resultado buscado:** no confundir “herramienta registrada” con “capacidad utilizable”, ni “retorno de función” con “operación exitosa”.

### P1 — Hacer visible el Manager

Proponer comandos con namespace propio, por ejemplo:

- `/vgx-status`: workspace, backend, versión, modo y disponibilidad de workers.
- `/vgx-workers`: rol, estado, archivos autorizados, tiempo y modelo efectivo.
- `/vgx-memory`: consultas y handoff sanitizado con procedencia.
- `/vgx-sdd`: estado del cambio activo; mutaciones solo bajo autorización explícita.

Usar `setStatus`, widgets y renderers expandibles antes de reemplazar el editor o footer completos. Mostrar “esperando usuario”, “reintentando”, “compactando” y “verificación pendiente”, no un spinner ambiguo.

La UI consulta estado autoritativo; no mantiene una segunda máquina de estados SDD. Las variantes RPC necesitan mensajes soportados por su protocolo, no componentes TUI.

### P1 — Explorer útil y workers observables

Aprovechar `explore`, agregar herramientas de descubrimiento acotadas y devolver:

- hallazgos y preguntas abiertas;
- rutas y referencias verificables;
- cobertura y límites de la inspección;
- motivo de terminación;
- uso, duración y modelo efectivo.

Inspirarse en el ejemplo `scout` para contexto comprimido, single/parallel/chain y UI. **No copiar su autorización ni su manejo de procesos como sustituto del contrato de VGXNESS.**

La exploración puede paralelizarse sobre scopes independientes. Las escrituras siguen requiriendo exclusión y revisión del Manager. El scheduler actual decide lectores/escritores por rol: incluso `general` read-only entra en la categoría de escritor.

### P1 — Continuidad sin contaminar memoria

Usar los eventos y APIs de contexto para facilitar que el Manager recupere un resumen acotado con referencias al estado durable.

Separar:

1. historial completo de conversación: Pi;
2. estado de presentación y rama: Pi;
3. hechos durables y SDD: backend VGXNESS.

No guardar automáticamente prompts, payloads o transcripciones en memoria. Una compactación no crea autoridad, no sustituye revisiones aceptadas y no prueba que una tarea haya terminado.

### P2 — Capacidad bajo demanda

Mantener una superficie inicial pequeña y activar herramientas adicionales cuando hagan falta. Es especialmente útil si crece el catálogo de integraciones.

- Activación no equivale a autorización.
- Preservar las herramientas ajenas; no sobrescribir globalmente todo el set activo.
- Los cambios puramente aditivos pueden aprovechar deferred loading.
- Cambiar prompt snippets/guidelines puede invalidar caché aunque los schemas sean diferidos.
- Medir tokens, caché, coste, activación correcta y omisiones; no asumir ahorro.

### P2 — Modelos por capacidad

Reutilizar catálogo/auth de Pi y mostrar claramente modelo solicitado, seleccionado y esfuerzo efectivo. Explorar separación de modelos para reconocimiento, implementación y verificación solo dentro de proveedores autorizados.

`getSupportedThinkingLevels` y los metadatos reales son preferibles a nombres comerciales o tablas duplicadas. Pi permite niveles adicionales en ciertos modelos, pero eso no justifica reinterpretar silenciosamente los planes Go de VGXNESS.

Los modelos locales son candidatos para tareas acotadas; su calidad, consumo de recursos y soporte de tools deben medirse antes de asignarles verificaciones críticas.

### P2 — Testing y observabilidad nativos

- Faux provider para flujos deterministas con el runtime real.
- RPC/SDK reales para herramientas, errores, compactación y reemplazo de sesión.
- Fixtures actuales de VGXNESS como base, no como única representación de Pi.
- Harness de evals upstream como referencia para comparaciones, sin reemplazar los criterios ni evidencia independiente de VGXNESS.
- `usage` de herramientas para coste de llamadas anidadas; telemetría opcional sin contenido sensible.

Pruebas sugeridas: error después de aceptar un prompt; retry tras `agent_end`; cancelación con cola; patch y edit sobre el mismo archivo; fallo de recuperación; fork/tree/reload; truncación/Unicode; transporte ausente; provider no soportado; tentativa de acceso fuera del scope.

### P3 — Ejecución aislada e interfaces remotas

Evaluar containers/VMs para repositorios no confiables y ejecución no supervisada. Las operations interfaces permiten conservar herramientas y presentación, pero el backend debe seguir imponiendo autoridad.

Para una UI propia futura, comparar SDK con RPC. Mantener server/client/protocol y Chord como línea de investigación independiente hasta necesitar múltiples presentaciones y contar con contratos adecuados.

## 7. Alternativas de arquitectura

| Alternativa | Ventaja | Coste / límite | Recomendación |
|---|---|---|---|
| Extensión Pi + backend Go | Conserva UX y ecosistema; mantiene semántica VGXNESS | Adaptación de versiones y dos runtimes | Base recomendada |
| Workers Pi por RPC | Separación de procesos y superficie explícita | Framing, lifecycle, auth y diagnóstico a cargo nuestro | Mantener y mejorar |
| Workers mediante SDK en proceso | Tipado y acceso directo a estado/eventos | No hay separación de proceso; recursos/lifecycle compartidos | Útil en tests y casos controlados |
| Pi agent-core directamente | Máximo control del loop | Hay que reconstruir recursos, sesiones y parte del harness | No reemplazar por defecto |
| Fork de Pi | Control total de producto | Coste continuo de mantenimiento y divergencia | Evitar sin necesidad demostrada |
| Servidor experimental + Chord | Sesiones durables y múltiples presentaciones | Contratos experimentales, integración aún source-only | Investigar después |

### No confundir tres protocolos

- RPC habitual de Pi: JSONL con LF por stdin/stdout.
- Backend VGXNESS: protocolo propio `vgxness-pi/v1`, con bindings y capacidades.
- Pi protocol experimental: CBOR con prefijo de longitud, versión 8 en el snapshot.

No son intercambiables. El servidor experimental tampoco implementa por sí mismo autenticación de peers. El backend SQLite de sesiones declara que el host debe garantizar un único escritor; no aporta leases entre procesos ni reemplaza el storage de memoria de VGXNESS.

## 8. Límites de seguridad y datos

Actores: usuario autorizado, Manager, workers acotados y contenido externo/repo potencialmente malicioso. Activos: código, credenciales, memoria, revisiones aceptadas y evidencias.

| Flujo | Riesgo | Control requerido / dueño |
|---|---|---|
| Repo/documentos → contexto del agente | Instrucciones inyectadas | Tratar como datos; nunca elevar autorización / Manager |
| Manager → worker | Exceso de scope o herencia de credenciales | Misión explícita, recursos mínimos, revalidación / runner |
| Worker → filesystem/comandos | Cambios fuera de scope | Tools acotadas y aislamiento cuando corresponda / runner + host |
| Resultado → memoria/SDD | Evidencia falsa o datos sensibles | Validación independiente, minimización y backend autoritativo / Manager + Go |
| Sesión/telemetría → servicios externos | Filtración de código o secretos | Consentimiento específico y sanitización / integración |
| UI → transición SDD | Aprobación ambigua | Vincular decisión a acción y revisión exactas / Manager + Go |

Project trust solo controla carga de recursos. `--offline` desactiva operaciones de red de arranque; **no es un bloqueo general de red**. Desactivar `write` no hace read-only una herramienta `bash`. Enrutamiento de built-ins a una VM no mueve automáticamente nuestras tools custom al mismo aislamiento.

No se ejecutaron pruebas adversariales ni se certifica seguridad. Una lista de comandos permitidos puede ejecutar código del repositorio: la autorización de argv no demuestra inocuidad de su implementación.

## 9. Secuencia recomendada

1. Congelar versión objetivo de Pi y reproducir los hallazgos P0.
2. Corregir settlement, error contract y concurrencia con evidencia del runtime real.
3. Añadir diagnóstico y UI de capacidades disponibles.
4. Mejorar `explore`, presupuestos y resultado estructurado de workers.
5. Mejorar continuidad por ramas y uso/coste observable.
6. Evaluar carga dinámica, routing y aislamiento con mediciones separadas.
7. Considerar arquitectura remota experimental solo si aparece una necesidad de producto que la justifique.

No se implementó ninguna de estas recomendaciones ni se lanzó SDD como parte de esta investigación.

## 10. Fuentes reproducibles

Todos los enlaces upstream siguientes están fijados al snapshot consultado.

[1]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/README.md
[2]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/extensions.md
[3]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/rpc.md
[4]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/sdk.md
[5]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/tui.md
[6]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/skills.md
[7]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/prompt-templates.md
[8]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/packages.md
[9]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/session-format.md
[10]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/compaction.md
[11]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/agent/README.md
[12]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/ai/README.md
[13]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/custom-provider.md
[14]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/security.md
[15]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/examples/extensions/README.md
[16]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/examples/extensions/subagent/README.md
[17]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/server/README.md
[18]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/chord/README.md
[19]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/evals/README.md
[20]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/telemetry/README.md
[21]: https://github.com/earendil-works/pi/blob/b2602be77cb7b0de45dd616407fd210daa48aa75/packages/coding-agent/docs/llama-cpp.md

Referencias adicionales consultadas en ese mismo snapshot: `docs/models.md`, `settings.md`, `themes.md`, `keybindings.md`, `environment-variables.md`, `containerization.md`, `development.md`; READMEs de `packages/client`, `protocol` y `session-backends/sqlite-node`; ejemplos `subagent/index.ts`, `plan-mode/README.md` y `structured-output.ts`.
