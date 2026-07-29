# Plan maestro de producto de VGXNESS

> **Versión del plan maestro:** 1.0  
> **Estado del documento:** Current  
> **Autoridad:** Traducción al español no canónica  
> **Fuente canónica:** [English — canonical product blueprint](product-blueprint.md)

Este documento es una traducción completa para navegación y comprensión. El documento en inglés es la única fuente canónica de la visión, el vocabulario, los límites y la hoja de ruta de VGXNESS. Los documentos complementarios describen contratos y flujos de trabajo más acotados; no redefinen este plan maestro. Si la versión en español difiere, prevalece la versión en inglés.

Las etiquetas de entrega de capacidades tienen significados precisos y respaldados por evidencia:

- **Implemented (Implementado)** — el comportamiento entregado está presente y puede verificarse en el candidato actual del repositorio. La publicación aún requiere los controles normales de revisión y merge.
- **Partial (Parcial)** — se entregó un subconjunto delimitado; el ciclo de vida o servicio indicado no está completo.
- **Contracts-only (Solo contratos)** — existen esquemas o reglas semánticas, pero no proporcionan el comportamiento de runtime que describen.
- **Planned (Planificado)** — dirección de producto aprobada sin comportamiento de runtime entregado.

**Deferred (Diferido)** y **Non-goal (No objetivo)** siguen siendo etiquetas de alcance, no afirmaciones de entrega. El trabajo diferido está aprobado pero fuera del horizonte actual; un no objetivo requiere una nueva decisión de alcance. «Current» solo describe la vigencia de este documento.

## 1. Propósito y audiencia

VGXNESS está dirigido a desarrolladores que desean que el trabajo asistido por IA siga siendo comprensible, delimitado y recuperable. El candidato actual contiene una base funcional del plano de control local y un ciclo SDD nativo instalado en OpenCode con identidades exactas, política, evidencia Chronicle, almacenamiento semántico y estructurado aislado, delegación delimitada y operación consciente de recuperación. La recuperación enriquecida de SDD interrumpido, el routing neutral entre proveedores/sondeos de catálogo y la integración automática de delivery siguen planificados.

**Implemented:** El repositorio compila un binario `vgxness` con composición, launcher versionado con activación atómica y rollback, resolución de almacenamiento, `status`/`doctor`, SQLite/FTS5 esquema v5, validación de contratos, servicios Chronicle, Registry, Gatekeeper, prompts y ejecución OpenCode. La proyección instalada contiene 14 artefactos gestionados: manager v29, cinco revisores de solo lectura, seis perfiles SDD de solo lectura, plugin de almacenamiento v5 y manifiesto de planes de modelo. El plugin expone 18 herramientas de almacenamiento: cinco de memoria semántica y 13 de SDD. El CLI del puente legacy permanece por compatibilidad, pero no se proyecta mediante el plugin.

**Implemented:** Chronicle anexa y verifica eventos JSONL, deriva estado de tareas, publica instantáneas activas mediante archivos SHA-256 inmutables más un único reemplazo atómico del puntero y recupera una eliminación interrumpida del puntero terminal. **Partial:** su alcance de recuperación sigue siendo menor que el futuro ciclo general de puntos de control/artefactos.

**Contracts-only:** Los esquemas Draft 2020-12 de autoría independiente para rutas futuras de routing neutral entre proveedores, artefactos y continuidad definen registros que los runtimes entregados aún no ejecutan. Las invariantes SDD nativas están implementadas en el dominio Go y SQLite esquema v5; los esquemas usados por rutas de compatibilidad actuales se validan en runtime.

**Implemented/Partial:** SDD nativo admite backends `memory`, `openspec` y `hybrid`, más modos por cambio `automatic` e `interactive`. Memoria es canónica en hybrid; OpenSpec queda delimitado a `openspec/changes/<safe-change-id>/`; el contenido divergente nunca se importa automáticamente. El manager es el único escritor del ciclo y workspace, los seis agentes SDD son de solo lectura, apply compone un patch ligado por hash y los cinco revisores son de solo lectura. Puente, orquestación del plano de control, mantenimiento, edición aislada, tickets/waves y Delivery Authority siguen como superficies CLI/de mantenimiento, no como scheduler instalado activo.

### Contrato del documento y la traducción

- El inglés es la fuente de decisiones y resuelve las ambigüedades.
- Esta versión en español es completa para navegación y comprensión, pero explícitamente no canónica.
- Ambos documentos mantienen la misma versión, modelo de estados, inventario de secciones numeradas, decisiones, controles, significado de la hoja de ruta y temas de trazabilidad.
- Un cambio semántico en la versión canónica debe actualizar ambos documentos de forma atómica. Una diferencia bloquea la publicación.
- Los documentos complementarios enlazan la fuente canónica en lugar de mantener hojas de ruta competidoras.

## 2. Estado general

