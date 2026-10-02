# Frontera del servicio de sincronización

`vgxness-syncd` es el servicio opcional de sincronización respaldado por
PostgreSQL. Su listener HTTP está limitado a propósito a una dirección loopback
literal y por defecto escucha en `127.0.0.1:8787`.

El servicio acepta peticiones de sincronización autenticadas con bearer. No
termina TLS y nunca debe exponerse directamente en una red no confiable. Un
despliegue remoto necesita un terminador TLS o un reverse proxy que acepte
HTTPS y reenvíe al demonio por loopback en el mismo host confiable. El cliente
de sincronización de la aplicación sigue exigiendo un endpoint `https` y no
sigue redirecciones que lleven credenciales.

## Configuración en tiempo de ejecución

El demonio lee `VGXNESS_SYNC_POSTGRES_DSN` o, de forma mutuamente excluyente,
`VGXNESS_SYNC_POSTGRES_DSN_FILE`, más `VGXNESS_SYNC_OWNER_ID`, al iniciar el
servicio o al gestionar credenciales de dispositivo. El archivo debe ser una
ruta absoluta a un archivo regular, acotado y sin symlink; se elimina
exactamente un LF o CRLF final. Mantén la cadena de conexión fuera de los
argumentos de comandos, de los logs, de archivos versionados y de la
configuración del proxy. Una configuración ausente o malformada falla cerrado.

Los límites de admisión opcionales son `VGXNESS_SYNC_AUTH_GLOBAL_PER_MINUTE`,
`VGXNESS_SYNC_AUTH_DEVICE_PER_MINUTE` y `VGXNESS_SYNC_AUTH_DEVICE_STATES`. Sin
definir, valen 120, 60 y 256 respectivamente. Cada valor debe ser un entero
positivo en base 10; un valor malformado, cero o negativo aborta el arranque
antes de preparar la base de datos o crear el listener. La ventana de admisión
es fija: un minuto.

## Límites de admisión y auditoría

Antes de autenticar contra PostgreSQL, el demonio aplica los límites de
admisión configurados: por defecto admite como máximo 120 intentos válidos de
bearer por minuto en todo el proceso y 60 por minuto por cada UUID de
dispositivo declarado sintácticamente. Los intentos excedentes reciben la
respuesta normal `429 limit_exceeded` y no llegan a PostgreSQL. El estado de
equidad por UUID se limita a 256 entradas y es local al proceso, así que un
despliegue con varios procesos aplica el límite de forma independiente. Esto no
es un rate limit distribuido: un despliegue multiproceso necesita un control de
admisión upstream propio si requiere un límite para toda la flota.

La evidencia de auditoría de autenticaciones fallidas converge a una ventana de
30 días y 10.000 eventos para el propietario configurado. La limpieza corre solo
dentro de una transacción de auditoría de fallo, respeta la cancelación y borra
como máximo 250 registros vencidos o excedentes por escritura; un exceso previo
puede persistir brevemente mientras los siguientes fallos lo convergen. No crea
goroutines en segundo plano.

Arranca el listener local con:

```sh
vgxness-syncd serve
```

Un `--listen` explícito debe seguir siendo una IP loopback literal con puerto
distinto de cero. Nombres de host, comodines, direcciones públicas o privadas no
loopback y el puerto cero se rechazan antes de leer configuración o
credenciales. La única excepción es `serve --container-network --listen
0.0.0.0:8787`, pensada para una red privada de Docker; no impone por sí misma el
aislamiento de esa red. `GET /healthz` es una respuesta de liveness acotada y
sin autenticación para health checks de contenedores; no revela estado de la
base de datos. El flag retirado `--development-allow-insecure-non-loopback`
rechaza `true`; `false` explícito sigue siendo un no-op solo para que los
comandos de arranque existentes puedan migrar.

## Administración local desde el navegador

Ejecuta `vgxness-syncd admin` en una terminal interactiva en el host de la base
de datos. El comando enlaza un puerto loopback literal aleatorio e imprime la
URL de administración y el secreto efímero de operador de esa corrida. Abre
exactamente la URL impresa, introduce el secreto y mantén la terminal privada.
La consola no usa cookie de sesión; sus acciones son solo POST y toda respuesta
es `no-store`.

