// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"shellstudio/internal/i18n"
	"shellstudio/internal/mux"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
	"shellstudio/internal/theme"
)

// Minimize hides a console from a view without ending its program: the
// membership stays, with its position, and the Minimized panel lists it.
// Closing a panel (RemoveFromView) is different: it unlinks the console.
func (a *App) Minimize(view, id string) error {
	if e := a.Store.SetMinimized(view, id, true); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New(i18n.T("err.not_member"))
		}
		return e
	}
	a.Store.Event("console-minimize", id)
	a.bumpMinimized()
	return a.RefreshOpen(view)
}

// Restore shows a minimized console again in its original place.
func (a *App) Restore(view, id string) error {
	if e := a.Store.SetMinimized(view, id, false); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New(i18n.T("err.not_member"))
		}
		return e
	}
	a.Store.Event("console-restore", id)
	a.bumpMinimized()
	return a.RefreshOpen(view)
}

// RestoreIn shows a minimized console of one view in another view: it is
// linked there (or restored if already minimized there) and left minimized in
// the first one.
func (a *App) RestoreIn(view, id string) error {
	members, e := a.Store.Members(view)
	if e != nil {
		return e
	}
	for _, c := range members {
		if c.ID == id {
			if c.Minimized {
				return a.Restore(view, id)
			}
			return nil
		}
	}
	return a.AddToView(view, id)
}

// MinimizedRev changes whenever a console is minimized or restored, so the
// Minimized panels redraw without waiting for their next poll.
func (a *App) MinimizedRev() string {
	o, _ := a.Mux.Run(true, "show-option", "-gqv", "@ss_minimized_rev")
	return strings.TrimSpace(o)
}

// NotifyMinimized makes the Minimized panels reload now.
func (a *App) NotifyMinimized() { a.bumpMinimized() }

// KindLabelsCached is KindLabels, reused for a few seconds.
func (a *App) KindLabelsCached() map[string]string { return a.labelCache() }

func (a *App) bumpMinimized() {
	a.Mux.Run(true, "set-option", "-g", "@ss_minimized_rev", strconv.FormatInt(time.Now().UnixNano(), 36))
}

// Maximize toggles the zoom of a views-server pane.
func (a *App) Maximize(pane string) error {
	_, e := a.Mux.Run(true, "resize-pane", "-Z", "-t", pane)
	return e
}

// SyncZoom marks the console zoomed in the window of a views pane, and
// unmarks the other consoles of that view, so their headers show ❐ or □.
func (a *App) SyncZoom(pane string) error {
	o, e := a.Mux.Run(true, "list-panes", "-t", pane, "-F", "#{window_zoomed_flag}\t#{pane_active}\t#{@ss_kind}\t#{@ss_console}")
	if e != nil {
		return e
	}
	var cmds [][]string
	var consoles []string
	for _, line := range strings.Split(strings.TrimSpace(o), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[2] != "console" || f[3] == "" {
			continue
		}
		target := "=c-" + f[3] + ":"
		if f[0] == "1" && f[1] == "1" {
			cmds = append(cmds, []string{"set-option", "-t", target, "@ss_zoomed", "1"})
		} else {
			cmds = append(cmds, []string{"set-option", "-u", "-t", target, "@ss_zoomed"})
		}
		consoles = append(consoles, f[3])
	}
	if len(cmds) == 0 {
		return nil
	}
	// A console without a running program has no session: skip errors.
	for _, c := range cmds {
		a.Mux.Run(false, c...)
	}
	for _, id := range consoles {
		if c, e := a.Store.Console(id); e == nil {
			a.Mux.SetHeader(c)
		}
	}
	return nil
}

// ValidTagName keeps tag names short enough for a badge and safe in formats.
func ValidTagName(name string) error {
	if name == "" || len([]rune(name)) > 24 {
		return errors.New(i18n.T("tags.invalid_name"))
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '#' {
			return errors.New(i18n.T("tags.invalid_name"))
		}
	}
	return nil
}

// ValidDescription accepts up to 120 characters without control characters.
func ValidDescription(text string) error {
	if len([]rune(text)) > 120 {
		return errors.New(i18n.T("describe.invalid"))
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return errors.New(i18n.T("describe.invalid"))
		}
	}
	return nil
}

func validTagColor(c int) error {
	for _, p := range theme.TagPalette {
		if p == c {
			return nil
		}
	}
	return errors.New(i18n.T("tags.invalid_color"))
}

