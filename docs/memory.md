# Memoria nativa y almacenamiento

El almacenamiento de VGXNESS es un subsistema en Go dentro del mismo proceso,
respaldado por una sola base de datos SQLite/FTS5 propia. La memoria semántica
expone `Remember`, `Recall`, `Recent`, `Get` y `Forget`. No necesita demonio,
segundo binario, embeddings ni servicio de red. Las tablas históricas de SDD
siguen existiendo, inertes, por compatibilidad de la base de datos; no hay
servicio, herramienta ni comando de archivo para ellas.

## Frontera estricta, núcleo flexible

JSON es un formato de frontera de confianza, no un contrato interno. La CLI
mantiene la versión de schema 1 del payload, rechaza campos desconocidos y
duplicados, acepta como máximo 64 KiB y rechaza fuentes en conflicto entre
flags y payload. Tras decodificar, los adaptadores construyen valores nativos
(`memory.Remember`, `memory.Recall`, `memory.Recent`, `memory.Lookup`,
`memory.Forget`) y las llamadas internas usan esos valores directamente.

`memory_search` en MCP acepta `match_mode` opcional: omitido o `all` exige
todos los términos; `any` acepta cualquiera. Un valor inválido falla antes de
consultar. La CLI conserva su campo nativo `matchAny`.

`memory recent` resuelve el proyecto canónico desde `--workspace`. Devuelve las
observaciones activas de alcance proyecto, ordenadas por actualización más
reciente con desempate por ID, con la misma vista previa acotada que la búsqueda
y sin el contenido completo.

`Forget` es una operación de ciclo de vida: marca la observación como archivada
y elimina su fila FTS en una sola transacción. La fila y sus relaciones siguen
disponibles para `Get` como historia durable; `Recall` ya no la devuelve.

## Dominios del schema v23

El schema v23 de SQLite guarda observaciones semánticas, referencias, sesiones
y filas FTS. Las migraciones publicadas y las tablas históricas se conservan
para abrir bases existentes sin borrar historia; ningún código en ejecución las
lee ni escribe, y no aparecen en recall ni en sincronización.

**Sesiones de proveedor y handoff.** Cada sesión de Claude Code abre una sesión
local de proveedor con un lease opaco de 24 horas. `Start` toma la misma
identidad externa de forma atómica, rota el token del lease y preserva el
handle y el borrador local. Antes de iniciar, se interrumpen como máximo 128
sesiones del mismo proyecto vencidas o sin lease y se eliminan sus borradores.
`Checkpoint` y el cierre terminal requieren el token actual; las transiciones
terminales limpian el lease. Los leases son locales: no se sincronizan ni se
exponen por MCP. El cierre con estado `completed` convierte el borrador
(`memory_session_summary`) en una observación de tipo `summary`, que es lo que
la siguiente sesión recibe como handoff acotado (4.096 caracteres) y marcado
como datos no confiables.

**Enrolamiento de sync.** Usa un marcador durable y acotado de la credencial
anterior para terminar la limpieza del keyring en el siguiente enrolamiento;
nunca guarda un bearer en SQLite. En Linux y macOS existe el modo explícito
`memory sync --credential-file /ruta/absoluta/privada` para uso sin escritorio;
el archivo se revalida en cada uso y no se persiste (ver
[sync](sync.md#enrolamiento-local-y-estado)). Para datos previos, ejecuta
`memory sync backfill --workspace /ruta/absoluta` antes de la primera
sincronización; es local, acotado e idempotente.

**Sync en primer plano por proyecto.** `memory sync --workspace
/ruta/absoluta` exige que el workspace tenga un marcador `.vgxness/project-id`
válido y la vinculación portable local creada por `memory project init`; si no,
falla cerrado antes de cualquier llamada remota. Empuja solo las mutaciones de
proyecto, sesión y observación de ese proyecto y trae solo su historia con un
cursor propio. Nunca hace bootstrap, no resuelve conflictos globales ni toca el
cursor global del propietario. Su modo de resultado es `project_bidirectional`.

La base por defecto es `~/.vgxness/memory.db`. `--storage-root` y
`--project-local` usan bases aisladas. MCP no tiene autoridad de planificación,
delegación, edición ni ejecución: solo expone la memoria.

## Actualización y migraciones

Una apertura de solo lectura no puede migrar un schema anterior a v23. Justo
después de actualizar el binario, `status`, `doctor` u otra lectura pueden
reportar un fallo de migración o almacenamiento. No borres ni recrees la base:
los datos existentes son la fuente de la migración.

Para actualizar con seguridad, detén otros escritores locales y conserva una
copia fuera de línea con tu procedimiento normal de respaldo. Luego ejecuta una
operación de memoria con escritura. El migrador toma la propiedad de escritura
con `BEGIN IMMEDIATE`, aplica todas las migraciones pendientes y su `PRAGMA
user_version` en una transacción, y la revierte ante cualquier error. Reintenta
un error transitorio de SQLite ocupado o bloqueado dentro del contexto de la
operación; no degrada una base cuyo schema sea más nuevo que el binario. Al
terminar, repite el comando de lectura para confirmar la migración reportada.

Si el escritor reporta un fallo o queda pendiente, conserva la base y la salida
del error, asegúrate de que ningún otro proceso tenga el archivo y de que la
ubicación sea escribible, y reintenta la misma escritura. No reinicies
`user_version`, no reproduzcas SQL a mano ni reemplaces la base por un archivo
vacío. Si persiste, escala con la copia preservada.

### Alcance de doctor

`doctor` tiene el mismo alcance local que `status`: resuelve las rutas de
almacenamiento seleccionadas y pregunta la versión del schema a la comprobación
de salud configurada. No tiene evidencia local sobre TLS del proxy, pertenencia
a red privada, alcance del endpoint remoto ni límites de admisión del
despliegue, así que no adivina nada sobre eso y no hace llamadas de red. Esas
fronteras están en [sync](sync.md).

Los `memory.db` antiguos a nivel de proyecto son un caso aparte: el importador
transaccional e idempotente sigue en el paquete de memoria, pero ni el arranque
ni las operaciones normales lo invocan. Conserva esos archivos hasta que se
elija una ruta de migración; que hoy no se usen no es permiso para borrarlos.

## Identidad de proyecto en sync

Durante el push por proyecto, los mapeos ordinarios envían IDs portables
deterministas y conservan sin cambios los IDs locales y los bytes del outbox.
Los mapeos adoptados tienen prioridad y reenvían su ID de entrada exacto. El
schema registra esa procedencia; el pull por proyecto materializa la historia
soportada, mientras que la traducción referencia-antes-que-objetivo y el
transporte `resolve` siguen sin soporte.
