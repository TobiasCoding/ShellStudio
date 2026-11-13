// SPDX-License-Identifier: GPL-3.0-only
package i18n

// catalog holds every translated string: {Spanish, English}. Keys are
// grouped by the screen that shows them.
var catalog = map[string]entry{
	// Top bar (views server status line).
	"top.views":   {"VISTAS", "VIEWS"},
	"top.new":     {"Nueva consola", "New console"},
	"top.arrange": {"Distribuir", "Arrange"},
	"top.more":    {"Más", "More"},

	// Top bar menus.
	"menu.add_existing":     {"Agregar existente…", "Add existing…"},
	"menu.arrange_marked":   {"Distribuir marcadas…", "Arrange marked…"},
	"menu.new_view":         {"Nueva vista", "New view"},
	"menu.other_views":      {"Otras vistas", "Other views"},
	"menu.toggle_explorer":  {"Mostrar u ocultar explorador", "Show or hide explorer"},
	"menu.toggle_minimized": {"Mostrar u ocultar Minimizadas", "Show or hide Minimized"},
	"menu.tags":             {"Etiquetas…", "Tags…"},
	"menu.ports":            {"Reenviar puertos", "Forward ports"},
	"menu.permissions":      {"Permisos de agentes", "Agent permissions"},
	"menu.language":         {"Idioma", "Language"},
	"menu.quit":             {"Salir", "Quit"},
	"menu.rename_view":      {"Renombrar vista", "Rename view"},
	"menu.close_view":       {"Cerrar vista", "Close view"},

	// Console header and its menu.
	"console.minimize":    {"Minimizar", "Minimize"},
	"console.maximize":    {"Maximizar", "Maximize"},
	"console.unmaximize":  {"Restaurar tamaño", "Restore size"},
	"console.tag":         {"Etiqueta", "Tag"},
	"console.description": {"Descripción…", "Description…"},
	"console.no_tag":      {"Sin etiqueta", "No tag"},
	"console.manage_tags": {"Gestionar…", "Manage…"},

	// Explorer.
	"explorer.title":     {"ARCHIVOS", "FILES"},
	"explorer.search":    {"Buscar archivos", "Search files"},
	"explorer.no_match":  {"Sin coincidencias", "No matches"},
	"explorer.truncated": {"Se muestran los primeros %d resultados", "Showing the first %d results"},

	// Minimized panel.
	"min.title":            {"MINIMIZADAS", "MINIMIZED"},
	"min.search":           {"Buscar consolas", "Search consoles"},
	"min.filter_tag":       {"Etiqueta", "Tag"},
	"min.all_tags":         {"Todas las etiquetas", "All tags"},
	"min.columns":          {"Columnas", "Columns"},
	"min.col.created":      {"Creada", "Created"},
	"min.col.console":      {"Consola", "Console"},
	"min.col.tag":          {"Etiqueta", "Tag"},
	"min.col.folder":       {"Carpeta", "Folder"},
	"min.col.status":       {"Estado", "Status"},
	"min.col.minimized_at": {"Minimizada el", "Minimized on"},
	"min.empty":            {"No hay consolas minimizadas", "No minimized consoles"},
	"min.no_match":         {"Ninguna consola coincide", "No console matches"},
	"min.restore":          {"Restaurar", "Restore"},
	"min.rename":           {"Renombrar", "Rename"},
	"min.restore_in":       {"Restaurar en otra vista", "Restore in another view"},
	"min.close_panel":      {"Cerrar panel", "Close panel"},
	"min.kill":             {"Matar consola", "Kill console"},
	"min.kill_confirm":     {"¿Matar %s? El programa termina y la consola se borra.", "Kill %s? Its program ends and the console is deleted."},

	// Console status.
	"status.running":  {"En ejecución", "Running"},
	"status.finished": {"Terminada", "Finished"},
	"status.stopped":  {"Detenida", "Stopped"},

	// Tags dialog.
	"tags.title":              {"ETIQUETAS", "TAGS"},
	"tags.help":               {"N nueva | F2 renombrar | C color | S atajo | [ ] mover | Supr borrar | Esc cerrar", "N new | F2 rename | C color | S shortcut | [ ] move | Del delete | Esc close"},
	"tags.new":                {"Nueva etiqueta", "New tag"},
	"tags.key_title":          {"ATAJO DE %s", "SHORTCUT OF %s"},
	"tags.key_detail":         {"Pulsa la combinación que pone esta etiqueta a la consola enfocada (y se la quita si ya la tiene): Alt+letra o número, o F1–F12 con Ctrl, Alt o Shift.", "Press the keys that give the focused console this tag (and remove it when it already has it): Alt+letter or digit, or F1–F12 with Ctrl, Alt or Shift."},
	"tags.key_current":        {"Atajo actual: %s", "Current shortcut: %s"},
	"tags.key_none":           {"ninguno", "none"},
	"tags.key_keys":           {"Retroceso/Supr quitar el atajo · Esc cancelar", "Backspace/Del remove the shortcut · Esc cancel"},
	"tags.key_invalid":        {"Usa Alt+letra o número, o F1–F12 con Ctrl, Alt o Shift.", "Use Alt+letter or digit, or F1–F12 with Ctrl, Alt or Shift."},
	"tags.key_reserved":       {"%s ya es un atajo de ShellStudio.", "%s is already a ShellStudio shortcut."},
	"tags.key_taken":          {"%s ya es el atajo de %s.", "%s is already the shortcut of %s."},
	"tags.key_set":            {"%s pone la etiqueta %s.", "%s now applies the tag %s."},
	"tags.key_removed":        {"%s ya no tiene atajo.", "%s no longer has a shortcut."},
	"tags.key_no_console":     {"Enfoca una consola para etiquetarla.", "Focus a console to tag it."},
	"tags.rename":             {"Renombrar etiqueta", "Rename tag"},
	"tags.color":              {"Color de la etiqueta", "Tag color"},
	"tags.name_detail":        {"Nombre de la etiqueta (1–24 caracteres).", "Tag name (1–24 characters)."},
	"tags.delete_confirm_one": {"¿Borrar la etiqueta %s? Una consola quedará sin etiqueta.", "Delete the tag %s? One console will be left untagged."},
	"tags.delete_confirm":     {"¿Borrar la etiqueta %s? %d consolas quedarán sin etiqueta.", "Delete the tag %s? %d consoles will be left untagged."},
	"tags.empty":              {"No hay etiquetas. N crea una.", "No tags. N creates one."},
	"tags.invalid_name":       {"usa de 1 a 24 caracteres, sin caracteres de control ni #", "use 1–24 characters, without control characters or #"},
	"tags.duplicate":          {"ya existe una etiqueta %s", "a tag %s already exists"},
	"tags.invalid_color":      {"elige un color de la paleta", "choose a color from the palette"},
	"tags.uses":               {"%d consolas", "%d consoles"},
	"tags.uses_one":           {"1 consola", "1 console"},
	"tags.choose_title":       {"ETIQUETA DE %s", "TAG OF %s"},
	"tags.small":              {"Agranda la terminal. Esc: salir", "Enlarge the terminal. Esc: leave"},
	// Shortcut hints: each one is clickable and does what its key does.
	"tags.hint_new":    {"N nueva", "N new"},
	"tags.hint_rename": {"F2 renombrar", "F2 rename"},
	"tags.hint_key":    {"S atajo", "S shortcut"},
	"tags.hint_color":  {"C color", "C color"},
	"tags.hint_up":     {"[ subir", "[ up"},
	"tags.hint_down":   {"] bajar", "] down"},
	"tags.hint_delete": {"Supr borrar", "Del delete"},
	"tags.hint_close":  {"Esc cerrar", "Esc close"},
	"tags.hint_pick":   {"←→ elegir", "←→ choose"},
	"tags.hint_choose": {"Enter elegir", "Enter choose"},
	"tags.hint_accept": {"Enter aceptar", "Enter accept"},
	"tags.hint_cancel": {"Esc cancelar", "Esc cancel"},

	// Description dialog.
	"describe.title":   {"DESCRIPCIÓN", "DESCRIPTION"},
	"describe.detail":  {"Texto breve bajo el nombre de la consola (hasta 120 caracteres).", "Short text under the console name (up to 120 characters)."},
	"describe.invalid": {"usa hasta 120 caracteres sin caracteres de control", "use up to 120 characters without control characters"},

	// Language dialog.
	"lang.title": {"IDIOMA", "LANGUAGE"},
	"lang.auto":  {"Automático (según el sistema)", "Automatic (system)"},
	"lang.es":    {"Español", "Spanish"},
	"lang.en":    {"Inglés", "English"},

	// Errors shown to the user by the new actions.
	"err.console_gone":   {"la consola ya no existe", "the console no longer exists"},
	"err.not_member":     {"la consola no pertenece a esta vista", "the console does not belong to this view"},
	"err.tag_gone":       {"la etiqueta ya no existe", "the tag no longer exists"},
	"err.view_gone":      {"la vista ya no existe", "the view no longer exists"},
	"err.unknown_kind":   {"tipo desconocido: %s", "unknown kind: %s"},
	"err.cannot_restore": {"no se pudo restaurar: %s", "could not restore: %s"},

	// tmux formats, menus and messages (mux/config.go). They are embedded
	// in tmux formats: no #, comma, brace, semicolon or quote, and a menu
	// label never starts with "-" (TestTmuxTextIsSafe).
	"tmux.header.finished": {"TERMINADA · Enter: opciones", "FINISHED · Enter: options"},
	"tmux.dead.finished":   {"Terminada", "Finished"},
	"tmux.dead.signal":     {"señal", "signal"},
	"tmux.dead.exit":       {"salida", "exit"},
	"tmux.dead.options":    {"Enter o clic derecho: reiniciar · renombrar · cerrar panel · matar consola", "Enter or right click: restart · rename · close panel · kill console"},
	"tmux.dead.title":      {"terminada", "finished"},
	"tmux.restart":         {"Reiniciar", "Restart"},
	"tmux.rename":          {"Renombrar (F2)", "Rename (F2)"},
	"tmux.close_listed":    {"Cerrar panel (la consola sigue en la lista)", "Close panel (the console stays in the list)"},
	"tmux.kill":            {"Matar consola", "Kill console"},
	"tmux.panel_ended":     {"Este panel terminó", "This panel ended"},
	"tmux.panel_options":   {"Enter o clic derecho: rehacer · cerrar panel", "Enter or right click: redo · close panel"},
	"tmux.move_before":     {"Mover antes (Alt+<)", "Move before (Alt+<)"},
	"tmux.move_after":      {"Mover después (Alt+>)", "Move after (Alt+>)"},
	"tmux.new_console":     {"Nueva consola (F3)", "New console (F3)"},
	"tmux.replace":         {"Matar y reemplazar aquí", "Kill and replace here"},
	"tmux.ports":           {"Reenviar puertos a tu PC", "Forward ports to your PC"},
	"tmux.close_running":   {"Cerrar panel (la consola sigue en ejecución)", "Close panel (the console keeps running)"},
	"tmux.kill_f9":         {"Matar consola (F9)", "Kill console (F9)"},
	"tmux.view_menu":       {"Menú de la vista (F1)", "View menu (F1)"},
	"tmux.search":          {"Buscar (/)", "Search (/)"},
	"tmux.edit_nano":       {"Editar la selección con nano (E)", "Edit the selection with nano (E)"},
	"tmux.rename_sel":      {"Renombrar la selección (F2)", "Rename the selection (F2)"},
	"tmux.reload":          {"Recargar carpetas (R)", "Reload folders (R)"},
	"tmux.hide_explorer":   {"Ocultar explorador", "Hide explorer"},
	"tmux.redo_panel":      {"Rehacer el panel", "Redo the panel"},
	"tmux.close_panel":     {"Cerrar panel", "Close panel"},
	"tmux.drop_path":       {"Suelta sobre una consola para escribir la ruta", "Drop on a console to type the path"},
	"tmux.drop_swap":       {"Suelta sobre otra consola para intercambiarlas", "Drop on another console to swap them"},

	// Layouts (display only: mux.LayoutKeys keeps the English labels).
	"layout.even-horizontal": {"Columnas", "Columns"},
	"layout.even-vertical":   {"Filas", "Rows"},
	"layout.tiled":           {"Cuadrícula", "Grid"},
	"layout.main-vertical":   {"Activa a la izquierda", "Active on the left"},
	"layout.main-horizontal": {"Activa arriba", "Active on top"},

	// Console status inside list items ("api | running").
	"status.word.running":  {"en ejecución", "running"},
	"status.word.stopped":  {"detenida", "stopped"},
	"status.word.finished": {"terminada", "finished"},

	// View menu (F1) and the dialogs it opens.
	"ui.menu.consoles":       {"CONSOLAS", "CONSOLES"},
	"ui.menu.layout":         {"DISTRIBUCIÓN", "LAYOUT"},
	"ui.menu.views":          {"VISTAS", "VIEWS"},
	"ui.menu.more":           {"MÁS", "MORE"},
	"ui.menu.new_kind":       {"Nueva consola %s", "New %s"},
	"ui.menu.in_view":        {"Consolas de esta vista…", "Consoles in this view…"},
	"ui.menu.new_view":       {"Nueva vista…", "New view…"},
	"ui.menu.other_views":    {"Otras vistas…", "Other views…"},
	"ui.menu.explorer":       {"Explorador: %s", "Explorer: %s"},
	"ui.menu.yes":            {"sí", "yes"},
	"ui.menu.no":             {"no", "no"},
	"ui.menu.ports":          {"Reenviar puertos…", "Forward ports…"},
	"ui.menu.permissions":    {"Permisos de agentes…", "Agent permissions…"},
	"ui.menu.leave":          {"Salir sin detener (F10)", "Leave without stopping (F10)"},
	"ui.menu.back":           {"Volver (Esc)", "Back (Esc)"},
	"ui.menu.title":          {"VISTA %s | %s", "VIEW %s | %s"},
	"ui.menu.hint":           {"Clic o tecla · flechas y Enter", "Click or key · arrows and Enter"},
	"ui.menu.new_kind_name":  {"Nueva consola %s — nombre editable", "New %s console — editable name"},
	"ui.dialog.failed":       {"NO SE PUDO COMPLETAR LA ACCIÓN", "ACTION COULD NOT BE COMPLETED"},
	"ui.dialog.unknown":      {"diálogo desconocido: %s", "unknown dialog: %s"},
	"ui.dialog.editable":     {"Enter usa el sugerido; escribe para cambiarlo.", "Enter uses the suggested one; type to change it."},
	"ui.dialog.keep_name":    {"Enter conserva el nombre; escribe para reemplazarlo.", "Enter keeps the name; type to replace it."},
	"ui.dialog.new_view":     {"Nombre de la vista nueva", "Name of the new view"},
	"ui.dialog.view_folder":  {"Carpeta de la vista nueva", "Folder of the new view"},
	"ui.dialog.folder_hint":  {"Su explorador y las consolas nuevas empiezan ahí.", "Its explorer and new consoles start there."},
	"ui.dialog.new_in":       {"NUEVA CONSOLA EN %s", "NEW CONSOLE IN %s"},
	"ui.dialog.name_of":      {"Nombre de %s (editable)", "Name of %s (editable)"},
	"ui.dialog.name_new":     {"Nombre de la nueva consola %s", "Name of the new %s console"},
	"ui.dialog.replace":      {"REEMPLAZAR %s", "REPLACE %s"},
	"ui.dialog.replace_c":    {"REEMPLAZAR CONSOLA", "REPLACE CONSOLE"},
	"ui.dialog.replace_d":    {"%s termina; la consola nueva ocupa su lugar en todas las vistas", "%s ends; the new console takes its place in every view"},
	"ui.dialog.rename_c":     {"RENOMBRAR CONSOLA", "RENAME CONSOLE"},
	"ui.dialog.kill_c":       {"MATAR CONSOLA", "KILL CONSOLE"},
	"ui.dialog.kill_d":       {"%s — termina el proceso y la quita de todas las vistas", "%s — ends the process and removes it from every view"},
	"ui.dialog.kill_short":   {"%s — termina el proceso", "%s — ends the process"},
	"ui.dialog.arrange":      {"DISTRIBUIR %s | %d/%d marcadas", "ARRANGE %s | %d/%d marked"},
	"ui.dialog.mark_one":     {"Marca al menos una consola.", "Mark at least one console."},
	"ui.dialog.partial":      {"Nombre de la vista parcial (vacío = automático)", "Name of the partial view (empty = automatic)"},
	"ui.dialog.saved_views":  {"VISTAS GUARDADAS", "SAVED VIEWS"},
	"ui.dialog.view_item":    {"%s | %d consolas | %s", "%s | %d consoles | %s"},
	"ui.dialog.delete_view":  {"BORRAR VISTA: %s", "DELETE VIEW: %s"},
	"ui.dialog.keep_running": {"Sus consolas siguen en ejecución.", "Its consoles keep running."},
	"ui.dialog.add":          {"AGREGAR CONSOLA", "ADD CONSOLE"},
	"ui.dialog.consoles_of":  {"CONSOLAS DE %s", "CONSOLES OF %s"},
	"ui.dialog.removed":      {"Quitada; sigue en ejecución.", "Removed; it keeps running."},
	"ui.dialog.full_name":    {"Nuevo nombre completo de la consola", "New full name of the console"},
	"ui.dialog.replaced":     {"Reemplazada: %s → %s", "Replaced: %s → %s"},
	"ui.dialog.ended":        {"Consola terminada: %s", "Console ended: %s"},
	"ui.dialog.pick_replace": {"elige una consola para reemplazar", "choose a console to replace"},
	"ui.dialog.changed":      {"la consola seleccionada cambió durante el diálogo; abre el menú otra vez", "the selected console changed during the dialog; open the menu again"},

	// Agent permissions dialog.
	"ui.perm.title":   {"PERMISOS DE AGENTES", "AGENT PERMISSIONS"},
	"ui.perm.agent":   {"PERMISOS DE %s", "%s PERMISSIONS"},
	"ui.perm.invalid": {"no válido: %s", "invalid: %s"},
	"ui.perm.confirm": {"¿Iniciar las nuevas consolas de %s con %s?", "Start new %s consoles with %s?"},
	"ui.perm.danger":  {"%s El agente puede cambiar o borrar cualquier archivo al que tu usuario tenga acceso. Úsalo solo en un entorno aislado.", "%s The agent can change or delete any file your user can reach. Use it only in an isolated environment."},
	"ui.perm.set":     {"%s: %s para las consolas nuevas", "%s: %s for new consoles"},

	// Agent permission presets, as shown (app.PermissionText). The JSON of
	// "shellstudio permissions" keeps the English labels.
	"perm.claude.default":                   {"Predeterminado del agente", "Agent default"},
	"perm.claude.default.detail":            {"Sin flag: decide la configuración propia de Claude.", "No flag: Claude's own settings decide."},
	"perm.claude.plan":                      {"Plan", "Plan"},
	"perm.claude.plan.detail":               {"Lee y planifica; sin ediciones ni comandos.", "Read and plan; no edits or commands."},
	"perm.claude.acceptEdits":               {"Aceptar ediciones", "Accept edits"},
	"perm.claude.acceptEdits.detail":        {"Las ediciones de archivos se aceptan; los comandos siguen preguntando.", "File edits are accepted; commands still ask."},
	"perm.claude.auto":                      {"Auto", "Auto"},
	"perm.claude.auto.detail":               {"Claude aprueba las acciones que considera seguras.", "Claude approves actions it judges safe."},
	"perm.claude.dontAsk":                   {"No preguntar", "Don't ask"},
	"perm.claude.dontAsk.detail":            {"Se rechaza todo lo que no esté permitido de antemano.", "Anything not allowed in advance is denied."},
	"perm.claude.bypassPermissions":         {"Omitir permisos", "Bypass permissions"},
	"perm.claude.bypassPermissions.detail":  {"Todas las acciones se ejecutan sin preguntar.", "Every action runs without asking."},
	"perm.codex.default":                    {"Predeterminado del agente", "Agent default"},
	"perm.codex.default.detail":             {"Sin flag: decide la configuración propia de Codex.", "No flag: Codex's own settings decide."},
	"perm.codex.read-only":                  {"Solo lectura", "Read only"},
	"perm.codex.read-only.detail":           {"Sandbox de solo lectura; pregunta antes de cualquier otra cosa.", "Read-only sandbox; asks before anything else."},
	"perm.codex.workspace-write":            {"Escritura en el espacio de trabajo", "Workspace write"},
	"perm.codex.workspace-write.detail":     {"Edita en el espacio de trabajo; pregunta fuera de él.", "Edits in the workspace; asks outside it."},
	"perm.codex.workspace-never-ask":        {"Espacio de trabajo, sin preguntar", "Workspace, never ask"},
	"perm.codex.workspace-never-ask.detail": {"Edita en el espacio de trabajo; los fallos vuelven a Codex.", "Edits in the workspace; failures return to Codex."},
	"perm.codex.full-access":                {"Acceso total", "Full access"},
	"perm.codex.full-access.detail":         {"Sin sandbox y sin aprobaciones.", "No sandbox and no approvals."},

	// Shortcut lines of the lists. The key stays first and untranslated:
	// HintKey reads it.
	"ui.help.mark":         {"Space  Marcar / desmarcar", "Space  Mark / unmark"},
	"ui.help.mark_all":     {"A  Marcar todas", "A  Mark all"},
	"ui.help.unmark_all":   {"0  Desmarcar todas", "0  Unmark all"},
	"ui.help.mark_active":  {"S  Marcar solo la activa", "S  Mark only the active one"},
	"ui.help.back":         {"Q / Esc  Volver", "Q / Esc  Back"},
	"ui.help.cancel":       {"Q / Esc  Cancelar", "Q / Esc  Cancel"},
	"ui.help.partial":      {"Selección parcial: crea otra vista.", "Partial selection: creates another view."},
	"ui.help.open":         {"Enter  Abrir", "Enter  Open"},
	"ui.help.view_cons":    {"E  Consolas de la vista", "E  Consoles of the view"},
	"ui.help.new_view":     {"N  Nueva vista", "N  New view"},
	"ui.help.delete_view":  {"Del  Borrar vista", "Del  Delete view"},
	"ui.help.create":       {"Enter  Crear", "Enter  Create"},
	"ui.help.add":          {"Enter  Agregar", "Enter  Add"},
	"ui.help.change":       {"Enter  Cambiar", "Enter  Change"},
	"ui.help.applies":      {"Se aplica a las consolas que se creen después.", "Applies to consoles created afterwards."},
	"ui.help.use_mode":     {"Enter  Usar en consolas nuevas", "Enter  Use for new consoles"},
	"ui.help.create_kind":  {"%s  Crear %s", "%s  Create %s"},
	"ui.help.add_existing": {"A  Agregar consola existente", "A  Add existing console"},
	"ui.help.remove":       {"-  Quitar de esta vista", "-  Remove from this view"},
	"ui.help.rename":       {"F2  Renombrar", "F2  Rename"},
	"ui.help.replace":      {"R  Matar y reemplazar aquí", "R  Kill and replace here"},
	"ui.help.kill":         {"M/Del  Matar consola", "M/Del  Kill console"},
	"ui.help.go_to":        {"Enter  Ir a la consola seleccionada", "Enter  Go to the selected console"},
	"ui.help.choose_kind":  {"Enter  Elegir tipo", "Enter  Choose kind"},

	// Shared widgets (widgets.go).
	"ui.choose.count":   {"%d/%d | ↑↓ elegir", "%d/%d | ↑↓ choose"},
	"ui.choose.small":   {"Agranda la terminal. Q: salir", "Enlarge the terminal. Q: leave"},
	"ui.choose.more":    {"Tab  Más atajos (%d/%d)", "Tab  More shortcuts (%d/%d)"},
	"ui.choose.empty":   {"No hay elementos.", "No items."},
	"ui.message.keys":   {"Enter / Esc  Cerrar    ↑↓  Desplazar", "Enter / Esc  Close    ↑↓  Scroll"},
	"ui.confirm.ok":     {"Enter  Confirmar", "Enter  Confirm"},
	"ui.confirm.cancel": {"Esc  Cancelar", "Esc  Cancel"},
	"ui.confirm.small":  {"Agranda la terminal para confirmar.", "Enlarge the terminal to confirm."},
	"ui.prompt.accept":  {"Enter  Aceptar", "Enter  Accept"},

	// Forward ports and upload dialogs.
	"ui.ports.none_cmd":  {"# Ningún puerto seleccionado", "# No ports selected"},
	"ui.ports.no_copy":   {"No se pudo copiar: %s", "Could not copy: %s"},
	"ui.ports.copied":    {"Se actualizó el portapapeles de tu PC.", "Your PC clipboard was updated."},
	"ui.ports.title":     {"REENVIAR PUERTOS", "FORWARD PORTS"},
	"ui.ports.scanning":  {"Buscando puertos en este servidor…", "Looking for ports on this server…"},
	"ui.ports.button":    {"[ Copiar comando ]", "[ Copy command ]"},
	"ui.ports.mark_one":  {"(marca al menos un puerto)", "(mark at least one port)"},
	"ui.ports.keys":      {"C/Enter copiar · Space marcar · A todos/ninguno · R volver a buscar · Q/Esc volver", "C/Enter copy · Space mark · A all/none · R rescan · Q/Esc back"},
	"ui.ports.login":     {"En tu PC (PowerShell o Windows Terminal), conéctate con este comando en lugar del habitual:", "On your PC (PowerShell or Windows Terminal), log in with this command instead of the usual one:"},
	"ui.ports.open":      {"y abre %s en el navegador. Si te conectas con un nombre de host, úsalo en lugar de la IP.", "and open %s in the browser. If you log in with a host name, use it instead of the IP."},
	"ui.ports.permanent": {"Para dejarlo permanente, en ~/.ssh/config de tu PC, dentro del Host que usas:", "To make it permanent, in ~/.ssh/config on your PC, inside the Host you use:"},
	"ui.ports.escape":    {"Sin reconectar (ssh con EnableEscapeCommandline yes): Enter, ~C y %s, uno a la vez.", "Without reconnecting (ssh with EnableEscapeCommandline yes): Enter, ~C and %s, one at a time."},
	"ui.ports.marked":    {"REENVIAR PUERTOS | %d/%d marcados", "FORWARD PORTS | %d/%d marked"},
	"ui.ports.intro":     {`Puertos accesibles en localhost. "web" significa HTTP; elige cuáles reenviar.`, `Ports reachable on localhost. "web" means HTTP; choose which to forward.`},
	"ui.ports.empty":     {"No hay puertos escuchando en localhost.", "No ports are listening on localhost."},
	"ui.upload.title":    {"SUBIR DESDE TU PC CON SCP", "UPLOAD FROM YOUR PC WITH SCP"},
	"ui.upload.source":   {"Origen en tu PC: %s", "Source on your PC: %s"},
	"ui.upload.dest":     {"Destino en este servidor: %s/", "Destination on this server: %s/"},
	"ui.upload.folders":  {"PowerShell detecta las carpetas y agrega -r automáticamente.", "PowerShell detects folders and adds -r automatically."},
	"ui.upload.run":      {"Comando para ejecutar en una pestaña de PowerShell en tu PC:", "Command to run in a PowerShell tab on your PC:"},
	"ui.upload.keys":     {"C/Enter: copiar · Esc: cerrar", "C/Enter: copy · Esc: close"},
	"ui.upload.copied":   {"Copiado al portapapeles de tu PC.", "Copied to your PC clipboard."},

	// Disconnected and empty panes.
	"ui.disc.disconnected": {"%s: la consola se desconectó.", "%s: the console disconnected."},
	"ui.disc.running":      {"La consola sigue en ejecución.", "The console keeps running."},
	"ui.disc.stopped":      {"%s: la consola está detenida.", "%s: the console is stopped."},
	"ui.disc.stopped_d":    {"Su programa no está en ejecución, por ejemplo después de reiniciar esta máquina.", "Its program is not running, for example after a restart of this machine."},
	"ui.disc.gone":         {"%s: la consola ya no existe.", "%s: the console no longer exists."},
	"ui.disc.reconnect":    {"Enter  Reconectar", "Enter  Reconnect"},
	"ui.disc.start":        {"Enter  Iniciarla de nuevo", "Enter  Start it again"},
	"ui.disc.resumable":    {"Su conversación se puede reanudar.", "Its conversation can be resumed."},
	"ui.disc.resume":       {"Enter  Reanudar la conversación", "Enter  Resume the conversation"},
	"ui.disc.fresh":        {"N      Empezar una conversación nueva", "N      Start a new conversation"},
	"ui.disc.close":        {"Q      Cerrar panel", "Q      Close panel"},
	"ui.disc.kill":         {"M      Matar consola", "M      Kill console"},
	"ui.disc.new":          {"F3     Nueva consola", "F3     New console"},
	"ui.disc.menu":         {"F1     Menú de la vista", "F1     View menu"},
	"ui.disc.connecting":   {"Conectando…", "Connecting…"},
	"ui.disc.starting":     {"Iniciando…", "Starting…"},
	"ui.disc.closing":      {"Cerrando…", "Closing…"},
	"ui.empty.name":        {"Vista vacía", "Empty view"},
	"ui.empty.title":       {"Esta vista está vacía", "This view is empty"},
	"ui.empty.create":      {"F3  Crear una consola aquí", "F3  Create a console here"},
	"ui.empty.menu":        {"F1  Menú de esta vista", "F1  Menu of this view"},
	"ui.empty.switch":      {"Ctrl+Shift+←/→  Cambiar de vista", "Ctrl+Shift+←/→  Change view"},
	"ui.empty.leave":       {"F10  Salir; las consolas siguen en ejecución", "F10  Leave; consoles keep running"},

	// Explorer tree, preview and error panel.
	"ui.tree.not_regular": {"No es un archivo regular.", "Not a regular file."},
	"ui.tree.binary":      {"Archivo binario.", "Binary file."},
	"ui.tree.limited":     {"[Vista previa limitada a 128 KiB]", "[Preview limited to 128 KiB]"},
	"ui.tree.preview":     {"Esc: árbol | ↑↓ leer", "Esc: tree | ↑↓ read"},
	"ui.tree.indexing":    {"Indexando…", "Indexing…"},
	"ui.tree.folder":      {"Es una carpeta.", "It is a folder."},
	"ui.tree.in_nano":     {"En nano; al salir se cierra la consola.", "In nano; leaving it closes the console."},
	"ui.tree.error":       {"Error", "Error"},
	"ui.tree.errors":      {"ERRORES DE LA APP", "APP ERRORS"},
	"ui.tree.err_close":   {"[X] Cerrar", "[X] Close"},
	"ui.tree.err_fix":     {"[F] Corregir", "[F] Fix this"},
	"ui.tree.sent":        {"Enviado a %s.", "Sent to %s."},
	"ui.tree.pick_folder": {"Primero haz clic en la carpeta de destino.", "First click the destination folder."},
	"ui.tree.no_panel":    {"No se pudo identificar el panel del árbol.", "The tree panel could not be identified."},
	"ui.tree.no_ssh":      {"No se pudo identificar tu conexión SSH.", "Your SSH connection could not be identified."},
	"ui.tree.scp_ready":   {"Comando SCP preparado para esa carpeta.", "SCP command prepared for that folder."},
	"ui.tree.rename":      {"Renombrar", "Rename"},
	"ui.tree.renamed":     {"Renombrado.", "Renamed."},
	"ui.tree.path_typed":  {"Ruta escrita.", "Path written."},
	"ui.tree.no_console":  {"No hay consolas en la vista.", "No console in the view."},

	// Errors shown to the user (internal/app).
	"err.no_agent":         {"ni Claude ni Codex están instalados en el PATH", "neither Claude nor Codex is installed in PATH"},
	"err.error_changed":    {"el error visible cambió; revisa el panel otra vez", "the visible error changed; check the panel again"},
	"err.outside_view":     {"la ruta está fuera de la carpeta de la vista", "the path is outside the view folder"},
	"err.not_file":         {"no es un archivo", "not a file"},
	"err.too_large":        {"demasiado grande para nano (más de 50 MiB)", "too large for nano (more than 50 MiB)"},
	"err.not_text":         {"no es un archivo de texto", "not a text file"},
	"err.no_nano":          {"nano no está instalado", "nano is missing"},
	"err.root_rename":      {"la carpeta raíz no se renombra desde aquí", "the root folder is not renamed from here"},
	"err.invalid_name":     {"nombre no válido", "invalid name"},
	"err.exists":           {"%s ya existe", "%s already exists"},
	"err.view_name":        {"usa de 1 a 80 caracteres para el nombre de la vista", "use 1–80 characters for the view name"},
	"err.view_exists":      {"la vista %s ya existe", "the view %s already exists"},
	"err.read_panels":      {"no se pudieron leer los paneles de la vista", "read view panels"},
	"err.panel_metadata":   {"tmux devolvió metadatos de paneles no válidos; la vista no cambió", "tmux returned invalid panel metadata; the view was not changed"},
	"err.window_metadata":  {"tmux devolvió metadatos de ventana no válidos", "tmux returned invalid window metadata"},
	"err.no_new_pane":      {"tmux no informó un panel nuevo", "tmux did not report a new pane"},
	"err.already_open":     {"ShellStudio ya está abierto en esta terminal; usa F7 para cambiar de vista", "ShellStudio is already open in this terminal; use F7 to change views"},
	"err.needs_terminal":   {"abrir una vista requiere una terminal", "opening a view needs a terminal"},
	"err.client_gone":      {"el cliente ya no está conectado", "the client is no longer connected"},
	"err.panel_not_member": {"el panel seleccionado ya no pertenece a esta vista", "the selected panel no longer belongs to this view"},
	"err.invalid_panel":    {"panel no válido", "invalid panel"},
	"err.panel_orphan":     {"el panel ya no pertenece a ninguna vista", "the panel no longer belongs to a view"},
	"err.not_in_view":      {"esta consola no está dentro de una vista", "this console is not inside a view"},
	"err.no_client":        {"esta vista no tiene un cliente conectado", "this view has no connected client"},
	"err.permission_mode":  {"modo de permisos de %s desconocido: %q", "unknown %s permission mode %q"},
	"err.drop_one":         {"arrastra un solo archivo o carpeta con una ruta absoluta de Windows", "drag a single file or folder with an absolute Windows path"},
	"err.remote_folder":    {"la carpeta remota ya no existe o está fuera de la carpeta de la vista", "the remote folder no longer exists or is outside the view folder"},
	"err.ssh_server":       {"no se pudo identificar el servidor SSH; vuelve a conectar la vista por SSH", "the SSH server could not be identified; reconnect the view through SSH"},
	"err.no_extension":     {"no se encontró la extensión %s", "extension %s not found"},
	"err.name_length":      {"usa de 1 a 80 caracteres", "use 1–80 characters"},
	"err.name_chars":       {"el nombre no puede contener caracteres de control ni #", "the name cannot contain control characters or #"},
	"err.not_in_path":      {"no se encontró %s en el PATH", "%s was not found in PATH"},
	"err.view_not_found":   {"no se encontró la vista", "view not found"},
	"err.console_moved":    {"la consola seleccionada ya no pertenece a esta vista", "the selected console no longer belongs to this view"},
	"err.invalid_layout":   {"distribución no válida", "invalid layout"},

	// Notes and extensions screens (internal/ui).
	"notes.placeholder":   {"Escribe una nota…", "Write a note…"},
	"notes.error":         {"Error: %s", "Error: %s"},
	"notes.saving":        {"Guardando", "Saving"},
	"notes.saved":         {"Guardada", "Saved"},
	"notes.working":       {"Procesando…", "Working…"},
	"notes.done":          {"Listo", "Done"},
	"notes.conflict":      {"Edición simultánea: se conservaron ambas versiones; editando la copia en conflicto", "Concurrent edit: both versions kept; editing the conflict copy"},
	"notes.busy":          {"Espera a que termine la operación en curso antes de cerrar", "Wait for the active operation to finish before closing"},
	"notes.restore_hint":  {"Presiona r para restaurar esta nota", "Press r to restore this note"},
	"notes.new":           {"Nueva nota", "New note"},
	"notes.field_title":   {"Título", "Title"},
	"notes.untitled":      {"Sin título", "Untitled"},
	"notes.search":        {"Buscar notas", "Search notes"},
	"notes.field_text":    {"Texto", "Text"},
	"notes.rename":        {"Renombrar nota", "Rename note"},
	"notes.export":        {"Exportar Markdown", "Export Markdown"},
	"notes.export_dest":   {"Destino (si el archivo existe, se reemplaza)", "Destination (existing file is replaced)"},
	"notes.export_ask":    {"¿Exportar Markdown a %s?\nSi el archivo existe, se reemplaza.", "Export Markdown to %s?\nAn existing file is replaced."},
	"notes.title":         {"NOTAS", "NOTES"},
	"notes.trash_title":   {"NOTAS · Papelera", "NOTES · Trash"},
	"notes.ext_title":     {"EXTENSIONES", "EXTENSIONS"},
	"notes.viewer_title":  {"VISOR", "VIEWER"},
	"notes.text_title":    {"TEXTO", "TEXT"},
	"notes.save":          {"[ Guardar ]", "[ Save ]"},
	"notes.cancel":        {"[ Cancelar ]", "[ Cancel ]"},
	"notes.help_form":     {"Tab campo siguiente · Enter confirmar · Esc cancelar", "Tab next field · Enter confirm · Esc cancel"},
	"notes.help_choice":   {"↑↓ elegir · Enter seleccionar · Esc volver", "↑↓ choose · Enter select · Esc back"},
	"notes.review":        {"Revisar acción", "Review action"},
	"notes.help_confirm":  {"y confirmar · n / Esc cancelar · ↑↓ desplazar", "y confirm · n / Esc cancel · ↑↓ scroll"},
	"notes.help_editor":   {"Autoguardado: 300 ms de inactividad / 1 s continuo · Ctrl+S guardar · Esc guardar y salir", "Autosave: 300 ms idle / 1 s continuous · Ctrl+S save · Esc save and leave"},
	"notes.help_ext":      {"i instalar · e habilitar · d deshabilitar · c perfil · m MCP · u desinstalar · X purgar · Esc", "i install · e enable · d disable · c profile · m MCP · u uninstall · X purge · Esc"},
	"notes.help_viewer":   {"↑↓ / PgUp/PgDn desplazar · / filtrar visor · r actualizar · Esc volver · q salir", "↑↓ / PgUp/PgDn scroll · / filter viewer · r refresh · Esc back · q quit"},
	"notes.help_notes":    {"n nueva · Enter editar · / buscar · r renombrar/restaurar · d papelera · t ver papelera · e exportar · q salir", "n new · Enter edit · / search · r rename/restore · d trash · t trash view · e export · q quit"},
	"notes.help_exts":     {"Enter detalles · a importar archivo/HTTPS · n comando existente · q salir", "Enter details · a import file/HTTPS · n existing command · q quit"},
	"notes.empty":         {"Todavía no hay nada aquí. Usa las acciones de abajo para empezar.", "Nothing here yet. Use the actions below to get started."},
	"notes.loading":       {"Cargando…", "Loading…"},
	"notes.filter":        {"Filtrar visor", "Filter viewer"},
	"notes.ext_invalid":   {"No válida: %s — %s", "Invalid: %s — %s"},
	"notes.ext_enabled":   {"[habilitada]", "[enabled]"},
	"notes.ext_disabled":  {"[deshabilitada]", "[disabled]"},
	"notes.ext_source":    {"Origen: %s", "Source: %s"},
	"notes.ext_import":    {"Importar extensión", "Import extension"},
	"notes.ext_manifest":  {"Archivo de manifiesto o URL HTTPS", "Manifest file or HTTPS URL"},
	"notes.ext_import_ok": {"¿Importar este manifiesto? (Todavía no se ejecuta ningún comando.)", "Import this manifest? (No commands run yet.)"},
	"notes.ext_install":   {"¿Instalar y habilitar?", "Install and enable?"},
	"notes.ext_uninstall": {"¿Desinstalar %s?\nLas consolas en ejecución continúan. Los datos se conservan.", "Uninstall %s?\nRunning consoles continue. Data is retained."},
	"notes.ext_purge":     {"BORRAR también los datos guardados de esta extensión.", "DELETE this extension's stored data as well."},
	"notes.ext_profile":   {"Guardar perfil predeterminado · %s", "Save default profile · %s"},
	"notes.ext_prof_name": {"Nombre del perfil", "Profile name"},
	"notes.ext_exec":      {"Ejecutable", "Executable"},
	"notes.ext_args":      {"Argumentos (arreglo JSON)", "Arguments (JSON array)"},
	"notes.ext_env":       {"Entorno (objeto JSON)", "Environment (JSON object)"},
	"notes.ext_no_mcp":    {"Esta extensión no tiene servidor MCP", "This extension has no MCP server"},
	"notes.ext_mcp_first": {"Primero instala y habilita la extensión MCP", "Install and enable the MCP extension first"},
	"notes.ext_register":  {"Registrar cliente MCP", "Register MCP client"},
	"notes.ext_absent":    {"Cliente ausente: solo se guarda un registro pendiente. Vuelve a aplicarlo después de instalar el cliente.", "Client absent: save a pending registration only. Apply again after installing the client."},
	"notes.ext_command":   {"Extensión de comando existente", "Existing command extension"},
	"notes.ext_id":        {"ID (minúsculas, dígitos, guiones)", "ID (lowercase letters, digits, hyphens)"},
	"notes.ext_desc":      {"Descripción", "Description"},
	"notes.ext_my_cmd":    {"Mi comando", "My command"},
	"notes.ext_add_cmd":   {"¿Agregar este comando?", "Add this command?"},

	// Command line (cmd/shellstudio). Command syntax stays as typed.
	"cli.usage":           {"uso: %s", "usage: %s"},
	"cli.usage_open":      {"uso: shellstudio [DIRECTORY] o shellstudio open DIRECTORY", "usage: shellstudio [DIRECTORY] or shellstudio open DIRECTORY"},
	"cli.launch_view":     {"launch requiere --view ID", "launch requires --view ID"},
	"cli.unknown_viewer":  {"visor desconocido", "unknown viewer"},
	"cli.unknown_command": {"comando desconocido %q; usa --help", "unknown command %q; use --help"},
	"cli.other_views":     {"las consolas están en vistas distintas", "the consoles are in different views"},
	"cli.valid":           {"Válido: %s %s (esquema %d)", "Valid: %s %s (schema %d)"},
	"cli.backup_ok":       {"Copia de seguridad verificada: %s", "Verified backup: %s"},
	"cli.perm_warning":    {"Aviso: las nuevas consolas de %s se ejecutan sin aprobaciones. Úsalo solo en un entorno aislado.", "Warning: new %s consoles run without approvals. Use only in an isolated environment."},
	"cli.no_flags":        {"sin flags", "no flags"},
	"cli.perm_set":        {"Nuevas consolas de %s: %s (%s)", "New %s consoles: %s (%s)"},
	"cli.report_locate":   {"no se encontró la carpeta de informes; usa report --stdout", "cannot locate report directory; use report --stdout"},
	"cli.report_create":   {"no se pudo crear la carpeta de informes; usa report --stdout", "cannot create report directory; use report --stdout"},
	"cli.report_path":     {"Informe de diagnóstico: %s", "Diagnostic report: %s"},
	"cli.update_off":      {"ShellStudio: no se pudo comprobar si hay actualizaciones; se sigue con la versión instalada.", "ShellStudio: update check unavailable; continuing with the installed version."},
	"cli.up_to_date":      {"ShellStudio está actualizado (%s).", "ShellStudio is up to date (%s)."},
	"cli.available":       {"ShellStudio %s está disponible (instalada: %s).", "ShellStudio %s is available (installed: %s)."},
	"cli.update_tty":      {"confirmar la actualización requiere una terminal", "update confirmation needs a terminal"},
	"cli.keeping":         {"Se conserva la versión instalada.", "Keeping the installed version."},
	"cli.downloading":     {"Descargando y verificando la actualización…", "Downloading and verifying the update…"},
	"cli.update_failed":   {"La actualización falló: %v \nSe sigue con la versión instalada.", "Update failed: %v \nContinuing with the installed version."},
	"cli.updated":         {"Actualizado a %s.", "Updated to %s."},
	"cli.help":            {helpES, helpEN},
	// Contexts of recorded errors, translated only where the error panel
	// shows them: the diagnostics log keeps the English name.
	"ctx.Add console":             {"Agregar consola", "Add console"},
	"ctx.Agent permissions":       {"Permisos de agentes", "Agent permissions"},
	"ctx.Change view":             {"Cambiar de vista", "Change view"},
	"ctx.Close editor":            {"Cerrar el editor", "Close editor"},
	"ctx.Consoles of the view":    {"Consolas de la vista", "Consoles of the view"},
	"ctx.Tag":                     {"Etiqueta", "Tag"},
	"ctx.Description":             {"Descripción", "Description"},
	"ctx.Dialog":                  {"Diálogo", "Dialog"},
	"ctx.Drop path":               {"Soltar ruta", "Drop path"},
	"ctx.Error panel":             {"Panel de errores", "Error panel"},
	"ctx.Explorer shortcut":       {"Atajo del explorador", "Explorer shortcut"},
	"ctx.Explorer: edit file":     {"Explorador: editar archivo", "Explorer: edit file"},
	"ctx.Explorer: insert path":   {"Explorador: insertar ruta", "Explorer: insert path"},
	"ctx.Explorer: rename":        {"Explorador: renombrar", "Explorer: rename"},
	"ctx.Explorer: upload file":   {"Explorador: subir archivo", "Explorer: upload file"},
	"ctx.Layout":                  {"Disposición", "Layout"},
	"ctx.Menu":                    {"Menú", "Menu"},
	"ctx.Move console":            {"Mover consola", "Move console"},
	"ctx.New console":             {"Nueva consola", "New console"},
	"ctx.Panel action":            {"Acción del panel", "Panel action"},
	"ctx.Panels":                  {"Paneles", "Panels"},
	"ctx.Reconnect":               {"Reconectar", "Reconnect"},
	"ctx.Resize explorer":         {"Redimensionar el explorador", "Resize explorer"},
	"ctx.Restore in another view": {"Restaurar en otra vista", "Restore in another view"},
	"ctx.Saved views":             {"Vistas guardadas", "Saved views"},
	"ctx.Upload from the PC":      {"Subir desde la PC", "Upload from the PC"},
	"ctx.View menu":               {"Menú de la vista", "View menu"},
	"ctx.View tab":                {"Pestaña de vista", "View tab"},
	// shellstudio doctor and update.
	"ctx.tmux":             {"tmux", "tmux"},
	"ctx.database":         {"base de datos", "database"},
	"ctx.configuration":    {"configuración", "configuration"},
	"ctx.console server":   {"servidor de consolas", "console server"},
	"doctor.permissions":   {"se esperaban permisos 0700", "expected permissions 0700"},
	"doctor.manifest":      {"manifiesto:", "manifest:"},
	"doctor.not_installed": {"no instalado", "not installed"},
	"doctor.detected":      {"detectado: %s", "detected: %s"},
	"doctor.programs":      {"%d programas conocidos en tmux; los datos de consolas detenidas nunca se relanzan solos", "%d known tmux programs; stopped metadata is never relaunched automatically"},
	"doctor.failed":        {"el diagnóstico encontró fallas", "diagnostics found failures"},
	"update.prompt":        {"ShellStudio %s está disponible (instalada: %s). ¿Instalar ahora? [s/N] ", "ShellStudio %s is available (installed: %s). Install now? [y/N] "},
}

