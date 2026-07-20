# Plan maestro de producto de VGXNESS

> **Versión del plan maestro:** 1.0  
> **Estado del documento:** Current  
> **Autoridad:** Traducción al español no canónica  
> **Fuente canónica:** [English — canonical product blueprint](product-blueprint.md)

Este documento es una traducción completa para navegación y comprensión. El documento en inglés es la única fuente canónica de la visión, el vocabulario, los límites y la hoja de ruta de VGXNESS. Los documentos complementarios describen contratos y flujos de trabajo más acotados; no redefinen este plan maestro. Si la versión en español difiere, prevalece la versión en inglés.

Las etiquetas de estado tienen significados precisos:

- **Current (Actual)** — presente y verificable ahora en este repositorio.
- **Planned (Planificado)** — dirección de producto aprobada que todavía no está implementada.
- **Deferred (Diferido)** — dirección aprobada que queda intencionalmente fuera del primer horizonte de entrega.
- **Non-goal (No objetivo)** — comportamiento excluido que requiere una nueva decisión de alcance antes de ingresar en la hoja de ruta.

## 1. Propósito y audiencia

VGXNESS está dirigido a desarrolladores que desean que el trabajo asistido por IA siga siendo comprensible, delimitado y recuperable. Está destinado a convertirse en un plano de control con prioridad local que coordina agentes, habilidades, memoria semántica, estado operativo, herramientas estructurales y de diseño, y salvaguardas de entrega sin ocultar decisiones importantes dentro de prompts.

**Current:** El repositorio ofrece documentación de producto de autoría independiente y esquemas de contrato Draft 2020-12. No ofrece un producto ejecutable. La conformidad de los esquemas puede comprobarse con validadores compatibles, pero todavía no existen un validador completo de entregas de VGXNESS ni una aplicación integral de extremo a extremo en tiempo de ejecución.

**Planned:** VGXNESS guiará silenciosamente el trabajo ordinario dentro del alcance y conservará evidencia de enrutamiento, aprobaciones, artefactos, validación y recuperación. La persona define la intención y aprueba las acciones importantes; el sistema facilita planificar, ejecutar, revisar y reanudar trabajo delimitado.

### Contrato del documento y la traducción

- El inglés es la fuente de decisiones y resuelve las ambigüedades.
- Esta versión en español es completa para navegación y comprensión, pero explícitamente no canónica.
- Ambos documentos mantienen la misma versión, modelo de estados, inventario de secciones numeradas, decisiones, controles, significado de la hoja de ruta y temas de trazabilidad.
- Un cambio semántico en la versión canónica debe actualizar ambos documentos de forma atómica. Una diferencia bloquea la publicación.
- Los documentos complementarios enlazan la fuente canónica en lugar de mantener hojas de ruta competidoras.

## 2. Estado general

| Estado | Alcance |
| --- | --- |
| **Current** | Documentación y esquemas JSON Draft 2020-12 neutrales respecto del runtime para orquestación, ejecución, registros, instantáneas de ejecución y eventos de solo anexado. |
| **Planned** | Un plano de control Go instalado globalmente; configuración mediante teclado; capacidades delimitadas; almacenamiento operativo Chronicle; memoria semántica propia SQLite/FTS5; aprobaciones; validación; recuperación; entrega segura; y adaptadores opcionales de runtime, inteligencia estructural, diseño, MCP y compatibilidad. |
| **Deferred** | Adaptadores de runtime adicionales, exposición MCP local opcional, más clientes MCP externos, recuperación semántica avanzada y superficies gráficas de producto. |
| **Non-goal** | Artefactos copiados de terceros, autonomía destructiva silenciosa, verdad operativa oculta, dependencia rígida de un runtime o herramienta, bucles de agentes sin límites, promoción automática de prototipos a producción o una interfaz propietaria de la política de negocio. |

**Current:** Este repositorio no entrega código Go, binario, instalador, catálogo de habilidades incluido, adaptador de runtime, servidor o cliente MCP, implementación de persistencia, automatización Git ni mutación de configuración del producto.

**Current — limitación:** Los esquemas bajo [`schemas/`](schemas/README.md) son contratos actuales para implementaciones futuras. Sus declaraciones `$schema` y su compatibilidad con herramientas Draft 2020-12 no demuestran corrección semántica completa ni aplicación en tiempo de ejecución.

## 3. Principios de producto

