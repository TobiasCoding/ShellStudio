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