const helpES = `ShellStudio — consolas persistentes en vistas, con un explorador de archivos

Uso:
  shellstudio [DIRECTORY]              Abre la vista de la carpeta actual o de la elegida
  shellstudio open DIRECTORY           Abre una carpeta que se llama como un comando de la CLI
  shellstudio --menu                   Retoma la última vista
  shellstudio update [--check]         Busca versiones nuevas; pregunta antes de instalar
  shellstudio notes                    Notas globales (también F3 → Notes dentro de una vista)
  shellstudio extensions [manage]      Lista las extensiones, o las instala, habilita y configura
  shellstudio permissions [AGENT MODE] Permisos predeterminados de las nuevas consolas de Claude/Codex
  shellstudio viewer KIND [DATABASE]   Visor de solo lectura de agent-chat / agent-gantt
  shellstudio doctor                   Revisa el almacenamiento, las dependencias y las extensiones
  shellstudio report [FILE | --stdout] Exporta un informe de diagnóstico privado para compartir
  shellstudio validate MANIFEST        Valida un manifiesto local o HTTPS
  shellstudio schema                   Muestra el esquema JSON público
  shellstudio backup DESTINATION       Crea y verifica una copia de seguridad de SQLite
  shellstudio new-view [--folder PATH] NAME
  shellstudio views | consoles
  shellstudio launch --view ID [--extension ID] [--profile NAME] [--folder PATH] [--name NAME]
  shellstudio restart [--new-conversation] CONSOLE_ID
                                       Reinicia un programa detenido; Claude y Codex
                                       reanudan su conversación salvo con --new-conversation
  shellstudio stop CONSOLE_ID          Termina un programa y conserva la consola
  shellstudio kill CONSOLE_ID          Termina un programa y borra la consola
  shellstudio version

Dentro de una vista:
  F1 menú de la vista · F2 renombrar · F3 nueva consola · F5 distribuir · F7 vistas · F9 matar consola
  F10 salir (los programas siguen en ejecución) · F4 zoom · F6 / Ctrl+Alt+flechas cambiar de panel
  Shift+F10 menú del panel · Ctrl+RePág/AvPág, Ctrl+Shift+←/→ o F8 cambiar de vista
  Alt+1–5 distribuciones · Alt+< / > mover consola · F1 → W elige los permisos con que empiezan las nuevas consolas de Claude y Codex.
  Haz clic en el encabezado de una consola para ver su menú; arrástralo sobre otra consola para intercambiarlas.
  Arrastra un archivo del explorador sobre una consola para escribir su ruta.

Cerrar la terminal o SSH conserva los programas. Reiniciar la computadora los detiene.
El almacenamiento sigue XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME y XDG_RUNTIME_DIR.
Define SHELLSTUDIO_NO_UPDATE_CHECK=1 para omitir la búsqueda de versiones al iniciar.
ShellStudio es GPL-3.0-only; los servidores MCP incluidos conservan sus licencias MIT.
`