Cada POST acepta exactamente un `Origin` que coincida con la autoridad HTTP
configurada, con independencia de Fetch Metadata. Cuando `Origin` está ausente
o es exactamente `null`, el fallback de compatibilidad acepta que las tres
cabeceras de enrutamiento estén ausentes o que `Sec-Fetch-Site`,
`Sec-Fetch-Mode` y `Sec-Fetch-Dest` valgan exactamente `same-origin`,
`navigate` y `document`. Si cualquiera de las tres está presente, deben estar
las tres y ser exactas; metadatos parciales, vacíos, duplicados, `cross-site`,
`none` o discordantes se rechazan. Un `Origin` vacío, incorrecto o duplicado se
rechaza siempre. `Sec-Fetch-User` puede estar presente, pero se ignora porque
no establece el origen de la petición.

El caso sin metadatos de enrutamiento cubre el comportamiento observado de
Chrome al enviar formularios POST sobre un origen HTTP de Tailscale: `Origin:
null` con las tres cabeceras ausentes. Esta puerta de origen no es
autenticación. Cada POST aceptado sigue exigiendo su secreto de operador de alta
entropía o su sesión en el cuerpo del formulario codificado en URL, y la consola
no lee cookies. Una página de otro origen aún puede provocar una petición o un
fallo no autenticado, así que esto no es inmunidad amplia a CSRF: la
confidencialidad de la credencial del cuerpo y el aislamiento del listener
siguen siendo necesarios.

El panel puede emitir una credencial de dispositivo con nombre e iniciar la
revocación de un dispositivo activo. La emisión muestra el bearer nuevo
exactamente una vez, en la respuesta inmediata. Cópialo directamente a la
entrada estándar de `vgxness memory sync configure`; no lo pongas en una URL,
argumento de shell, log, nota, extensión ni almacenamiento del navegador. Salir
de la página o recargarla pierde la visualización. La revocación usa una página
de confirmación de un solo uso, de corta vida, que muestra el UUID canónico del
dispositivo antes del POST final. Volver al panel tras cualquiera de las dos
acciones requiere POST; un GET nunca emite ni revoca. Tras emitir, verifica que
el dispositivo aparece activo en el panel antes de usar su bearer: la entrega
puede completarse aunque el acuse del commit sea ambiguo, así que el panel es la
fuente de verdad.

Para administrar por SSH, primero ejecuta el comando en el host remoto y anota
el `127.0.0.1:PORT` impreso. En una segunda terminal, reenvía el **mismo**
puerto para que Host y Origin del navegador sigan coincidiendo:

```sh
ssh -N -L 127.0.0.1:PORT:127.0.0.1:PORT operator@sync-host
```

Luego abre `http://127.0.0.1:PORT/` en local y usa el secreto de operador de la
terminal remota. Sustituye ambos `PORT` por el impreso en esa corrida. No
expongas ni hagas reverse proxy de este listener.

Loopback y un puerto aleatorio reducen la exposición, pero no son una frontera
completa de aislamiento del navegador. Un service worker registrado antes para
exactamente el mismo origen loopback puede iniciar acciones de consola o leer
un bearer en el navegador. Ese riesgo residual se acepta; la consola no afirma
eliminarlo. Usa un contexto de navegador dedicado cuando sea práctico, cierra la
página pronto y detén el proceso de administración al terminar.

### Panel persistente por Tailscale en Docker

El despliegue en Docker puede habilitar un panel persistente dentro del mismo
proceso `vgxness-syncd serve`. Sus ajustes integrados forman una configuración
que falla cerrada:

- `VGXNESS_SYNC_ADMIN_LISTEN` debe ser exactamente `0.0.0.0:8788` y solo se
  acepta con `serve --container-network`.
- `VGXNESS_SYNC_ADMIN_AUTHORITY` debe ser una autoridad IP canónica exacta con
  puerto canónico distinto de cero y una dirección en el rango IPv4
  `100.64.0.0/10` o IPv6 `fd7a:115c:a1e0::/48` de Tailscale.
- `VGXNESS_SYNC_ADMIN_SECRET_FILE` debe ser un archivo regular, absoluto,
  acotado y sin symlink con un payload de 32 a 4.096 bytes tras quitar un LF o
  CRLF final opcional. El margen bruto de 4.098 bytes solo permite el payload
  máximo más CRLF; payloads mayores o varios saltos de línea fallan. Se acepta
  modo `0600` o `0640` en producción; escritura o ejecución de grupo y cualquier
  permiso de otros usuarios se rechazan.
