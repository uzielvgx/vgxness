# Inspección operativa y retención

VGXNESS separa el inventario de solo lectura del mantenimiento destructivo:

```sh
vgxness doctor --deep --workspace /ruta/absoluta/del/proyecto
vgxness orchestrate list --workspace /ruta/absoluta/del/proyecto
vgxness maintenance prune --workspace /ruta/absoluta/del/proyecto --older-than 720h
vgxness maintenance prune --workspace /ruta/absoluta/del/proyecto --older-than 720h --apply
vgxness edit inspect --workspace /ruta/absoluta/del/proyecto --ticket ticket-...
vgxness edit approve --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
vgxness edit integrate --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
vgxness edit retire --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
vgxness edit discard --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
```

`doctor --deep` verifica el puntero actual de Chronicle y cada orquestación, ticket nativo y lease nativo reconocido. Devuelve `doctor=degraded` y un código distinto de cero cuando encuentra trabajo bloqueado o estancado, estado vencido o recuperable, documentos malformados o entradas de almacenamiento inseguras.

`orchestrate list` emite en JSON el inventario verificado de orquestaciones.

`maintenance prune` solo simula por defecto. La retención debe estar entre 24 horas y 10 años. Únicamente considera orquestaciones y tickets terminales estrictamente anteriores al corte. Protege tickets referenciados por orquestaciones conservadas o por leases activos. Al aplicar, adquiere el lock normal de cada documento, vuelve a leer y validar el candidato, elimina solo su documento JSON y conserva locks, evidencia Chronicle, memoria, autoridad, logs y trabajo no terminal. Cualquier corrupción del inventario bloquea la limpieza.

El ciclo `edit` es la única ruta soportada desde un artefacto nativo `write-files` completado hacia el checkout canónico. `inspect` muestra el artefacto y estado duraderos. `approve` vincula al operador con el manifiesto y commit base exactos. `integrate` acepta únicamente esa aprobación, exige un checkout limpio en la misma base, aplica solo los archivos aprobados y los deja sin agregar al índice. `retire` elimina el worktree aislado únicamente tras verificar el contenido integrado; `discard` elimina un artefacto que no fue integrado. Todas las acciones mutables requieren manifiesto y actor explícitos y se pueden repetir de forma segura.