| Estado | Alcance |
| --- | --- |
| **Implemented** | Binario/composición Go; launcher y auto-instalación/actualización/rollback; almacenamiento semántico y SDD estructurado esquema v5; ciclo/backends/modos SDD nativos; escrituras solo del manager; agentes SDD/revisión de solo lectura; proyección OpenSpec; validación de contratos; eventos Chronicle y recuperación delimitada; Registry/Gatekeeper; ejecución OpenCode; 14 artefactos gestionados y 18 herramientas; setup, pruebas y CI. |
| **Partial** | Chronicle aún no ofrece reconstrucción enriquecida de SDD interrumpido; Delivery Authority sigue como CLI explícita de compatibilidad sin integración automática; el smoke nativo de Windows sigue incompleto. |
| **Contracts-only** | Formas Draft 2020-12 y reglas semánticas para rutas futuras de routing neutral entre proveedores, artefactos y continuidad que los runtimes entregados aún no ejecutan. |
| **Planned** | Recuperación Chronicle enriquecida de SDD interrumpido, routing neutral entre proveedores/sondeos de catálogo, integración automática de delivery, UX de autonomía/aprobación más rica, TUI, adaptadores adicionales, validación nativa de Windows y ciclo semántico avanzado. |
| **Deferred** | Adaptadores de runtime adicionales, exposición MCP local opcional, más clientes MCP externos, recuperación semántica avanzada y superficies gráficas de producto. |
| **Non-goal** | Artefactos copiados de terceros, autonomía destructiva silenciosa, verdad operativa oculta, dependencia rígida de un runtime o herramienta, integración con Engram, bucles de agentes sin límites, promoción automática de prototipos a producción o una interfaz propietaria de la política de negocio. |

**Límite de la evidencia:** El plugin de almacenamiento v5 expone cinco herramientas de memoria semántica y 13 de SDD; no tiene autoridad de filesystem, shell, scheduler, routing, edición, delegación ni ciclo de vida. Cada mutación SDD se cierra salvo que el contexto confiable identifique al manager superior rastreado. Los planes configuran slots exactos, pero no prueban disponibilidad de modelos en runtime. Los comandos del puente/plano de control/edición/delivery permanecen como compatibilidad. No se entregan TUI, MCP propio, Engram, integración automática de delivery, adaptadores adicionales ni mutación silenciosa de configuración.

**Contracts-only — limitación:** Los esquemas bajo [`schemas/`](schemas/README.md) mezclan contratos de runtime entregados con comportamientos futuros y parciales. La validación en runtime solo demuestra los esquemas y rutas realmente invocados; las declaraciones `$schema` por sí solas no prueban la aplicación completa del producto.

## 3. Principios de producto

1. **La persona dirige.** La persona usuaria controla objetivos, alcance y aprobación de acciones importantes.
2. **Enseñar, no ocultar.** Explicar hechos, recomendaciones, compensaciones e incertidumbre relevantes.
3. **Verificar antes de aceptar.** Las afirmaciones sobre código, herramientas, estado y finalización requieren evidencia.
4. **Coordinar mediante límites.** La orquestación, ejecución, revisión, almacenamiento, habilidades, permisos y adaptadores siguen siendo separables.
5. **Mantener el estado inspeccionable.** Chronicle registra la verdad operativa; la memoria semántica conserva significado duradero.
6. **Usar la ruta suficiente más rápida.** El trabajo simple permanece directo; un asunto delimitado usa una tarea por defecto; la evidencia independiente se solapa solo cuando ahorra tiempo; la complejidad y el riesgo reciben planificación y validación proporcionales.
7. **Cerrar ante límites inseguros.** Capacidades ausentes, contratos inválidos, adaptadores obsoletos o aprobaciones faltantes detienen el avance.
8. **Conservar continuidad, no el volumen de la transcripción.** Transferir paquetes pequeños y referencias duraderas.
9. **Diseñar para recuperación.** Cada operación delimitada tiene fallos visibles, cancelación y una siguiente acción segura.
10. **Mantener adaptadores pequeños y opcionales.** Las herramientas externas traducen capacidades sin controlar la política de VGXNESS.
11. **Usar inglés por defecto en artefactos técnicos.** Una política explícita de la persona o del proyecto puede autorizar otro idioma.
12. **Proteger la atención de revisión.** Las unidades de trabajo son comprensibles, verificables y reversibles de forma independiente.

## 4. Límite de desarrollo independiente (clean room)

VGXNESS puede estudiar sistemas como Gentle AI, Engram, OpenCode, CodeGraph, OpenPencil y otros runtimes o herramientas para comprender categorías de capacidades y necesidades de interoperabilidad. Obtener resultados comparables no permite copiar la implementación.

| Límite | Decisión |
| --- | --- |
| **Planned — paridad de capacidades** | Proporcionar de forma independiente resultados útiles de orquestación, memoria, habilidades, análisis estructural, diseño, revisión, recuperación y entrega. |
| **Implemented — regla de autoría** | La prosa y los esquemas de este repositorio son contratos propios de VGXNESS y mantienen autoría independiente. |
| **Non-goal: artefactos copiados** | No copiar código, prompts, esquemas, nombres, habilidades, manifiestos, estructuras de base de datos ni flujos exactos de terceros. |
| **Non-goal: acoplamiento oculto** | No convertir el comportamiento privado de un runtime, herramienta o almacén externo en el modelo de dominio de VGXNESS. |
| **Planned — interoperabilidad** | Integrar mediante adaptadores documentados, contratos de capacidades, referencias estables y procedencia explícita. |

Los sistemas externos son inspiración o adaptadores opcionales, nunca la fuente de identidad de VGXNESS. El diseño limpio también se aplica a los adaptadores preferidos: una preferencia no puede convertirse en una dependencia oculta.

## 5. Vocabulario de producto

VGXNESS utiliza cuatro grupos de conceptos distintos. No deben agruparse como una sola lista de «agentes».