const helpEN = `ShellStudio — persistent consoles in views, with a file explorer

Usage:
  shellstudio [DIRECTORY]              Open the view of the current or selected folder
  shellstudio open DIRECTORY           Open a folder named like a CLI command
  shellstudio --menu                   Resume the last view
  shellstudio update [--check]         Check releases; ask before installing
  shellstudio notes                    Global Notes (also F3 → Notes inside a view)
  shellstudio extensions [manage]      List extensions, or install/enable/configure them
  shellstudio permissions [AGENT MODE] Default permissions of new Claude/Codex consoles
  shellstudio viewer KIND [DATABASE]   Read-only agent-chat / agent-gantt viewer
  shellstudio doctor                   Check storage, dependencies and extensions
  shellstudio report [FILE | --stdout] Export a private diagnostic report to share
  shellstudio validate MANIFEST        Validate a file or HTTPS manifest
  shellstudio schema                   Print the public JSON schema
  shellstudio backup DESTINATION       Create and verify a SQLite backup
  shellstudio new-view [--folder PATH] NAME
  shellstudio views | consoles
  shellstudio launch --view ID [--extension ID] [--profile NAME] [--folder PATH] [--name NAME]
  shellstudio restart [--new-conversation] CONSOLE_ID
                                       Restart a stopped program; Claude and Codex
                                       resume their conversation unless --new-conversation
  shellstudio stop CONSOLE_ID          End a program, keeping the console
  shellstudio kill CONSOLE_ID          End a program and remove the console
  shellstudio version

Inside a view:
  F1 view menu · F2 rename · F3 new console · F5 arrange · F7 views · F9 kill console
  F10 leave (programs keep running) · F4 zoom · F6 / Ctrl+Alt+arrows change panel
  Shift+F10 panel menu · Ctrl+PgUp/PgDn, Ctrl+Shift+←/→ or F8 change view
  Alt+1–5 layouts · Alt+< / > move console · F1 → W chooses the permissions new Claude and Codex consoles start with.
  Click a console header for its menu; drag it onto another console to swap them.
  Drag a file from the explorer onto a console to type its path.

Closing the terminal or SSH preserves programs. Restarting the computer stops them.
Storage follows XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME and XDG_RUNTIME_DIR.
Set SHELLSTUDIO_NO_UPDATE_CHECK=1 to skip the startup release check.
ShellStudio is GPL-3.0-only; bundled MCP servers retain their MIT licenses.
`
