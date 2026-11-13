# Spike del rediseño (tmux 3.4)

Comprobaciones hechas el 2026-11-05 con un servidor tmux aislado
(`tmux -S <scratch>/spike.sock -f /dev/null`), sin tocar el repositorio ni los
servidores de ShellStudio. El cliente se adjuntó desde un PTY de 100×20 con
`TERM=xterm-256color`.

## 1. Rangos clicables en la línea de estado

Con `status on`, `status-position top` y

```
status-format[0] = #[range=user|vistas]VISTAS#[norange] #[range=user|tab:abc]Dev#[norange] … #[align=right]#[range=user|new]Nueva#[norange]
```

y `bind -n MouseDown1Status run-shell "… #{mouse_status_range} #{mouse_x}"`,
los clics SGR en las columnas 2, 9, 13 y 97 de la fila 1 dieron:

```
range=vistas x=1
range=tab:abc x=8
range=tab:xyz x=12
range=new x=96
```

**Límite:** tmux guarda como mucho **15 caracteres** del argumento de un rango
`user`. `#[range=user|tab:0123456789abcdef01234567]` llega como
`tab:0123456789a`; `#[range=user|t0123456789ab]` llega completo.

**Decisión:** la barra superior usa `range=user|…` y `#{mouse_status_range}`.
No hace falta el respaldo con desplazamientos x (`@ss_tabs_hit`). Cada pestaña
lleva `t` seguido de los 12 primeros caracteres hex del ID de la vista
(`mux.TabRange`); `_view tab` resuelve la vista por ese prefijo. Los botones son
`vistas`, `new`, `arrange` y `more`.

## 2. Estilos en `display-menu`

`display-menu -c <cliente> -x 5 -y 5 -T Tags '#[fg=colour179]● Amber' a '' …`
emite `\e[38;5;179m● Amber`: tmux 3.4 respeta `#[fg=colourN]` en el nombre de
cada entrada.

**Decisión:** el desplegable de etiquetas, el filtro `Etiqueta ▼`,
`Columnas ▼`, `⋯` y los menús de la barra superior son **menús nativos**
(`display-menu -c … -x … -y …`, como `headerMenu` en
`internal/mux/config.go`). No hace falta la capa `Overlay` dentro del frame.
Lanzar `display-menu` desde un proceso externo bloquea hasta que el menú se
cierra: siempre se lanza con `run-shell -b` o desde un binding, nunca de forma
síncrona dentro de un lote de `Mux.Batch`.

## 3. Ancho de los glifos `─ □ ▼ ● ⋯`

| Cliente | `client_utf8` | Resultado en la línea de estado |
|---|---|---|
| `LANG=C.UTF-8` | 1 | `A─B□C▼D●E⋯F`, una columna cada uno |
| `LANG=C`, sin `-u` | 0 | `─` se dibuja como línea ACS; `□ ▼ ● ⋯` pasan a `_` |

ShellStudio adjunta siempre con `tmux -u` (vistas, programas y popups), así
que sus clientes están en modo UTF-8 aunque el locale sea `C`. Así lo verifica
`test_utf8_headers_with_ssh_c_locale`.

**Decisión:**
- Los formatos de tmux usan los glifos UTF-8 envueltos en `#{?client_utf8,G,A}`
  con el respaldo ASCII `_ O v * ...` (una columna cada uno, como las ranuras fijas de la cabecera) (`theme.Glyphs` / `theme.ASCII`), para
  un cliente ajeno que se adjunte sin `-u`.
- La TUI dibuja siempre en UTF-8 porque corre dentro de clientes `-u`.
- Todos los glifos ocupan una columna (`go-runewidth` sin modo East Asian).

## 4. Mosh (medido con Mosh 1.4.0)

`mosh-server new -i 127.0.0.1 …` local, con `mosh-client` en un PTY de
140×40 (`MOSH_KEY` del `MOSH CONNECT`), registrando lo que llega a la terminal
del usuario.

**Portapapeles (OSC 52).** Dentro de la sesión se emitió `ESC]52;;…` (sin
selector) y después `ESC]52;c;…`:

| Secuencia emitida | Llega al cliente |
|---|---|
| `ESC]52;;RU1QVFk=` | no: Mosh la descarta |
| `ESC]52;c;Q0xJUA==` | sí: `ESC]52;c;Q0xJUA==` |

Esto confirma el selector de `terminal-overrides[1]`, que pone `c` cuando la
selección está vacía. Sigue igual tras el rediseño, y lo verifica también
`test_clipboard_uses_mosh_compatible_selector_through_nested_tmux`.

**Colores e interfaz.** Dentro de Mosh se corrió ShellStudio con datos
aislados:

| Colores del servidor | TERM en tmux | Resultado |
|---|---|---|
| `-c 256` (lo que pasa `mosh` con una terminal de 256 colores) | `xterm-256color` | Llegan 174 SGR `38;5`/`48;5` con los índices del tema: fondos 16, 234, 235, 24 y 33; texto 231, 250, 252, 244, 240 y 33. Llegan `VIEWS`, `New console`, `MINIMIZED` y `▼` (8 veces). Cero SGR truecolor (`38;2`/`48;2`). |
| sin `-c` (8 colores) | `xterm` | tmux degrada la paleta a ANSI: el acento pasa a azul (`1;34`) y el panel activo a `37;44`. La interfaz sigue legible. |

**Decisión:** el tema usa solo índices de la paleta de 256 colores. Se ve
igual a través de Mosh y se degrada a ANSI con terminales de 8 colores. La
barra superior es una línea de estado de tmux y no consulta a la terminal
del usuario. Se mantienen `escape-time` 50 ms y `XT@` del arreglo de
reportes partidos.