1. **La persona dirige.** La persona usuaria controla objetivos, alcance y aprobación de acciones importantes.
2. **Enseñar, no ocultar.** Explicar hechos, recomendaciones, compensaciones e incertidumbre relevantes.
3. **Verificar antes de aceptar.** Las afirmaciones sobre código, herramientas, estado y finalización requieren evidencia.
4. **Coordinar mediante límites.** La orquestación, ejecución, revisión, almacenamiento, habilidades, permisos y adaptadores siguen siendo separables.
5. **Mantener el estado inspeccionable.** Chronicle registra la verdad operativa; la memoria semántica conserva significado duradero.
6. **Usar la ruta capaz más pequeña.** El trabajo simple permanece directo; la complejidad y el riesgo reciben planificación y validación proporcionales.
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
| **Current — regla de autoría** | La prosa y los esquemas de este repositorio son contratos propios de VGXNESS y mantienen autoría independiente. |
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
| Adaptadores | Traducciones reemplazables entre el plano de control y runtimes, herramientas, protocolos o almacenes externos. | OpenCode, CodeGraph, OpenPencil, Engram, MCP y adaptadores futuros. |

**Planned:** Los nombres de capacidades describen responsabilidades estables del producto, no un proceso por nombre ni archivos de prompts específicos de un proveedor. `sdd-design` es un modo de Blueprint, `sdd-apply` es un modo de Forge y `sdd-verify` es un modo de Sentinel.

**Current:** Los términos de los esquemas existentes siguen siendo normativos para registros legibles por máquinas. Este plan maestro controla la taxonomía para personas, no las definiciones de campos de los esquemas.

## 6. Modelo del sistema

**Planned:** VGXNESS será un plano de control Go con prioridad local, instalado globalmente y con límites explícitos de paquetes y dependencias. La arquitectura Go se detalla en [`go-implementation.md`](go-implementation.md).

```text
TUI mediante teclado / CLI / MCP local opcional
                       |
              servicios de aplicación
                       |
 Navigator + capacidades y modos delimitados
                       |
          Registry + Chronicle + Gatekeeper
                       |
 MemoryStore propio + adaptadores externos opcionales
```

| Límite | Estado y responsabilidad |
| --- | --- |
| Plano de control Go | **Planned:** Controlar política de aplicación, orquestación, servicios de instalación, validación y composición. |
| TUI mediante teclado | **Planned:** Ofrecer configuración e interacción enfocada sin controlar la política de instalación u orquestación. |
| CLI | **Planned:** Ofrecer inspección, recuperación y automatización; es una comodidad, no una dependencia. |
| Adaptador OpenCode | **Planned:** Primer adaptador de runtime preferido, elegido solo cuando supera controles de capacidad y política. |
| Adaptador CodeGraph | **Planned, opcional:** Ruta preferida de inteligencia estructural cuando está saludable y aprobado; se mantiene el análisis del sistema de archivos. |
| Adaptador OpenPencil | **Planned, opcional:** Ruta de diseño y prototipado; los artefactos son propuestas hasta implementarse y verificarse por separado. |
| MemoryStore propio | **Planned desde la base inicial:** Autoridad semántica SQLite/FTS5 bajo control de VGXNESS. |
| Archivos Chronicle | **Planned:** Instantáneas legibles, eventos JSONL, comprobantes, artefactos y evidencia de recuperación. |
| Otros adaptadores de runtime/MCP | **Deferred:** Pueden agregarse sin cambiar los contratos centrales. |

**Non-goal:** Ningún adaptador puede omitir Gatekeeper, redefinir la taxonomía, convertirse en verdad operativa o incluir política que corresponde al plano de control.

## 7. Experiencia de usuario

### Asistente de configuración

**Planned:** Un asistente mediante teclado detectará requisitos y rutas, explicará integraciones opcionales, mostrará los cambios propuestos, solicitará la aprobación necesaria, respaldará configuraciones existentes, realizará la instalación aprobada mediante servicios de aplicación, volverá a leer los resultados y ofrecerá acciones de reintento, reparación o reversión.

El asistente puede detectar OpenCode, CodeGraph y OpenPencil. Solo puede ofrecer la instalación de un adaptador opcional ausente después de informar fuente, versión, comando, destino, uso de red, cambios de configuración y reversión. Rechazar un adaptador opcional conserva una alternativa admitida. La detección no autoriza instalación ni inicialización.

**Non-goal:** La configuración no instalará paquetes, inicializará repositorios, modificará configuración ni sobrescribirá archivos de forma silenciosa, ni afirmará éxito sin evidencia de relectura.