| Grupo | Significado | Miembros |
| --- | --- | --- |
| Capacidades de producto | Roles respaldados por LLM que ofrecen un tipo delimitado de razonamiento o trabajo. | Navigator, Scout, Blueprint, Forge, Sentinel y Challenger opcional. |
| Servicios deterministas | Límites de política y persistencia controlados por código; no improvisan decisiones. | Registry, Chronicle y Gatekeeper. |
| Modos operativos | Responsabilidades con nombre usadas cuando una capacidad realiza trabajo SDD o de recuperación. | explore, propose, spec, design, tasks, apply, verify, archive, fix y recovery. |
| Adaptadores | Traducciones reemplazables entre el plano de control y runtimes, herramientas, protocolos o almacenes externos. | OpenCode, CodeGraph, OpenPencil, MCP y adaptadores futuros. |

**Implemented/Partial:** Los nombres de capacidades describen responsabilidades estables, no un proceso por nombre. Los perfiles SDD instalados realizan roles delimitados de research/proposal/spec/design/tasks/apply, con verificación y escrituras del ciclo propiedad del manager; el routing neutral entre proveedores sigue planificado.

**Contracts-only:** Los términos de los esquemas existentes siguen siendo normativos para registros legibles por máquinas. Este plan maestro controla la taxonomía para personas, no las definiciones de campos de los esquemas.

La división de entrega es intencional: memoria semántica propia, almacenamiento/ciclo SDD estructurado, Registry, Gatekeeper, ejecución delimitada validada, publicación crash-atomic de Chronicle y la ruta OpenCode están **Implemented**. Chronicle sigue **Partial** para recuperación enriquecida de SDD interrumpido y el routing neutral entre proveedores sigue **Planned**.

## 6. Modelo del sistema

VGXNESS tiene un plano de control Go local y un ciclo SDD nativo de OpenCode **Implemented y delimitado**, con límites explícitos de autoridad. El routing neutral entre proveedores/sondeos de catálogo y la recuperación enriquecida siguen **Planned**. La arquitectura Go se detalla en [`go-implementation.md`](go-implementation.md).

```text
TUI planificada / CLI + manager nativo OpenCode implementados
                            |
 routing direct / Task de solo lectura / SDD del manager
                            |
 agentes SDD de solo lectura -> patch apply ligado por hash
                            |
 solo el manager escribe, valida, acepta y transiciona
                            |
 plugin v5 -> memoria semántica + SDD estructurado v5
```

| Límite | Estado y responsabilidad |
| --- | --- |
| Binario Go y composición | **Implemented:** Compilar un binario local y conectar configuración, inspección, memoria, Chronicle, Registry/Gatekeeper, proveedores, coordinador, setup y comandos del puente. |
| CLI, auto-instalación y raíces de almacenamiento | **Implemented:** Instalar versiones inmutables detrás de un launcher permanente, activar actualizaciones atómicamente, hacer rollback de un nivel, resolver almacenamiento de proyecto/persona y ofrecer status, doctor, memoria, setup guiado, integración y comandos delimitados del puente. |
| MemoryStore propio | **Implemented:** Almacenamiento semántico SQLite/FTS5, migraciones, campos de ciclo de vida, búsqueda filtrada, registros con procedencia, recuperación automática delimitada y candidatos propuestos por agentes bajo gobierno de VGXNESS. |
| Chronicle | **Implemented/Partial:** Implementa lectura estricta de la ejecución actual, eventos JSONL verificados, instantáneas activas inmutables direccionadas por contenido, commits atómicos del puntero, reparación terminal, reproducción del estado de tareas, evidencia de cancelación y reconstrucción delimitada de recuperación. La continuidad general de puntos de control/artefactos sigue planificada. |
| Esquemas y reglas semánticas | **Implemented/Contracts-only:** Paquetes, eventos, instantáneas, registros Registry, prompts, resultados y reglas SDD nativas se validan; las formas futuras de routing/continuidad neutrales entre proveedores siguen solo como contratos. |
| TUI mediante teclado | **Planned:** Ofrecer configuración e interacción enfocada sin controlar la política de instalación u orquestación. |
| Coordinación delimitada | **Implemented, acotada:** El manager elige trabajo directo, lectura delimitada o SDD nativo. Como máximo se solapan cuatro subtrabajos independientes de solo lectura; síntesis y escrituras son secuenciales. Los servicios de compatibilidad permanecen separados. |
| Adaptador OpenCode | **Implemented:** Manager v29, cinco revisores, seis perfiles SDD de solo lectura, plugin v5 y manifiesto low/medium/high forman 14 artefactos. El plan actual/predeterminado `medium` usa slots Luna Fast, Terra y Sol; cambiar plan o slot requiere reiniciar. |
| Adaptador CodeGraph | **Implemented, opcional:** El manager y los revisores pueden usar la herramienta MCP de solo lectura `codegraph_explore` para evidencia estructural delimitada cuando existe un índice saludable; la fuente exacta y el diff siguen siendo las autoridades y alternativas. |
| Adaptador OpenPencil | **Planned, opcional:** Ruta de diseño y prototipado; los artefactos son propuestas hasta implementarse y verificarse por separado. |
| Otros adaptadores de runtime/MCP | **Deferred:** Pueden agregarse sin cambiar los contratos centrales. |

**Non-goal:** Ningún adaptador puede omitir Gatekeeper, redefinir la taxonomía, convertirse en verdad operativa o incluir política que corresponde al plano de control.

## 7. Experiencia de usuario

### Asistente de configuración

