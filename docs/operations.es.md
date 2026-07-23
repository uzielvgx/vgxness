# Inspección operativa y retención

VGXNESS separa el inventario de solo lectura del mantenimiento destructivo:

```sh
vgxness doctor --deep --workspace /ruta/absoluta/del/proyecto
vgxness orchestrate list --workspace /ruta/absoluta/del/proyecto
vgxness maintenance prune --workspace /ruta/absoluta/del/proyecto --older-than 720h
vgxness maintenance prune --workspace /ruta/absoluta/del/proyecto --older-than 720h --apply
```

`doctor --deep` verifica el puntero actual de Chronicle y cada orquestación, ticket nativo y lease nativo reconocido. Devuelve `doctor=degraded` y un código distinto de cero cuando encuentra trabajo bloqueado o estancado, estado vencido o recuperable, documentos malformados o entradas de almacenamiento inseguras.

`orchestrate list` emite en JSON el inventario verificado de orquestaciones.

`maintenance prune` solo simula por defecto. La retención debe estar entre 24 horas y 10 años. Únicamente considera orquestaciones y tickets terminales estrictamente anteriores al corte. Protege tickets referenciados por orquestaciones conservadas o por leases activos. Al aplicar, adquiere el lock normal de cada documento, vuelve a leer y validar el candidato, elimina solo su documento JSON y conserva locks, evidencia Chronicle, memoria, autoridad, logs y trabajo no terminal. Cualquier corrupción del inventario bloquea la limpieza.