func (a *App) Tags() ([]store.Tag, error) { return a.Store.Tags() }

func (a *App) checkTagName(name, except string) error {
	if e := ValidTagName(name); e != nil {
		return e
	}
	tags, e := a.Store.Tags()
	if e != nil {
		return e
	}
	for _, t := range tags {
		if t.ID != except && strings.EqualFold(t.Name, name) {
			return errors.New(i18n.T("tags.duplicate", name))
		}
	}
	return nil
}

// AddTag creates a tag at the end of the list.
func (a *App) AddTag(name string, color int) (store.Tag, error) {
	name = strings.TrimSpace(name)
	if e := a.checkTagName(name, ""); e != nil {
		return store.Tag{}, e
	}
	if e := validTagColor(color); e != nil {
		return store.Tag{}, e
	}
	return a.Store.AddTag(name, color)
}

// UpdateTag renames or recolors a tag and redraws the consoles that use it.
func (a *App) UpdateTag(t store.Tag) error {
	t.Name = strings.TrimSpace(t.Name)
	if e := a.checkTagName(t.Name, t.ID); e != nil {
		return e
	}
	if e := validTagColor(t.Color); e != nil {
		return e
	}
	if e := a.Store.UpdateTag(t); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New(i18n.T("err.tag_gone"))
		}
		return e
	}
	return a.refreshTagged(t.ID)
}

// DeleteTag removes a tag; its consoles are left untagged.
func (a *App) DeleteTag(id string) error {
	users, e := a.tagged(id)
	if e != nil {
		return e
	}
	if e = a.Store.DeleteTag(id); e != nil {
		return e
	}
	for _, c := range users {
		c.TagID = ""
		a.Mux.SetHeader(c)
	}
	a.bumpMinimized()
	return a.BindTagKeys()
}

// MoveTag shifts a tag one place up (-1) or down (+1).
func (a *App) MoveTag(id string, delta int) error {
	tags, e := a.Store.Tags()
	if e != nil {
		return e
	}
	ids := make([]string, len(tags))
	index := -1
	for i, t := range tags {
		ids[i] = t.ID
		if t.ID == id {
			index = i
		}
	}
	j := index + delta
	if index < 0 || j < 0 || j >= len(ids) {
		return nil
	}
	ids[index], ids[j] = ids[j], ids[index]
	return a.Store.ReorderTags(ids)
}

// TagUses counts the consoles with a tag.
func (a *App) TagUses(id string) (int, error) { return a.Store.TagUses(id) }

func (a *App) tagged(tag string) ([]store.Console, error) {
	cs, e := a.Store.Consoles("")
	if e != nil {
		return nil, e
	}
	var out []store.Console
	for _, c := range cs {
		if c.TagID == tag {
			out = append(out, c)
		}
	}
	return out, nil
}

func (a *App) refreshTagged(tag string) error {
	cs, e := a.tagged(tag)
	if e != nil {
		return e
	}
	for _, c := range cs {
		a.Mux.SetHeader(c)
	}
	a.bumpMinimized()
	return nil
}

// SetTag tags a console (an empty tag removes it) and updates its header.
func (a *App) SetTag(id, tag string) error {
	if tag != "" {
		if _, e := a.Store.Tag(tag); e != nil {
			return errors.New(i18n.T("err.tag_gone"))
		}
	}
	if e := a.Store.SetConsoleTag(id, tag); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New(i18n.T("err.console_gone"))
		}
		return e
	}
	c, e := a.Store.Console(id)
	if e != nil {
		return e
	}
	a.bumpMinimized()
	return a.Mux.SetHeader(c)
}

// SetDescription stores the text shown under a console's name.
func (a *App) SetDescription(id, text string) error {
	text = strings.TrimSpace(text)
	if e := ValidDescription(text); e != nil {
		return e
	}
	if e := a.Store.SetDescription(id, text); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return errors.New(i18n.T("err.console_gone"))
		}
		return e
	}
	a.bumpMinimized()
	return nil
}

// KindLabels maps kind IDs to labels: the available kinds, the built-in
// names and, for an extension that is no longer installed, its ID.
func (a *App) KindLabels() map[string]string {
	out := map[string]string{}
	for id, b := range builtinKinds {
		out[id] = b[0]
	}
	for _, k := range a.Kinds() {
		out[k.ID] = k.Label
	}
	return out
}

