# Diseño

Fuente de verdad de cómo se ve VGXNESS. El trabajo de UI debe coincidir con el
canvas; un cambio en uno se refleja en el otro.

- Design system: https://claude.ai/artifact/5tnRiD2UwvqtFj58kyiKPf (brand book,
  tokens, 19 componentes de terminal con preview y guía).
- Canvas: https://claude.ai/artifact/55i278YA64Yz3tKGM9zgvX. Páginas: Índice
  (entrada), Consola (22 láminas de 1040×760 = 120×35 celdas, agrupadas por
  módulo: Inicio, Setup del plugin, Memoria, Sync, Doctor) y Arquitectura
  (diagrama del sistema). Los archivos de lámina son
  `tui-<modulo>-<pantalla>[-<estado>].dc.html`.
- Decisiones: `docs/decisions.md` D-001 (consola) y D-002 (design system).

## Tokens

Temas: `midnight` (consola, por defecto) y `paper` (docs y superficies web
claras; el TUI nunca cambia a él). Los hex son lo que recibe `lipgloss.Color`;
lipgloss reduce solo a 256 y 16 colores. La columna ANSI 256 es el índice que
se espera tras la reducción.

| Token | midnight | paper | ANSI 256 (midnight) | Uso |
|---|---|---|---|---|
| `canvas` | `#071522` | `#f5f7f8` | 234 | Fondo de la terminal y de la página |
| `panel` | `#102231` | `#ffffff` | 235 | Paneles, tarjetas, relleno del picker |
| `panel-raised` | `#183447` | `#e9eef1` | 236 | Fila alterna o con hover; cola de la barra de progreso |
| `ink` | `#f5f7f8` | `#102231` | 255 | Texto principal |
| `ink-muted` | `#9db1bc` | `#52616d` | 109 | Texto secundario, etiquetas, barra de ayuda, cabeceras de tabla |
| `ink-faint` | `#4f6675` | `#a7b3bb` | 60 | Divisores, placeholders, filas deshabilitadas (no para texto legible) |
| `accent` | `#4dd4d4` | `#005f5c` | 80 | Títulos de sección, títulos de panel, `[Tecla]`, foco y selección |
| `accent-strong` | `#008b87` | `#008b87` | 30 | Marco del panel con foco, fin del degradado (solo bordes y texto grande) |
| `frame` | `#005f5c` | `#c9d4d9` | 23 | Bordes de panel (glifos de caja) y rail del stepper; decorativo |
| `on-accent` | `#071522` | `#ffffff` | 234 | Texto sobre rellenos `accent` |
| `focus-bg` / `focus-fg` | = `accent` / `on-accent` | | | Fila seleccionada |
| `success` | `#25d366` | `#15803d` | 77 | ✓ |
| `warning` | `#f59e0b` | `#b45309` | 214 | ! |
| `error` | `#f87171` | `#b91c1c` | 203 | ✕ y confirmaciones destructivas |
| `info` | `#60a5fa` | `#0369a1` | 75 | ◇ |
| `banner-from` / `banner-to` | = `accent` / `accent-strong` | | | Degradado del banner y de la barra de progreso (`lipgloss.Blend1D`) |

Fallback de 16 colores: `accent` → cian brillante, `ink-muted` → negro
brillante, `success`/`warning`/`error`/`info` → verde/amarillo/rojo/azul.

Tipografía: el TUI usa la fuente de la terminal. Todo es `term` 14/20; énfasis
`term-strong` (700); títulos de sección `term-label` (700, mayúsculas, 0.08em);
el banner de cuatro filas, generado desde un bitmap 5×7 con medios bloques
(`▀ ▄ █`, 47 columnas, degradado `Blend1D` por fila), únicamente a ≥ 120
columnas. Docs: Space Grotesk 600 para `display` (32/40) y `heading` (22/28);
Inter para `body` (15/22), `caption` (13/18) y `eyebrow` (12/16, 600,
mayúsculas). Los mockups dibujan la terminal en JetBrains Mono (celda 8.4 px ×
fila 20 px).

El espaciado en el TUI se mide en celdas y filas: dos celdas de sangría dentro
de un panel, una celda de padding, una fila en blanco entre secciones, la
barra de ayuda como última fila. Terminal mínima 80×24; por debajo solo se
dibuja la pantalla TooSmall.