### Interacción con Navigator

**Planned:** Navigator coincide con el idioma de la persona, diferencia hechos de recomendaciones, formula una pregunta bloqueante por vez, explica decisiones importantes, mantiene concisa la ruta normal y selecciona una capacidad delimitada en lugar de realizar todos los roles.

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

**Planned:** Navigator clasifica cada solicitud en la ruta adecuada más pequeña y persiste una decisión explicable.

| Ruta | Uso |
| --- | --- |
| `direct` | Responder o realizar una acción pequeña de bajo riesgo sin un flujo de varias fases. |
| `explore` | Investigar incógnitas, estado actual, restricciones o viabilidad. |
| `plan` | Ruta del clasificador para un enfoque delimitado cuando la implementación no está autorizada o SDD completo es innecesario. No es el modo SDD `tasks`. |
| `sdd` | Ejecutar proposal, spec, design, `tasks`, apply, verify y archive según riesgo y política. |
| `recovery` | Conciliar trabajo interrumpido, inconsistente, bloqueado o fallido a partir de evidencia duradera. |

El modo operativo `tasks` pertenece a SDD y transforma requisitos y diseño aprobados en unidades de implementación. Una ruta `plan` puede finalizar con recomendaciones o un desglose ligero y no implica artefactos SDD aprobados.

### Preflight y almacenes de artefactos

**Planned:** El preflight automático utiliza política y evidencia del repositorio, y solo pregunta ante una ambigüedad importante. El preflight interactivo explica la decisión relevante antes del trabajo de fase.

**Requisito previo de migración del esquema:** El esquema actual de preflight SDD acepta `engram`, `openspec`, `hybrid` y `none`. Su token `engram` es transitorio y no puede representar con claridad el valor predeterminado planificado de memoria propia. Antes de la primera implementación en Go, el esquema debe agregar un token de backend `memory` neutral respecto del proveedor/configurado; el SDD requerido con el `MemoryStore` propio no puede publicarse hasta entonces. Engram sigue siendo solo un adaptador opcional de compatibilidad/importación.

| Almacén visible | Backend planificado tras la migración | Comportamiento |
| --- | --- | --- |
| `memory` | `memory` → `MemoryStore` configurado | Persiste artefactos SDD mediante referencias de memoria inmutables. La memoria propia de VGXNESS es el valor predeterminado planificado; esta fila solo pasa a ser válida para el contrato después de la migración requerida, no al renombrar la memoria propia como `engram`. |
| `openspec` | `openspec` | Persiste artefactos en la estructura OpenSpec del repositorio. |
| `both` | `hybrid` | Mantiene sincronizados memoria y artefactos del sistema de archivos. |
| desactivado | `none` | No accede a artefactos SDD cuando la política permite omitirlos. |

El orden de fases se basa en evidencia, no en ceremonia. Una fase solo se omite cuando su artefacto o decisión requerida existe y sigue siendo válida. Apply sigue requisitos y diseño aprobados; verify demuestra el resultado; archive cierra y sincroniza el estado final.

## 9. Contexto y persistencia

### Contexto reducido

**Planned:** Cada tarea delimitada recibe objetivo, alcance, rutas y herramientas permitidas, referencias inmutables de artefactos, referencias exactas de habilidades, criterios de aceptación, estado de aprobación y contrato de retorno. Una cápsula de continuidad conserva decisiones, referencias de estado, procedencia, acciones completadas y siguientes, bloqueos y guía de recuperación sin copiar la transcripción.

### Autoridad semántica y verdad operativa

| Asunto | Estado y propietario |
| --- | --- |
| Chronicle | **Planned:** Autoridad operativa para eventos, estado de ejecución, instantáneas, comprobantes, aprobaciones, referencias de artefactos, puntos de control, resultados, cancelaciones y reproducción. |
| MemoryStore de VGXNESS | **Planned desde la base Go inicial:** Autoridad semántica para conocimiento duradero: decisiones, preferencias, convenciones, descubrimientos, causas de errores, restricciones, aprobaciones y su justificación, lecciones, resúmenes, cápsulas de continuidad y referencias de artefactos. |
| SQLite/FTS5 | **Planned desde la base Go inicial:** Persistencia local propia y recuperación léxica, introducida gradualmente detrás de `MemoryStore`; puede seguir una indexación semántica avanzada. |
| Adaptador Engram | **Planned, opcional:** Puente de compatibilidad, importación y referencia. Puede conservar identificadores externos y procedencia, pero no controla la semántica de VGXNESS. |
| Raíces de proyecto/persona | **Planned:** Política explícita de almacenamiento local del proyecto en `.vgxness/` o global de la persona usuaria en `~/.vgxness/projects/<project-id>/`. |