// KindLabel is the label of one kind; see KindLabels.
func (a *App) KindLabel(kind string) string {
	if l, ok := a.KindLabels()[kind]; ok {
		return l
	}
	return kind
}

// ConsoleLabel is "Kind · Name", the title of a console everywhere.
func ConsoleLabel(labels map[string]string, c store.Console) string {
	kind := labels[c.Extension]
	if kind == "" {
		kind = c.Extension
	}
	return kind + " · " + c.Name
}

// headerOptions feeds Mux.HeaderOptions: label and tag of a console header.
func (a *App) headerOptions(c store.Console) map[string]string {
	labels := a.labelCache()
	opts := map[string]string{"@ss_label": ConsoleLabel(labels, c), "@ss_tag": "", "@ss_tag_color": "", "@ss_tag_fg": ""}
	if c.TagID != "" {
		if t, e := a.Store.Tag(c.TagID); e == nil {
			opts["@ss_tag"] = t.Name
			opts["@ss_tag_color"] = theme.Colour(theme.TagColor(t.Color))
			opts["@ss_tag_fg"] = theme.Colour(theme.TagText(theme.TagColor(t.Color)))
		}
	}
	return opts
}

// labelCache keeps kind labels for a few seconds: listing kinds probes PATH
// and reconfiguring every header would repeat that per console.
func (a *App) labelCache() map[string]string {
	if a.labels == nil || time.Since(a.labelsAt) > 5*time.Second {
		a.labels, a.labelsAt = a.KindLabels(), time.Now()
	}
	return a.labels
}

// TabsFormat is the top bar's list of view tabs, sorted as F7 shows them.
func (a *App) TabsFormat() string {
	vs, e := a.SortedViews()
	if e != nil {
		return ""
	}
	return mux.TabsFormat(vs, 0)
}

// PublishTabs stores the tabs and view count in the views server so the top
// bar follows created, renamed and deleted views.
func (a *App) PublishTabs() {
	vs, e := a.SortedViews()
	if e != nil {
		return
	}
	a.Mux.Batch(true,
		[]string{"set-option", "-g", "@ss_views", fmt.Sprint(len(vs))},
		[]string{"set-option", "-g", "@ss_tabs", mux.TabsFormat(vs, 0)},
		[]string{"set-option", "-g", "@ss_tabs_short", mux.TabsFormat(vs, 10)},
		// status-interval is 0: redraw the top bar of every client now.
		[]string{"run-shell", "-C", "#{L:refresh-client -S -t #{client_name} ; }"})
}

// ViewByTab resolves the view of a tab range.
func (a *App) ViewByTab(r string) (store.View, error) {
	prefix := mux.TabView(r)
	vs, e := a.Store.Views()
	if e != nil {
		return store.View{}, e
	}
	for _, v := range vs {
		if prefix != "" && strings.HasPrefix(v.ID, prefix) {
			return v, nil
		}
	}
	return store.View{}, errors.New(i18n.T("err.view_gone"))
}

// SetLanguage saves the language (auto, es or en) and reapplies the tmux
// formats that show translated text.
func (a *App) SetLanguage(l string) error {
	if l != "es" && l != "en" {
		l = "auto"
	}
	if e := platform.UpdateConfig(a.Paths.Config, func(c *platform.Config) error {
		c.Language = l
		return nil
	}); e != nil {
		return e
	}
	i18n.SetLang(i18n.Resolve(l))
	a.Mux.Run(true, "set-option", "-g", "@ss_lang", i18n.Lang())
	a.Mux.Version = a.configVersion()
	if e := a.ConfigureViews(); e != nil {
		return e
	}
	return a.ConfigurePrograms(true)
}

// adoptServerLanguage keeps one language per running workspace. With
// language auto, each process would resolve it from its own environment (an
// SSH shell with LANG=C, run-shell children of tmux) and the formats would
// flip between languages on every reconfiguration. The views server
// publishes the language it was configured with in @ss_lang.
func (a *App) adoptServerLanguage() {
	if a.Language() != "auto" {
		return
	}
	o, e := a.Mux.Run(true, "show-option", "-gqv", "@ss_lang")
	if l := strings.TrimSpace(o); e == nil && (l == "es" || l == "en") {
		i18n.SetLang(l)
	}
}

// Language is the configured language: auto, es or en.
func (a *App) Language() string {
	c, _ := platform.LoadConfig(a.Paths.Config)
	if c.Language == "es" || c.Language == "en" {
		return c.Language
	}
	return "auto"
}
