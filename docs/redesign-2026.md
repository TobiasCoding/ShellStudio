# Rediseño de la interfaz — 2026-11-05

ShellStudio adopta el diseño de la maqueta *ShellStudio terminal workspace with
console table*: barra de vistas arriba, árbol de archivos con búsqueda y el
panel Minimizadas en la columna izquierda, y cabeceras de consola con botones.
Los atajos F2, F3, F4, F5, F7, F9 y F10 y los menús existentes se mantienen.

## Cambios

- **Barra superior** (línea de estado del servidor de vistas):
  - `VISTAS` abre el diálogo de vistas (F7). Cada vista es una pestaña; la
    actual se resalta en azul y un clic cambia a otra. Con el botón derecho
    sobre una pestaña se renombra o cierra la vista, o se crea otra.
  - A la derecha están `Nueva consola ▼`, `Distribuir ▼` y `Más ▼`, como menús
    nativos de tmux que se abren bajo el puntero.
  - Por debajo de 100 columnas, los nombres de las pestañas se abrevian.
- **Cabeceras de consola:**
  - Muestran `Tipo · Nombre`. La consola activa lleva fondo azul con texto
    blanco en negrita; las inactivas, gris sobre gris oscuro.
  - Si la consola tiene etiqueta, se ve su insignia de color.
  - A la derecha hay tres botones en ranuras fijas:
    - `─` minimiza la consola: el programa sigue vivo.
    - `□` maximiza el panel (lo mismo que F4).
    - `▼` abre el menú. El menú suma Minimizar, Etiqueta ▸ y Descripción….
  - La insignia FINISHED se conserva.
- **Panel Minimizadas**, debajo del árbol:
  - Una tabla con las columnas Creada, Consola (con su descripción) y Etiqueta.
    Desde `Columnas ▼` se pueden mostrar también Carpeta, Estado y Minimizada el.
  - Se busca por nombre, tipo, descripción, etiqueta o carpeta, y se filtra
    por etiqueta. Un clic en el título de una columna ordena por ella; otro
    clic invierte el orden.
  - Al pie aparecen la consola seleccionada y su estado (en ejecución,
    terminada o detenida), con los botones `[ Restaurar ]`, `[ Renombrar ]`
    y `[ ⋯ ]`.
  - Enter o un doble clic restauran la consola en su posición original sin
    reiniciar el programa.
  - Se puede ocultar desde «Más».
- **Etiquetas:**
  - Todas son del usuario: se crean, renombran, recolorean (con una paleta de
    8 colores), reordenan y borran desde «Más ▸ Etiquetas…».
  - Al borrar una etiqueta, sus consolas quedan sin etiqueta.
  - La migración siembra cuatro etiquetas iniciales, que son datos comunes y
    editables como cualquier otra.
- **Explorador:**
  - La cabecera es `ARCHIVOS ▼`, y debajo hay un campo «Buscar archivos»
    siempre visible.
  - La búsqueda filtra el árbol: muestra las coincidencias con sus carpetas,
    abiertas, y resalta el texto que coincide.
  - Esc restaura el árbol tal como estaba; Enter revela el resultado.
- **Tamaños:** el ancho de la columna izquierda y el alto de Minimizadas se
  guardan por vista al soltar un divisor.
- **Idioma:**
  - La interfaz está en español e inglés, según `language` en `config.json`
    (`auto`, `es` o `en`) o el locale.
  - Se cambia en «Más ▸ Idioma». Los registros y diagnósticos siguen en inglés.
- **Tema:** acento azul de la paleta de 256 colores, sin truecolor, para que se
  vea igual a través de Mosh.

## Datos

- **Esquema 4**, migrado en una sola transacción:
  - la tabla `tags`;
  - `description` y `tag_id` en `consoles`;
  - `minimized` y `minimized_at` en `members`;
  - `explorer_width`, `minimized_height` y `minimized_panel` en `views`.
- Las versiones anteriores rechazan una base de esquema 4.
- Minimizar es una propiedad de la pertenencia a una vista: la misma consola
  puede estar minimizada en una vista y visible en otra.
- Cerrar un panel sigue desvinculando la consola de la vista, así que no
  aparece en Minimizadas.

## Límites aceptados de tmux

- No hay huecos entre paneles: los paneles comparten bordes heavy, y el
  divisor entre el árbol y Minimizadas es un borde de tmux.
- Las cabeceras ocupan la fila 0 de cada panel.
- tmux guarda como mucho 15 caracteres de un rango de la línea de estado. Por
  eso cada pestaña se identifica con «t» y los 12 primeros caracteres del ID de
  su vista (ver [redesign-spike.md](redesign-spike.md)).
- El botón `□` no cambia a `❐` mientras el panel está maximizado: la cabecera
  la dibuja el servidor de programas, que no conoce el zoom de la vista.

## Comprobaciones

- `make check` en verde. Incluye:
  - pruebas Go con y sin `-race`;
  - 24 escenarios PTY con tmux real;
  - las pruebas del instalador y del pipeline de release;
  - `go vet` y la auditoría de publicación.
- Escenarios nuevos:
  - pestañas de la barra superior;
  - botones `□` (zoom) y `─` (minimizar), con el mismo PID;
  - restaurar con doble clic a la posición original;
  - búsqueda y filtro por etiqueta en Minimizadas;
  - borrar una etiqueta desde su diálogo, que deja la consola sin etiqueta y
    quita la insignia de la cabecera;
  - filtro del árbol;
  - ancho de la columna izquierda conservado tras F10 y un servidor de vistas
    nuevo;
  - interfaz en español con `LANG=es_AR.UTF-8`.
- Las pruebas existentes corren con `LANG=C` y siguen en inglés. El texto en
  inglés quedó idéntico byte a byte tras el barrido i18n: se comparó la salida
  antes y después del cambio.
- **Migración con datos reales:**
  - Un respaldo de esquema 2 y una copia de la base en uso (esquema 3), hecha
    con `shellstudio backup`, migraron a esquema 4.
  - `integrity_check` dio correcto y `doctor` no informó errores.
  - Se conservaron las vistas, las consolas y las pertenencias (4, 8 y 7 en la
    base en uso), y se sembraron las cuatro etiquetas.
  - No se relanzó ningún programa.
- **Revisión visual** en 160×45 y 80×24, en español y en inglés, comparada con
  la maqueta. Con menos de 44 columnas en la columna izquierda, Minimizadas
  oculta la fecha y conserva la consola y la etiqueta.