## Composición de pantalla

- Header (2 filas; 6 en Inicio con banner), uno o dos paneles lado a lado que
  llenan la altura, opcionalmente una fila de ActionCards de altura fija, y
  KeyHelp como última fila. El panel lateral mide 45 columnas; el principal
  toma el resto. Por debajo de 100 columnas el lateral baja debajo del
  principal.
- Flujos de varios pasos (Setup) llevan el Stepper a la izquierda (28
  columnas) y el panel de contenido con el marco `accent-strong`.
- Los modales (Picker, Confirm) se dibujan como capas sobre la pantalla
  atenuada y ocultan KeyHelp mientras están abiertos.

## Mapa a Bubble Tea v2, Bubbles v2 y Lip Gloss v2

| Pieza | Cómo se construye |
|---|---|
| Panel | `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(frame)`; foco → `accent-strong`. No hay API de título en borde: el título es la primera línea en `term-label`. |
| Columnas y filas | `lipgloss.JoinHorizontal(lipgloss.Top, …)` y `JoinVertical`, tamaños desde `tea.WindowSizeMsg`; recorte con `MaxWidth` y `ansi.Truncate`. |
| Centrado (vacíos, código de pareo, TooSmall) | `lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, s)`. |
| Modal | `lipgloss.NewCompositor(base, lipgloss.NewLayer(dialog).X(x).Y(y).Z(1))` → `tea.View.Content`; base con `Faint`; `Compositor.Hit()` para clics con `View.MouseMode = tea.MouseModeCellMotion`. |
| Tabla con cursor | `bubbles/table` con `Styles.Selected` = `focus-bg`/`focus-fg`; celdas truncadas con `…`, nunca envueltas. |
| Tabla de solo lectura | `lipgloss/table` con `StyleFunc` para la cabecera en `ink-muted`. |
| Barra de ayuda | `bubbles/help` con `key.Binding` cuyo `WithHelp` ya trae los corchetes (`"[Enter]"`, `"continuar"`); `ShortKey` = `accent`, `ShortDesc` y separador `" · "` en `ink-muted`. |
| Entrada de texto | `bubbles/textinput` con prompt `> `, `Placeholder` en español, `Validate` para rutas y `Cursor()` expuesto en `tea.View.Cursor`. |
| Progreso determinado | `bubbles/progress` con `WithColors(lipgloss.Blend1D(n, accent, accent-strong)...)`, `WithFillCharacters('█','░')`, 40 celdas; valor espejado en `tea.View.ProgressBar`. |
| Progreso indeterminado | `spinner.Spinner{Frames: ◐ ◓ ◑ ◒, FPS: 1/8 s}` en `accent`; `spinner.MiniDot` para búsquedas. |
| Registro desplazable | `bubbles/viewport` con `MouseWheelEnabled`; `ScrollPercent()` a la derecha del título de sección. |
| Pantalla completa | `tea.View{AltScreen: true, WindowTitle: "VGXNESS Console", ReportFocus: true}`; en `tea.BlurMsg` todo el frame se dibuja `Faint`. |
| Anchos con acentos | `charmbracelet/x/ansi`: `StringWidth`, `Truncate`, `TruncateLeft` (ruta del workspace). |

## Reglas que el código debe cumplir

- El estado siempre es glifo + color + palabra: `✓ ! ✕ ◇`; `◐` en progreso, `◌`
  pendiente, `▸` cursor, `·` separador, `…` recorte. Sin emoji ni Nerd Fonts.
- Atajos: `[Tecla] acción` en `ink-muted` con la tecla en `accent`, unidos
  por ` · `, máximo seis por pantalla.
- Las acciones destructivas (olvidar, reset, reinstalar) pasan por Confirm con
  la consecuencia escrita y `[y]`/`[n]`; Enter nunca confirma una destrucción.
- Los errores dicen qué falló y añaden una línea que empieza con `Acción:`.
- Las operaciones largas muestran Progress con la lista de pasos y el
  registro; mientras un paso muta solo se acepta Ctrl+C.
- Copy de UI en español (skill `ui-copy-es`); comandos, rutas e identificadores
  en inglés.
