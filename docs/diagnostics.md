# Diagnóstico de la instalación local

`vgxness status` y `vgxness doctor` inspeccionan el almacenamiento SQLite de la
instalación local. Son de solo lectura: no reparan, no instalan, no invocan
modelos y no sincronizan nada.

Ejecútalos desde el proyecto, o selecciónalo explícitamente:

```sh
vgxness doctor --workspace /ruta/absoluta/al/proyecto
```

`--storage-root` y `--project-local` eligen una base aislada en lugar de la
predeterminada `~/.vgxness/memory.db`.

Salida de ambos comandos:

| Campo | Significado |
| --- | --- |
| `storage_root=` | Raíz de almacenamiento resuelta. |
| `database=` | Ruta de la base de datos inspeccionada. |
| `migration=` | Versión del schema que reporta la comprobación de salud (hoy 23). |
| `doctor=healthy` | Solo en `doctor`: la inspección terminó sin errores. |

Códigos de salida: **0** sin errores; **1** error de almacenamiento, migración o
inspección (el mensaje en stderr empieza con `operational:`, `corrupt:` o
`not_found:`); **2** argumentos inválidos; **130** cancelación.

Un fallo de migración justo después de actualizar el binario es esperado hasta
que una operación con escritura migre la base; ver
[memoria](memory.md#actualización-y-migraciones). Para comprobar que Claude
Code ve el plugin y el servidor MCP, usa `claude plugin validate
plugins/vgxness --strict` y `claude mcp list` desde el proyecto; ver
[VGXNESS en Claude Code](claude-code.md).