Las entradas de memoria tienen identificador estable, tipo, tema, contenido, procedencia, marcas de tiempo, alcance, estado de ciclo de vida y referencias. La búsqueda comienza con filtros deterministas y FTS5; resúmenes y embeddings pueden complementar la recuperación más adelante sin sustituir registros fuente.

Chronicle y la memoria semántica pueden referenciarse, pero no sustituirse. Si el contexto semántico contradice un evento, comprobante o estado de ejecución, Chronicle controla la decisión operativa y se informa la inconsistencia. Si Engram está ausente, se rechaza o no está disponible, la memoria propia sigue siendo plenamente utilizable y autoritativa.

### Ciclo de vida de la memoria semántica

| Etapa | Comportamiento del almacén propio |
| --- | --- |
| Captura | Aceptar una observación duradera tipada con fuente, alcance y evidencia; rechazar ruido operativo sin procesar. |
| Normalización | Asignar identidad estable, tema, marcas de tiempo, metadatos de ciclo de vida y referencias inmutables de fuente. |
| Recuperación | Aplicar filtros de alcance/tipo/ciclo antes del ranking FTS5 y devolver procedencia con cada resultado. |
| Comparación | Conservar relaciones compatibles, relacionadas, delimitadas, conflictivas o de sustitución sin borrar el historial silenciosamente. |
| Revisión | Mostrar conocimiento obsoleto o pendiente de revisión antes de confiar en él como hecho actual. |
| Resumen | Crear resúmenes derivados que referencian las entradas fuente en lugar de sustituirlas. |
| Importación | Traducir registros Engram opcionales con ID de fuente y procedencia de importación; nunca sobrescribir autoridad silenciosamente. |

La retención, la supresión selectiva de datos, la exportación, la copia de seguridad y la migración siguen siendo servicios de aplicación explícitos. Las escrituras de memoria respetan el alcance del proyecto o de la persona y la política de secretos. Eliminar o reescribir conocimiento duradero es una acción de consecuencias relevantes y no puede ocultarse dentro de un arrendamiento de edición ordinaria.

### Recuperación y conflictos de autoridad

La recuperación primero reconstruye el estado operativo desde instantáneas, eventos, comprobantes, artefactos y cancelaciones de Chronicle. Luego recupera contexto semántico para explicar intención, restricciones, lecciones y siguientes acciones esperadas. Si las fuentes difieren, VGXNESS registra una referencia del conflicto, conserva el último estado operativo válido y solicita corrección o aprobación cuando la evidencia no lo resuelve. Un resumen semántico nunca repara ni avanza una ejecución por sí mismo.

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
| Registry | Resolver agente, habilidad, adaptador, versión, fuente, procedencia, capacidad, permiso y alcance exactos. | Rechaza referencias no resueltas o fuera de alcance. |
| Chronicle | Registrar hechos operativos correlacionados y exponer estado consistente para inspección y recuperación. | Nunca inventa estado faltante ni se convierte en memoria semántica. |
| Gatekeeper | Aplicar elegibilidad, esquemas, permisos, arrendamientos, aprobaciones, raíces/herramientas, presupuestos de bucles y transiciones. | Se cierra de forma segura y nunca pide a un LLM improvisar política. |

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

**Planned:** Las habilidades propias de autoría independiente son contratos de comportamiento versionados. Registry resuelve identidad, versión, fuente, procedencia, activador y alcance permitido antes del despacho. Navigator pasa una referencia resuelta o payload congelado; no presenta como autoridad una paráfrasis de memoria.

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

| Horizonte | Estado | Resultado |
| --- | --- | --- |
| Base de contratos | **Current** | Mantener documentación bilingüe de autoría independiente y esquemas Draft 2020-12 como contratos revisables, sin afirmar una validación completa de una entrega. |
| Base local del producto | **Planned** | Construir composición Go, resolución de almacenamiento, Chronicle, Gatekeeper, `MemoryStore` SQLite/FTS5 propio, inspección CLI y configuración mediante teclado con recuperación mediante copia de seguridad y relectura. |
| Orquestación delimitada | **Planned** | Agregar enrutamiento Navigator, paquetes pequeños, modos operativos, resolución Registry, aprobaciones, continuidad y compatibilidad Engram opcional. |
| Adaptadores estructurales y de diseño | **Planned** | Agregar detección opcional de CodeGraph y OpenPencil, instalación por asistente, procedencia, alternativas seguras y validación Sentinel enfocada. |
| Entrega segura | **Planned** | Agregar ciclo de vida de habilidades, soporte de worktrees/unidades de trabajo, presupuestos de revisión, salvaguardas Git, trabajo supervisado en segundo plano, revisiones delimitadas y recuperación. |
| Expansión del ecosistema | **Deferred** | Agregar runtimes elegibles además de OpenCode, MCP local opcional, más clientes, recuperación semántica avanzada y superficies gráficas cuando los contratos sean estables. |