**Implemented, headless:** El wizard detecta requisitos y rutas, explica los seis pasos, muestra cambios, solicita aprobación, instala los 14 artefactos mediante servicios, relee resultados, verifica el handshake y reporta recuperación. No afirma sondeos de disponibilidad de modelos en runtime. Una TUI más rica y el setup de adaptadores opcionales siguen planificados.

El asistente puede detectar OpenCode, CodeGraph y OpenPencil. Solo puede ofrecer la instalación de un adaptador opcional ausente después de informar fuente, versión, comando, destino, uso de red, cambios de configuración y reversión. Rechazar un adaptador opcional conserva una alternativa admitida. La detección no autoriza instalación ni inicialización.

**Non-goal:** La configuración no instalará paquetes, inicializará repositorios, modificará configuración ni sobrescribirá archivos de forma silenciosa, ni afirmará éxito sin evidencia de relectura.

### Interacción con Navigator

**Implemented/Partial:** El manager coincide con el idioma y elige trabajo directo, delegación Task nativa de solo lectura o SDD aprobado. Carga skills, puede consultar CodeGraph, realiza todas las ediciones y validación y usa cinco revisores de solo lectura. El almacenamiento, backends, modos, transiciones y controles de proyección SDD están entregados; la recuperación Chronicle enriquecida sigue planificada.

### Perfiles de autonomía

Los perfiles configuran el nivel de interrupción; no eliminan límites estrictos de seguridad.

| Perfil | Trabajo ordinario dentro del alcance | Postura de aprobación |
| --- | --- | --- |
| **Safe** | Las lecturas y el análisis avanzan; las ediciones, pruebas y comandos locales normalmente preguntan. | La opción más cautelosa para entornos desconocidos o regulados. |
| **Balanced (predeterminado)** | Lecturas, ediciones, pruebas enfocadas y comandos locales no destructivos avanzan sin preguntas repetidas. | Las operaciones importantes siempre preguntan; un riesgo ambiguo pregunta una vez. |
| **Autonomous** | Trabajo ordinario preaprobado más amplio puede avanzar dentro de raíces, herramientas y límites de riesgo explícitos. | Se mantienen los controles estrictos; el perfil no puede autorizarlos implícitamente. |
| **Custom** | La persona define una política con capacidades y límites identificados. | Una política inválida, incompleta o que se amplía por sí misma se cierra de forma segura. |

Siempre se requiere aprobación para acciones destructivas sobre archivos; historial Git, remotos, commits, pushes, releases o PR; instalación de paquetes; efectos externos o de red; secretos; mutación de configuración global o del runtime; y ampliación de permisos.

### Arrendamientos de capacidades

Un arrendamiento de capacidad es una concesión explícita y revocable para una unidad de trabajo. Registra identidad y texto de aprobación, raíces y herramientas permitidas, clases de operación, límite de riesgo, plazo e identificador de correlación. Aplica privilegio mínimo, expira al completar, cancelar, vencer o cambiar el contexto y no puede concederse ni renovarse a sí mismo. Exceder el alcance se rechaza o requiere una aprobación nueva. Cada uso es atribuible en Chronicle.

### Ruta de decisión de aprobación

Para cada operación propuesta, Gatekeeper evalúa las mismas preguntas en orden:

1. ¿La operación pertenece a la unidad de trabajo activa y a las raíces permitidas?
2. ¿La capacidad, el adaptador y la herramienta seleccionados declaran la operación necesaria?
3. ¿El perfil de autonomía permite esta operación ordinaria sin interrupción?
4. ¿Existe un arrendamiento válido cuando la política lo requiere?
5. ¿Un control estricto requiere aprobación humana nueva sin importar el perfil o arrendamiento?

Un rechazo incluye la condición fallida y la siguiente acción segura más pequeña. VGXNESS no convierte rechazos repetidos en consentimiento implícito y una aprobación para una unidad no crea precedente para otra.

## 8. Enrutamiento y SDD

**Implemented/Partial:** El manager nativo clasifica cada solicitud en la ruta adecuada más pequeña. El estado SDD estructurado se persiste; los registros de routing neutrales entre proveedores y sondeos de catálogo siguen planificados.

| Ruta | Uso |
| --- | --- |
| `direct` | Responder o realizar una acción pequeña de bajo riesgo sin un flujo de varias fases. |
| `explore` | Investigar incógnitas, estado actual, restricciones o viabilidad. |
| `plan` | Ruta del clasificador para un enfoque delimitado cuando la implementación no está autorizada o SDD completo es innecesario. No es el modo SDD `tasks`. |
| `sdd` | Ejecutar proposal, spec, design, `tasks`, apply, verify y archive según riesgo y política. |
| `recovery` | Conciliar trabajo interrumpido, inconsistente, bloqueado o fallido a partir de evidencia duradera. |

El modo operativo `tasks` pertenece a SDD y transforma requisitos y diseño aprobados en unidades de implementación. Una ruta `plan` puede finalizar con recomendaciones o un desglose ligero y no implica artefactos SDD aprobados.

### Preflight y almacenes de artefactos

**Implemented/Partial:** El manager ofrece SDD opcional y almacena ejecución `automatic` o `interactive` por cambio. El modo automático avanza gates validados sin pausas rutinarias; el interactivo se detiene en cada límite. El preflight/sondeo de catálogo neutral entre proveedores y una UX de recuperación más amplia siguen planificados.

**Implemented:** El almacén SDD estructurado admite `memory`, `openspec` y `hybrid` en tablas SQLite aisladas. Se instalan seis perfiles de fase de solo lectura y el contrato secuencial del manager. El manager escribe mediante herramientas OpenCode; el plugin no accede al filesystem. Engram no es backend ni adaptador planificado.

