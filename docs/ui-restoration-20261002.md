# Restauración de la interfaz — 2026-10-02

ShellStudio abre directamente una vista tmux siguiendo el diseño de sesiones:
árbol de archivos a la izquierda, atajos clicables en ese panel, cabeceras de
consola, menús contextuales y diálogos manejables con teclado y mouse. Las
pestañas exteriores ShellStudio, Views, Notes y Extensions se retiraron.
Notas se abre desde F3; las extensiones, con `shellstudio extensions manage`.

## Correcciones

- F3 crea consolas y conserva el foco en la consola nueva.
- F2 reúne consolas, disposiciones, vistas y otras acciones. En terminales
  pequeñas desplaza el contenido para mantener visible la opción seleccionada.
- F5, F7 y F9 recuperan los diálogos de distribución, vistas y cierre confirmado.
- Los menús de cabecera admiten clic izquierdo y derecho. Arrastrar una cabecera
  intercambia las consolas y guarda el orden bajo el mismo bloqueo del render.
- Arrastrar un archivo inserta su ruta. Los bordes se redimensionan al soltar.
- El explorador se restaura al ensanchar la ventana y recupera un ancho legible
  si tmux lo había reducido a unas pocas columnas al reconectar.
- Las consolas terminadas o desconectadas muestran opciones de recuperación.
- Se eliminó una consulta de color ejecutada por Bubble Tea antes de `main`.
  La dependencia mantiene su versión y licencia MIT; el parche está explicado
  en [third_party/bubbletea/SHELLSTUDIO.md](../third_party/bubbletea/SHELLSTUDIO.md).
- Un servidor de programas vacío ya no impide abrir los registros de consolas
  detenidas. Abrir la interfaz no reinicia esos programas.

La migración a esquema 3 conserva los identificadores, las notas y la pertenencia
de las consolas a las vistas; habilita el explorador y guarda su estado por vista.
Dos clientes que miran la misma vista comparten foco y disposición.

## Comprobaciones

Se ejecutó `make check`: pruebas Go, detector de carreras, integración MCP sin
red, ocho escenarios con PTY/tmux real, `go vet`, diez escenarios del instalador
y revisión de publicación sin hallazgos. Los escenarios PTY cubren creación,
menús, clics, arrastres, posiciones persistidas, distribución, confirmación de
cierre, reconexión, tamaños pequeños, supervivencia de procesos y notas.

Después se comprobaron con el detector de carreras los ajustes finales de
guardado de notas y rechazo de enlaces simbólicos. Los escritores de notas
serializan sus cambios al modo de sincronización para conservar la garantía de
`Saved`. Las preferencias y los metadatos tienen las características de
persistencia descritas en [architecture.md](architecture.md).

Las capturas sintéticas de bytes están en `.work/final-review/.work/ui-startup.pty`
y `.work/final-review/.work/ui-restoration.pty`. El comando `version` debe emitir
sólo su texto; la prueba de consolas responde a las consultas de tmux y comprueba
que los informes de dispositivo, color y movimiento del mouse no aparezcan como
entrada visible de la consola.

La compilación final se hizo desde una copia aislada en `.work/final-review`,
comparada con el código revisado. La instalación reemplaza atómicamente
`~/.local/bin/shellstudio`. Los respaldos del binario y las copias verificadas de
la base de datos están en `.work/install-backup`.

La prueba con los datos existentes comprobó F2, la integridad de SQLite y la
conservación de consolas, pertenencias y notas. En ese momento el servidor real
no tenía programas en ejecución; su estado se conservó. La supervivencia de
procesos activos se comprobó con consolas sintéticas.

Las pruebas se hicieron en Linux con PTY y tmux. No constituyen una comprobación
manual de todos los emuladores de terminal ni de una conexión SSH desde Windows.