### No objetivos explícitos

- Copiar código, prompts, esquemas, habilidades, nombres, disposiciones o flujos exactos de otro sistema.
- Acciones destructivas, instalaciones, commits, pushes, releases, efectos externos o mutación de configuración silenciosos.
- Tratar un perfil o arrendamiento como permiso permanente sin límites.
- Requerir CodeGraph, OpenPencil, Engram o un runtime específico para el funcionamiento central de VGXNESS.
- Tratar prototipos como producción o memoria semántica como verdad operativa.
- Sincronización multiusuario o planificación distribuida sin una decisión futura de alcance.
- Convertir dashboard, asistente o TUI en propietario de orquestación, instalación, memoria o permisos.

### Trazabilidad de la visión

Este es un mapa de revisión, no un sustituto de las definiciones anteriores.

| Área acordada | Autoridad en el plan maestro | Clasificación |
| --- | --- | --- |
| Resultado, estado, canonicidad y contrato bilingüe | Secciones 1-2 | Documentación **Current**; producto **Planned**. |
| Control humano, enseñanza, orientación crítica e idioma | Secciones 3 y 7 | **Planned**. |
| Paridad limpia, procedencia y copia prohibida | Sección 4 | Regla **Current**; copiar es **Non-goal**. |
| Capacidades, servicios, modos y adaptadores | Secciones 5, 6 y 10 | Taxonomía **Planned**. |
| Plano Go, estado con prioridad local, TUI, CLI y OpenCode | Secciones 6-7 | **Planned**. |
| Safe, Balanced, Autonomous, Custom y arrendamientos | Secciones 7 y 11 | **Planned**; se mantienen controles estrictos. |
| `direct`, `explore`, `plan`, `sdd`, `recovery`; `plan` frente a `tasks` | Sección 8 | **Planned** y explícitamente distintos. |
| Preflight automático/interactivo y backends de artefactos | Sección 8 | **Planned**. |
| Paquetes pequeños y cápsulas de continuidad | Sección 9 | **Planned**. |
| Verdad operativa Chronicle y JSON/JSONL legible | Secciones 6 y 9 | **Planned**. |
| Autoridad semántica SQLite/FTS5 propia y alcance del conocimiento duradero | Sección 9 | **Planned desde la base inicial**. |
| Adaptador opcional Engram de compatibilidad/importación/referencia | Secciones 6 y 9 | **Planned, opcional**. |
| Inteligencia estructural CodeGraph e instalación por asistente | Secciones 7 y 10 | **Planned, opcional**, con alternativa. |
| Diseño/prototipado OpenPencil e instalación por asistente | Secciones 7, 10 y 11 | **Planned, opcional**; sin promoción automática. |
| Habilidades, resolución exacta, aprobaciones, revisiones y entrega | Sección 11 | **Planned**. |
| Fallos, cancelación, recuperación y supervisión en segundo plano | Secciones 3, 9 y 11 | **Planned**. |
| Esquemas Draft 2020-12 y límite de validación | Secciones 1-2 y [`schemas/README.md`](schemas/README.md) | Contratos **Current**; no se afirma la validación completa de una entrega. |
| Entrega documental del plan maestro | Secciones 1-2 y este mapa | **Current**; no se entrega capacidad de runtime. |

## Documentos complementarios

- [`../README.md`](../README.md) — estado del repositorio y entrada bilingüe a la documentación.
- [`go-implementation.md`](go-implementation.md) — paquetes Go, interfaces, almacenamiento y límites de pruebas planificados.
- [`orchestration-flow.md`](orchestration-flow.md) — ciclo de solicitud, controles, modos, autoridad de memoria y recuperación planificados.
- [`schemas/README.md`](schemas/README.md) — índice actual de contratos legibles por máquinas y guía de validación disponible.