- `VGXNESS_SYNC_ADMIN_SECRET` está prohibido; el secreto de inicio de sesión
  solo va en archivo.

Cuando ningún ajuste integrado está presente, `serve` corre solo la API de
sync. Cualquier ajuste parcial o inválido aborta el arranque. Ambos listeners se
preparan y enlazan antes de que cualquiera empiece a servir. La cancelación
apaga los dos y un fallo inesperado de cualquiera termina `serve`. Ambos
handlers comparten repositorio y pool de conexiones. El comando separado
`vgxness-syncd admin` sigue siendo efímero, exclusivo de terminal, de puerto
aleatorio y solo loopback. El modo integrado persistente es solo Docker/Linux y
falla cerrado en Windows; esto no retira el soporte del admin independiente.

En el despliegue Compose revisado, el host publica únicamente
`${VGXNESS_TAILSCALE_IP}:8788:8788`; la API de sync no tiene publicación en el
host y sigue pasando por NPM en la red de Docker. Abre exactamente
`http://${VGXNESS_TAILSCALE_IP}:8788/` desde un peer de la tailnet e introduce
el valor leído del archivo de secreto protegido. Nunca enlaces el lado del host
a `0.0.0.0`, uses una dirección pública o de LAN, ni agregues una ruta de
proxy NPM o pública para el panel. Tailscale cifra el salto de la tailnet; la
aplicación sigue exigiendo su secreto. Los peers de la red del contenedor son
una zona de confianza residual, pero no pueden iniciar sesión sin ese secreto.

El panel sigue el ciclo de vida del proceso o contenedor de `serve`. Reiniciar,
actualizar o revertir lo interrumpe e invalida su sesión en memoria; conserva el
mapeo del secreto protegido y la autoridad Tailscale exacta, y vuelve a iniciar
sesión por la misma URL. Consulta el runbook de Docker para crear el secreto,
comprobar su legibilidad, verificar actualizaciones y reversiones, y las
advertencias de exposición. El reemplazo, la eliminación o el cambio de permisos
del secreto se leen solo al arrancar el proceso y requieren un reinicio
controlado del contenedor; hasta entonces el secreto cargado sigue activo
aunque el archivo cambie.

## Enrolamiento local y estado

Enrola un cliente local sin poner el bearer en argumentos, variables de entorno
ni en la base SQLite. Entrégalo solo por la entrada estándar:

```sh
vgxness memory sync configure \
  --endpoint https://sync.example.test \
  --device-id 550e8400-e29b-41d4-a716-446655440000
```

El comando lee el bearer de stdin; escríbelo o entúbalo directamente, sin
pasarlo por una variable de entorno ni un argumento.

En Linux y macOS puede usarse, de forma explícita, un archivo de credencial
propiedad del usuario actual en lugar del keyring del escritorio:

```sh
vgxness memory sync configure --credential-file /ruta/absoluta/privada/bearer --endpoint https://sync.example.test --device-id 550e8400-e29b-41d4-a716-446655440000
vgxness memory sync status --credential-file /ruta/absoluta/privada/bearer
vgxness memory sync --credential-file /ruta/absoluta/privada/bearer
```

El archivo debe ser regular y absoluto, no un symlink (ni estar bajo uno),
propiedad del usuario actual, sin permisos de grupo ni de otros, y contener una
sola línea con el bearer y un LF o CRLF final opcional. Ni la ruta ni el bearer
se persisten: cada `status` o `sync` posterior debe volver a indicar el
archivo. Los archivos de credencial no están soportados en Windows; omite el
flag ahí para usar el keyring. Las carreras del sistema de archivos entre
procesos del mismo usuario no pueden impedirse atómicamente, así que el comando
vuelve a comprobar el descriptor abierto y falla cerrado si su identidad o
metadatos cambian.

Para datos locales previos, encola los registros antes de la primera
sincronización remota con una operación local acotada al workspace:

```sh
vgxness memory sync backfill --workspace /ruta/absoluta/workspace --limit 100 --json
vgxness memory sync --credential-file /ruta/absoluta/privada/bearer
```

