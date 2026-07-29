# Inspección operativa y retención

> **Estado: Solo compatibilidad.** Estos comandos inspeccionan y mantienen estado legacy del puente/plano de control, tickets, leases, edición aislada y Delivery Authority. Siguen disponibles para CLI y mantenimiento, pero no son el scheduler activo instalado en OpenCode.

VGXNESS separa el inventario de compatibilidad del mantenimiento destructivo:

```sh
vgxness doctor --deep --workspace /ruta/absoluta/del/proyecto
vgxness orchestrate list --workspace /ruta/absoluta/del/proyecto
vgxness maintenance prune --workspace /ruta/absoluta/del/proyecto --older-than 720h
vgxness maintenance prune --workspace /ruta/absoluta/del/proyecto --older-than 720h --apply
vgxness edit inspect --workspace /ruta/absoluta/del/proyecto --ticket ticket-...
vgxness edit review --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --review-manifest manifiesto-revision.json
vgxness edit approve --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --receipt ID_RECIBO --actor NOMBRE
vgxness edit integrate --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
vgxness edit retire --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
vgxness edit discard --workspace /ruta/absoluta/del/proyecto --ticket ticket-... --manifest sha256-... --actor NOMBRE
```

`doctor --deep` verifica el puntero actual de Chronicle y cada orquestación, ticket nativo y lease nativo reconocido. Devuelve `doctor=degraded` y un código distinto de cero cuando encuentra trabajo bloqueado o estancado, estado vencido o recuperable, documentos malformados o entradas de almacenamiento inseguras.

`orchestrate list` emite en JSON el inventario verificado de orquestaciones.

`maintenance prune` solo simula por defecto. La retención debe estar entre 24 horas y 10 años. Únicamente considera orquestaciones y tickets terminales estrictamente anteriores al corte. Protege tickets referenciados por orquestaciones conservadas o por leases activos. Al aplicar, adquiere el lock normal de cada documento, vuelve a leer y validar el candidato, elimina solo su documento JSON y conserva locks, evidencia Chronicle, memoria, autoridad, logs y trabajo no terminal. Cualquier corrupción del inventario bloquea la limpieza.

Dentro del subsistema de edición aislada de compatibilidad, el ciclo `edit` es la ruta soportada desde un artefacto `write-files` completado hacia el checkout canónico. `inspect` muestra el artefacto y estado duraderos. `review` emite un recibo de Delivery Authority sobre el artefacto aislado exacto. `approve` exige ese recibo activo y vincula su árbol candidato y digest de revisión con el operador, manifiesto y commit base. `integrate` revalida el recibo antes de mutar, exige un checkout limpio en la misma base, aplica solo los archivos aprobados y los deja sin agregar al índice. `retire` elimina el worktree aislado únicamente tras verificar el contenido integrado; `discard` elimina un artefacto que no fue integrado. Estos comandos no describen la ruta apply de SDD nativo: el agente apply de solo lectura compone un patch ligado por hash y solo el manager escribe y valida el workspace.

La base predeterminada `~/.vgxness/memory.db` comparte dominios aislados de memoria semántica y SDD estructurado. Tras actualizar el binario desde el esquema v4, los comandos de status e inspección de solo lectura no pueden ejecutar la migración v5 y pueden fallar hasta que una operación mutable de memoria o SDD abra la base. Nunca elimine la base; ejecute esa operación y vuelva a ejecutar status. Consulte [Memoria nativa](memory.md#upgrade-migration-caveat).