| Almacén visible | Mapeo del contrato | Entrega y comportamiento |
| --- | --- | --- |
| `memory` | `memory` → tablas SDD estructuradas | **Implemented:** cuerpos canónicos, cambios, artefactos, revisiones aceptadas inmutables y enlaces de entrada están aislados de las observaciones semánticas. |
| `openspec` | `openspec` | **Implemented, bounded:** el archivo bajo `openspec/changes/<safe-change-id>/` es canónico; SQLite conserva ruta, digest, identidad y enlaces, pero no el cuerpo. El manager usa herramientas del workspace y registra evidencia verificada. |
| `both` | `hybrid` | **Implemented, bounded:** memoria es canónica; render/compare deterministas y la reconciliación explícita de sobrescribir, inspeccionar o guardar como candidato evitan importaciones divergentes automáticas. |
| desactivado | `none` | **Contracts-only:** no accede a artefactos SDD cuando la política permite omitirlos. |

El orden durable entregado es `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Cada guardado y aceptación se limita a la fase actual, y cada transición exige su artefacto aceptado; las transiciones con OpenSpec también exigen una proyección vigente enlazada a la misma revisión y digest. Todos los perfiles SDD son de solo lectura, incluido el compositor apply enlazado por hash. Pueden solaparse como máximo cuatro investigaciones independientes de solo lectura; solo el manager escribe archivos, ejecuta validación, persiste artefactos y realiza transiciones de forma secuencial.

## 9. Contexto y persistencia

### Contexto reducido

**Partial:** La ruta delimitada del proveedor construye y aplica paquetes de ejecución reducidos con objetivo, alcance, rutas/herramientas permitidas, referencias exactas de habilidades, criterios de aceptación, estado de aprobación y contratos de retorno. Las cápsulas generales de continuidad Navigator/SDD y la carga de artefactos siguen **Planned**.

### Autoridad semántica y verdad operativa

| Asunto | Estado y propietario |
| --- | --- |
| Chronicle | **Implemented/Partial:** Lee el puntero activo; valida, anexa, revierte y relee eventos JSONL; deriva estado de tareas/cancelaciones; publica instantáneas activas mediante archivos inmutables direccionados por contenido y repara una publicación terminal interrumpida. Los puntos de control generales y la recuperación rica de artefactos siguen planificados. |
| MemoryStore de VGXNESS | **Implemented:** Autoridad semántica para observaciones duraderas tipadas con identidad, alcance, tema, procedencia, ciclo de vida, referencias, save/search/get, metadatos de sesión, recuperación/captura automática de tareas y `memoryCandidates` gobernados. Las actualizaciones contradictorias pasan a `needs_review`; la UX amplia de revisión humana sigue planificada. |
| SQLite/FTS5 | **Implemented:** Una base predeterminada esquema v5, vínculos de workspace y dominios semántico/SDD aislados por proyecto. Una apertura v4 de solo lectura no puede migrar; siga [Memoria nativa](memory.md#upgrade-migration-caveat) y nunca elimine la base. |
| Integración Engram | **Non-goal:** VGXNESS no instala, invoca, importa ni depende de Engram. |
| Raíces de proyecto/persona | **Implemented:** Resuelve almacenamiento operativo local en `.vgxness/` o global en `~/.vgxness/projects/<project-id>/`; la memoria semántica predeterminada se comparte en `~/.vgxness/memory.db`. |

Las entradas de memoria tienen identificador estable, tipo, tema, contenido, procedencia, marcas de tiempo, alcance, estado de ciclo de vida y referencias. La búsqueda comienza con filtros deterministas y FTS5; resúmenes y embeddings pueden complementar la recuperación más adelante sin sustituir registros fuente.

Chronicle y la memoria semántica pueden referenciarse, pero no sustituirse. Si el contexto semántico contradice un evento, comprobante o estado de ejecución, Chronicle controla la decisión operativa y se informa la inconsistencia. El `MemoryStore` propio es la única autoridad de memoria semántica.

### Ciclo de vida de la memoria semántica

| Etapa | Comportamiento del almacén propio |
| --- | --- |
| Captura | **Implemented:** Acepta una observación duradera tipada con fuente, alcance y evidencia; cada tarea delimitada terminal también guarda una observación idempotente del resultado enlazada con las observaciones que utilizó. |
| Normalización | **Implemented:** Asigna identidad estable, tema, marcas de tiempo, metadatos de ciclo de vida y referencias de fuente. |
| Recuperación | **Implemented:** Aplica filtros deterministas antes del ranking FTS5, hidrata como máximo tres registros dentro de un presupuesto fijo de contexto de ejecución y devuelve procedencia. |
| Comparación | **Planned:** Conserva relaciones compatibles, relacionadas, delimitadas, conflictivas o de sustitución sin borrar el historial silenciosamente. |
| Revisión | **Planned:** Muestra conocimiento obsoleto o pendiente de revisión antes de confiar en él como hecho actual. |
| Resumen | **Planned:** Crea resúmenes derivados que referencian las entradas fuente en lugar de sustituirlas. |
| Importación | **Non-goal para Engram:** El runtime activo no inspecciona, importa ni sincroniza datos de Engram. |

La retención, la supresión selectiva de datos, la exportación, la copia de seguridad y la migración siguen siendo servicios de aplicación explícitos. Las escrituras de memoria respetan el alcance del proyecto o de la persona y la política de secretos. Eliminar o reescribir conocimiento duradero es una acción de consecuencias relevantes y no puede ocultarse dentro de un arrendamiento de edición ordinaria.

### Recuperación y conflictos de autoridad

**Implemented/Partial:** La recuperación Chronicle reconstruye estado operativo delimitado desde el puntero actual, su instantánea inmutable ligada al digest, el historial validado de eventos, resultados de tareas y cancelaciones. Completa una instantánea terminal preparada tras una eliminación interrumpida del puntero y se cierra ante inconsistencias de digest o autoridad. Los puntos de control/artefactos generales y la recuperación de contexto semántico siguen planificados. La memoria semántica nunca repara ni avanza una ejecución por sí misma.

## 10. Capacidades, servicios y adaptadores opcionales

### Capacidades de producto

| Capacidad | Responsabilidad planificada | Límite estricto |
| --- | --- | --- |
| Navigator | Coordinar interacción, enseñanza, intención, enrutamiento, aprobaciones, selección de capacidades y resumen. | No omite Gatekeeper ni absorbe silenciosamente trabajo especializado sustancial. |
| Scout | Inspeccionar código, documentos, herramientas, decisiones previas e incógnitas; devolver hallazgos con fuente. | Solo lectura, salvo que un modo aprobado por separado conceda escrituras limitadas. |
| Blueprint | Producir propuestas, requisitos, escenarios, diseños, prototipos, tareas y unidades de trabajo. | No afirma que planes o prototipos están implementados. |
| Forge | Implementar o corregir una unidad aprobada con evidencia enfocada y límite de reversión. | Escribe solo en raíces permitidas y no puede aprobarse. |
| Sentinel | Verificar requisitos, contratos, pruebas, alcance, seguridad, resiliencia, legibilidad y evidencia de diseño. | No reescribe silenciosamente lo que juzga. |
| Challenger | Opcionalmente realizar revisión adversarial de afirmaciones importantes. | Es consultivo; no amplía alcance, aprueba ni sustituye evidencia de aceptación. |

### Servicios deterministas

| Servicio | Responsabilidad planificada | Límite estricto |
| --- | --- | --- |
| Registry | **Implemented, delimitado:** Resuelve agentes, habilidades, prompts, proveedores, versiones, fuentes, procedencia, checksums, capacidades, permisos y alcances exactos usados por el runtime actual. | Rechaza referencias no resueltas, ambiguas, obsoletas o fuera de alcance. |
| Chronicle | **Implemented/Partial:** Registra y valida eventos, instantáneas ligadas a digest, estado de tareas/cancelaciones, punteros activos atómicos, reparación terminal y proyecciones de recuperación. La continuidad amplia de puntos de control/artefactos sigue abierta. | Nunca inventa estado faltante ni se convierte en memoria semántica. |
| Gatekeeper | **Implemented, delimitado:** Aplica elegibilidad/salud del adaptador, operaciones, capacidades, permisos, raíces/herramientas, límites de riesgo, arrendamientos, aprobaciones y transiciones de tareas en la ruta del proveedor. | Se cierra de forma segura y nunca pide a un LLM improvisar política. |

### Adaptador de inteligencia estructural CodeGraph

CodeGraph es el adaptador opcional preferido de Scout, Blueprint y el clasificador de rutas para mapas de repositorio, símbolos, referencias, rutas de llamadas, dependencias y evidencia del radio de impacto. Registry detecta disponibilidad y versión; Gatekeeper valida la raíz del proyecto y el arrendamiento. La inicialización se realiza bajo demanda, se limita a proyectos reales y crea un índice separado por worktree. Las consultas registran adaptador/versión, raíz, vigencia del índice y procedencia de la alternativa.

CodeGraph ausente, rechazado, inválido u obsoleto no bloquea por sí mismo el trabajo admitido. VGXNESS informa la condición y recurre a inspección delimitada del sistema de archivos. No instala, inicializa, reindexa ni reutiliza silenciosamente el índice de otro worktree. El asistente puede ofrecer instalación y validación bajo el contrato de aprobación de la sección 7.

### Adaptador de diseño y prototipado OpenPencil

OpenPencil es un adaptador opcional de Blueprint y Sentinel para briefs de diseño, flujos, wireframes, prototipos, inspección de tokens y layout, y evidencia de diseño. La integración puede operar con editor en vivo, headless, CLI o MCP, seleccionada a partir de capacidades declaradas y no de suposiciones.

Gatekeeper limita raíces de archivos de diseño, exportaciones, uso de red y escrituras. Registry registra herramienta/versión y procedencia. Sentinel revisa accesibilidad, coherencia, tokens, layout, intención responsive, estados de interacción y trazabilidad a requisitos. Un prototipo aprobado no es código de producción: Forge requiere alcance, aprobación, pruebas y verificación separados. El asistente puede ofrecer instalación informada y aprobada; la ausencia conserva una alternativa mediante especificación textual y de diseño.

### Contrato común de estado de adaptadores

| Control | Evidencia requerida |
| --- | --- |
| Detección | Identidad de ejecutable/servicio, versión, fuente e interfaz compatible. |
| Elegibilidad | Capacidades requeridas, compatibilidad de política, raíces permitidas y arrendamiento activo. |
| Vigencia | Estado de índice, documento o sesión adecuado para el worktree y la tarea actuales. |
| Invocación | Modo exacto del adaptador, entradas delimitadas, procedencia y correlación con la unidad. |
| Relectura | Existe un resultado estructurado o artefacto y puede inspeccionarse independientemente. |
| Alternativa | Una ruta admitida y comunicada cuando el adaptador opcional se rechaza o no está saludable. |

Los fallos se clasifican como no disponible, incompatible, obsoleto, permiso rechazado, resultado inválido o interrumpido. El fallo de un adaptador opcional no autoriza automáticamente instalar, modificar ni ampliar alcance.

## 11. Salvaguardas de entrega y agentes en segundo plano

### Habilidades, permisos y procedencia

**Partial:** Registry resuelve identidad, versión, fuente, procedencia, checksum y alcance permitido exactos de habilidades antes del despacho delimitado. Un servicio completo de autoría/ciclo de vida de habilidades propias y la selección dirigida por Navigator siguen planificados; las paráfrasis de memoria nunca se convierten en autoridad.

Gatekeeper evalúa perfil activo, arrendamiento, estado del adaptador, raíces permitidas y riesgo de operación antes de ejecutar. Un perfil no omite un control estricto y un adaptador opcional no amplía la unidad de trabajo.

### Revisión, diseño y entrega

Cada unidad registra el resultado de una validación enfocada, un escenario real cuando existe un límite de runtime y un límite exacto de reversión. La automatización Git/worktree es explícita y requiere aprobación. Los presupuestos de revisión activan límites autónomos de unidades o una excepción de tamaño registrada. La entrega encadenada conserva diffs enfocados, destinos correctos, verificación independiente y reversión segura.

Las perspectivas de Sentinel incluyen riesgo, fiabilidad, resiliencia, legibilidad y, cuando existen artefactos de diseño, accesibilidad, coherencia, tokens, layout, estados y trazabilidad a requisitos. Los bucles de corrección y revisión adversarial tienen presupuestos finitos de intentos y evidencia.

### Evidencia de la unidad de trabajo

| Evidencia | Requisito |
| --- | --- |
| Validación enfocada | Registrar el comando o control determinista más pequeño que demuestra la unidad y su resultado exacto. |
| Escenario de runtime | Ejercitar un límite real de integración cuando existe; de lo contrario, explicar por qué no corresponde. |
| Prueba de alcance | Enumerar rutas modificadas, aprobaciones usadas y cualquier efecto generado o externo. |
| Límite de reversión | Identificar archivos, estado y comportamiento exactos que pueden quitarse sin revertir trabajo ajeno. |
| Identidad de revisión | Correlacionar requisitos, implementación o documentación, hallazgos y veredicto final. |

La evidencia pertenece a la misma unidad que el cambio. Una suite general aprobada no sustituye un control enfocado fallido y una excepción de tamaño no elimina corrección, paridad bilingüe ni controles de seguridad.

### Supervisión de agentes en segundo plano

Navigator solo inicia trabajo concurrente cuando es seguro. Cada tarea en segundo plano pertenece al manager, se vincula a una ejecución y propósito, puede cancelarse independientemente, tiene límites de tiempo/iteraciones, es de solo lectura, no delega, no aprueba ni avanza la ejecución principal y es consultiva hasta validarse e incorporarse deliberadamente.

**Non-goal:** Ningún agente puede ocultar fallos, ejecutarse indefinidamente, ampliar autoridad, aprobar su propia acción riesgosa ni convertir salida consultiva en estado aceptado automáticamente.

## 12. Hoja de ruta y alcance diferido

### Horizontes de entrega

La secuencia nativa **almacenamiento SDD estructurado → agentes de fase de solo lectura → apply/validación/ciclo propiedad del manager** está implementada. La siguiente secuencia es **recuperación Chronicle enriquecida → routing neutral/sondeos de catálogo → integración automática de delivery**. El smoke nativo de Windows queda diferido.

| Horizonte | Estado | Resultado |
| --- | --- | --- |
| Base local del producto | **Implemented** | Composición Go, auto-instalación/actualización/rollback, almacenamiento semántico/SDD esquema v5, CLI, puente de compatibilidad, status/doctor, pruebas y CI. |
| Base de contratos de runtime | **Implemented/Contracts-only** | Valida contratos y reglas SDD usados por rutas entregadas; conserva formas futuras de routing/continuidad neutrales entre proveedores solo como contratos. |
| Estado operativo Chronicle | **Implemented/Partial** | Entrega eventos verificados, reproducción de tareas/cancelaciones, instantáneas ligadas a digest, publicación atómica del puntero, reparación terminal y proyección de recuperación; falta ampliar la continuidad general de puntos de control/artefactos. |
| Base SDD nativa | **Implemented/Partial** | Entrega backends, modos, revisiones/enlaces aislados, proyección OpenSpec, escrituras del manager, agentes de solo lectura y lecturas paralelas delimitadas; falta recuperación Chronicle enriquecida y routing neutral/sondeos de catálogo. |
| Plano de control de compatibilidad | **Implemented** | Conserva bridge/orchestrate/ticket/wave/edit/Delivery Authority para CLI y mantenimiento sin proyectarlos como scheduler activo. |
| Adaptadores estructurales y de diseño | **Partial/Planned** | La evidencia CodeGraph opcional y delimitada está implementada; sondeos neutrales amplios y OpenPencil siguen planificados. |
| Entrega segura | **Partial** | Existen recibos/gates de compatibilidad y límites estrictos de revisión nativa; la integración automática sigue planificada. |
| Expansión del ecosistema | **Deferred** | Agregar runtimes elegibles además de OpenCode, MCP local opcional, más clientes, recuperación semántica avanzada y superficies gráficas cuando los contratos sean estables. |

### No objetivos explícitos

- Copiar código, prompts, esquemas, habilidades, nombres, disposiciones o flujos exactos de otro sistema.
- Acciones destructivas, instalaciones, commits, pushes, releases, efectos externos o mutación de configuración silenciosos.
- Tratar un perfil o arrendamiento como permiso permanente sin límites.
- Requerir CodeGraph, OpenPencil o un runtime específico para el funcionamiento central de VGXNESS.
- Tratar prototipos como producción o memoria semántica como verdad operativa.
- Sincronización multiusuario o planificación distribuida sin una decisión futura de alcance.
- Convertir dashboard, asistente o TUI en propietario de orquestación, instalación, memoria o permisos.

### Trazabilidad de la visión

Este es un mapa de revisión, no un sustituto de las definiciones anteriores.

| Área acordada | Autoridad en el plan maestro | Clasificación |
| --- | --- | --- |
| Resultado, estado, canonicidad y contrato bilingüe | Secciones 1-2 | **Implemented**, **Partial**, **Contracts-only** y **Planned** están respaldados por evidencia; el inglés es canónico. |
| Control humano, enseñanza, orientación crítica e idioma | Secciones 3 y 7 | **Planned**. |
| Paridad limpia, procedencia y copia prohibida | Sección 4 | Regla documental **Implemented**; copiar es **Non-goal**. |
| Capacidades, servicios, modos y adaptadores | Secciones 5, 6 y 10 | Routing del manager y roles SDD delimitados están **Implemented**; routing neutral entre proveedores sigue **Planned**. |
| Plano Go, estado local, TUI, CLI y OpenCode | Secciones 6-7 | Go/CLI/almacenamiento/SDD nativo OpenCode están **Implemented**; orquestación de compatibilidad queda separada; TUI **Planned**. |
| Safe, Balanced, Autonomous, Custom y arrendamientos | Secciones 7 y 11 | **Planned**; se mantienen controles estrictos. |
| `direct`, `explore`, `plan`, `sdd`, `recovery`; `plan` frente a `tasks` | Sección 8 | Selección nativa **Implemented/Partial**; persistencia neutral planificada; términos distintos. |
| Preflight automático/interactivo y backends de artefactos | Sección 8 | Modos por cambio y `memory`/`openspec`/`hybrid` están **Implemented**. |
| Paquetes pequeños y cápsulas de continuidad | Sección 9 | Los paquetes de ejecución delimitados están **Implemented**; las cápsulas generales de continuidad siguen **Planned**. |
| Verdad operativa Chronicle y JSON/JSONL legible | Secciones 6 y 9 | Eventos, instantáneas ligadas a digest, reproducción de tareas, punteros atómicos, reparación terminal y proyección de recuperación están entregados; la continuidad amplia de puntos de control/artefactos mantiene Chronicle **Partial**. |
| Autoridad semántica SQLite/FTS5 propia y alcance del conocimiento duradero | Sección 9 | Base save/search/get **Implemented**; ciclo avanzado **Planned**. |
| Sin integración Engram en runtime | Secciones 2, 6 y 9 | **Non-goal**. |
| Inteligencia estructural CodeGraph e instalación por asistente | Secciones 7 y 10 | Evidencia delimitada de solo lectura **Implemented, opcional**; instalación por asistente **Planned**. |
| Diseño/prototipado OpenPencil e instalación por asistente | Secciones 7, 10 y 11 | **Planned, opcional**; sin promoción automática. |
| Habilidades, resolución exacta, aprobaciones, revisiones y entrega | Sección 11 | La resolución/aprobación/evidencia de revisión delimitada es **Partial**; el ciclo completo de habilidades y la automatización de entrega siguen **Planned**. |
| Fallos, cancelación, recuperación y supervisión en segundo plano | Secciones 3, 9 y 11 | Cancelación delimitada y supervisión de solo lectura están **Implemented**; recuperación Chronicle enriquecida de SDD interrumpido sigue **Planned**. |
| Esquemas Draft 2020-12 y límite de validación | Secciones 1-2 y [`schemas/README.md`](schemas/README.md) | Las rutas actuales de runtime se validan; las formas solo futuras son **Contracts-only** y no se afirma validación completa de release. |
| Base de runtime entregada | Secciones 1-2 y este mapa | Binario, almacenamiento semántico/SDD v5, ciclo del manager, agentes de fase/revisión de solo lectura, autorización del plugin, backends/modos/proyección, setup OpenCode, subsistemas CLI de compatibilidad, pruebas y CI están **Implemented**; recuperación enriquecida, routing neutral/sondeos de catálogo y delivery automático siguen **Planned**. |

## Documentos complementarios

- [`../README.md`](../README.md) — estado del repositorio y entrada bilingüe a la documentación.
- [`go-implementation.md`](go-implementation.md) — paquetes entregados del plano de control Go, límites parciales Chronicle/Windows nativo, extensiones planificadas, interfaces, almacenamiento y evidencia de pruebas.
- [`orchestration-flow.md`](orchestration-flow.md) — ciclo SDD nativo activo, límite del plano de control de compatibilidad y extensiones planificadas de recuperación/routing/delivery.
- [`schemas/README.md`](schemas/README.md) — índice de contratos legibles por máquinas y guía de validación disponible; los contratos no implican entrega de runtime.