`backfill` no envía peticiones de red ni lee credenciales. Encola de forma
determinista solo los registros no sincronizados de ese proyecto, conserva
contenido, marcas de tiempo y versiones de las observaciones, omite registros
con tombstone, detecta colisiones de identidad en la cola y puede repetirse sin
riesgo. Si un `create` existente contiene una instantánea válida pero anterior,
el backfill reemplaza solo su payload conservando la identidad de la mutación y
de la cola, y solo mientras esa fila siga pendiente con cero intentos y sin
historial de claims. Claims activos o vencidos, reintentos, intentos, payloads
malformados o con identidad cambiada y filas modificadas concurrentemente fallan
cerrado. `--limit` vale 100 por defecto y acepta de 1 a 1.000; el JSON informa
`remaining=true` cuando hace falta otra invocación.

Tras completar un push por proyecto, el pull por proyecto mapea las
identidades portables a identidades locales antes de consultar el recibo
durable del push. Un eco exacto, aceptado o previamente aceptado, de
`create`/`update` de proyecto, sesión u observación ya está materializado: el
registro local y su versión se conservan mientras el inbox y el cursor del
proyecto avanzan en la misma transacción. Hash de mutación, identidad y tipo del
registro, tipo de mutación, versiones base y canónica, secuencia y disposición
deben coincidir con el recibo. Recibos ausentes o discordantes y `create`
foráneos siguen fallando cerrado; las disposiciones de conflicto coincidentes
pasan por la materialización de conflictos, y las transiciones activas de
`reseed`/`rejoin` conservan su manejo de instantánea.

## Reseed del primer dispositivo y rejoin

Primero reinicia la nube. En el dispositivo origen (Mac), ejecuta `vgxness
memory sync reseed --workspace /ruta/absoluta/workspace --confirm-cloud-empty
[--json]`; solo procede cuando la nube está exactamente vacía. En cada
dispositivo Linux o Windows posterior, ejecuta `vgxness memory sync rejoin
--workspace /ruta/absoluta/workspace --confirm-merge [--json]`. Ambos comandos
exigen el marcador y la vinculación estrictos del proyecto, aplican solo a ese
proyecto y nunca ejecutan `git pull`. La confirmación es específica de cada modo
y exacta. Los reintentos reanudan la transición durable; una intención pendiente
bloquea solo ese proyecto. El comando anterior `repair-project` sigue siendo
una recuperación local estrecha para una reparación aceptada de proyecto
ausente, no el flujo de primer dispositivo.

`memory sync configure` valida localmente el endpoint HTTPS, el ID de
dispositivo y el bearer. Un lock entre procesos, cancelable por contexto,
serializa el enrolamiento. Deriva dos slots deterministas de keyring a partir de
la identidad canónica del almacenamiento local, guarda el bearer solo en el slot
inactivo y luego cambia el perfil SQLite en una transacción. Keyring y SQLite no
son una sola transacción atómica: si la persistencia falla, se elimina ese slot
inactivo y la credencial activa anterior queda intacta. El schema registra solo
el slot determinista opuesto como marcador de recuperación; en el siguiente
enrolamiento compensa o completa la limpieza. Si la limpieza falla, el marcador
se queda y bloquea nuevos enrolamientos en lugar de adivinar. Las referencias de
credencial heredadas nunca se convierten en marcadores ni se borran solas.
Ningún paso contacta al servicio remoto.

`vgxness memory sync status [--json]` también es local y de solo lectura.
Informa si hay un perfil configurado y si su credencial en el keyring está
disponible, ausente, no disponible o inválida; nunca imprime el bearer ni el
marcador de recuperación, y nunca contacta al servicio remoto.

## Frontera del despliegue remoto

Para sincronización remota, el responsable del despliegue se encarga del ciclo
de vida del certificado TLS, los controles de acceso del proxy, la preservación
del tamaño de las peticiones, los timeouts, la redacción de logs y de reenviar
únicamente al listener loopback. VGXNESS no ofrece terminación TLS nativa en
`vgxness-syncd`.

Para un ejemplo declarativo en un solo host Ubuntu 24.04, consulta el
[paquete de despliegue para Ubuntu 24.04](../deploy/ubuntu/README.md). Para una
ruta aditiva en Docker detrás de un Nginx Proxy Manager existente, consulta el
[paquete de despliegue en Docker](../deploy/docker/README.md). Ninguno de los dos
es evidencia de un despliegue observado en un VPS. El diseño de identidad
portable y del sync por proyecto está en
[project-scoped-memory-sync](architecture/project-scoped-memory-sync.md).
