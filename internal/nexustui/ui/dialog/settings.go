package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	"github.com/EngineerProjects/nexus-engine/internal/nexustui/config"
	"github.com/EngineerProjects/nexus-engine/internal/nexustui/ui/common"
	"github.com/EngineerProjects/nexus-engine/internal/nexustui/ui/list"
	"github.com/EngineerProjects/nexus-engine/internal/nexustui/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
	uv "github.com/charmbracelet/ultraviolet"
)

// SettingsID is the identifier for the settings dialog.
const SettingsID = "settings"

// settingsView tracks which sub-view is active inside the Settings dialog.
type settingsView uint

const (
	settingsViewRoot settingsView = iota
	settingsViewProviders
	settingsViewTheme
	settingsViewWebSearch
	settingsViewTools
	settingsViewMCP
	settingsViewSkills
)

const (
	settingsDialogMaxWidth  = settingsCardMaxWidth
	settingsDialogMaxHeight = settingsCardMaxHeight
)

// settingsSection describes one row in the root hub list.
type settingsSection struct {
	id       string
	name     string
	desc     string
	shortcut string
	dialogID string       // if non-empty: dispatch ActionOpenDialog{dialogID} on select
	subView  settingsView // if dialogID == "": navigate here internally
}

// Settings is the Settings hub dialog opened by ctrl+p. It shows navigable
// sections that route to sub-views or existing dialogs.
type Settings struct {
	com  *common.Common
	view settingsView

	// root view state
	input    textinput.Model
	rootList *list.FilterableList
	sections []settingsSection

	// providers sub-view state
	providers []catwalk.Provider
	provList  *list.FilterableList
	provInput textinput.Model

	// theme sub-view state
	themeList *list.FilterableList

	// info sub-views (Web Search / Tools / MCP / Skills) — rendered as text
	infoLines []string

	keyMap struct {
		Select, Next, Previous, Back, Close key.Binding
	}
	help help.Model

	windowWidth int
}

var _ Dialog = (*Settings)(nil)

// NewSettings creates a new Settings hub dialog.
func NewSettings(com *common.Common) (*Settings, error) {
	t := com.Styles
	s := &Settings{
		com:      com,
		view:     settingsViewRoot,
		sections: defaultSettingsSections(),
	}

	// Root filter input.
	s.input = textinput.New()
	s.input.SetVirtualCursor(false)
	s.input.Placeholder = "Search settings..."
	s.input.SetStyles(t.TextInput)
	s.input.Focus()

	s.rootList = list.NewFilterableList()
	s.rootList.Focus()
	s.rootList.SetSelected(0)
	s.rootList.SetGap(1) // one blank line between each section for visual breathing room
	s.rebuildRootList("")

	// Providers filter input + list.
	s.provInput = textinput.New()
	s.provInput.SetVirtualCursor(false)
	s.provInput.Placeholder = "Filter providers..."
	s.provInput.SetStyles(t.TextInput)

	s.provList = list.NewFilterableList()
	s.provList.Focus()
	s.provList.SetSelected(0)
	s.provList.SetGap(1)

	s.themeList = list.NewFilterableList()
	s.themeList.Focus()
	s.themeList.SetGap(1)

	providers, _ := config.Providers(com.Config()) // best-effort; nil on error → empty list
	s.providers = providers
	s.rebuildProvList("")

	// Key bindings.
	s.keyMap.Select = key.NewBinding(key.WithKeys("enter", "ctrl+y"), key.WithHelp("enter", "open"))
	s.keyMap.Next = key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next"))
	s.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "prev"))
	s.keyMap.Back = key.NewBinding(key.WithKeys("esc", "alt+esc", "left"), key.WithHelp("esc/←", "back"))
	s.keyMap.Close = key.NewBinding(key.WithKeys("esc", "alt+esc"), key.WithHelp("esc", "close"))

	h := help.New()
	h.Styles = t.DialogHelpStyles()
	s.help = h

	return s, nil
}

func defaultSettingsSections() []settingsSection {
	return []settingsSection{
		{id: "commands", name: "Commands", desc: "shortcuts, sessions, copy actions, app controls", dialogID: CommandsID},
		{id: "providers", name: "Providers", desc: "configure API keys and provider credentials", shortcut: "ctrl+,", subView: settingsViewProviders},
		{id: "models", name: "Models", desc: "switch the active AI model", shortcut: "ctrl+m", dialogID: ModelsID},
		{id: "theme", name: "Theme", desc: "background style and visual appearance", subView: settingsViewTheme},
		{id: "web_search", name: "Web Search", desc: "configure web search providers and API keys", subView: settingsViewWebSearch},
		{id: "tools", name: "Tools", desc: "tool UX options and available tool reference", subView: settingsViewTools},
		{id: "mcp", name: "MCP", desc: "MCP server status and management notes", subView: settingsViewMCP},
		{id: "skills", name: "Skills", desc: "slash-skill workflow and skill path discovery", subView: settingsViewSkills},
	}
}

// ID implements Dialog.
func (s *Settings) ID() string { return SettingsID }

// Cursor implements Dialog.
func (s *Settings) Cursor() *tea.Cursor {
	switch s.view {
	case settingsViewRoot:
		return InputCursor(s.com.Styles, s.input.Cursor())
	case settingsViewProviders:
		return InputCursor(s.com.Styles, s.provInput.Cursor())
	}
	return nil
}

// HandleMsg implements Dialog.
func (s *Settings) HandleMsg(msg tea.Msg) Action {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.view == settingsViewRoot {
		return s.handleRootKey(kp)
	}
	return s.handleSubKey(kp)
}

func (s *Settings) handleRootKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, s.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg, s.keyMap.Previous):
		if s.rootList.IsSelectedFirst() {
			s.rootList.SelectLast()
		} else {
			s.rootList.SelectPrev()
		}
		s.rootList.ScrollToSelected()
	case key.Matches(msg, s.keyMap.Next):
		if s.rootList.IsSelectedLast() {
			s.rootList.SelectFirst()
		} else {
			s.rootList.SelectNext()
		}
		s.rootList.ScrollToSelected()
	case key.Matches(msg, s.keyMap.Select):
		return s.activateSelected()
	default:
		// Per-item shortcut triggers (when filter is empty).
		if s.input.Value() == "" {
			for _, fi := range s.rootList.FilteredItems() {
				if si, ok := fi.(*settingsSectionItem); ok && si.shortcut != "" && msg.String() == si.shortcut {
					return s.activateSI(si)
				}
			}
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		s.rebuildRootList(s.input.Value())
		return ActionCmd{cmd}
	}
	return nil
}

func (s *Settings) handleSubKey(msg tea.KeyPressMsg) Action {
	if key.Matches(msg, s.keyMap.Back) {
		s.gotoRoot()
		return nil
	}
	switch s.view {
	case settingsViewProviders:
		return s.handleProvKey(msg)
	case settingsViewTheme:
		return s.handleThemeKey(msg)
	default:
		// Info views: any unhandled key navigates back.
		s.gotoRoot()
		return nil
	}
}

func (s *Settings) handleProvKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, s.keyMap.Previous):
		if s.provList.IsSelectedFirst() {
			s.provList.SelectLast()
		} else {
			s.provList.SelectPrev()
		}
		s.provList.ScrollToSelected()
	case key.Matches(msg, s.keyMap.Next):
		if s.provList.IsSelectedLast() {
			s.provList.SelectFirst()
		} else {
			s.provList.SelectNext()
		}
		s.provList.ScrollToSelected()
	case key.Matches(msg, s.keyMap.Select):
		if item := s.provList.SelectedItem(); item != nil {
			if pi, ok := item.(*settingsProviderItem); ok {
				return ActionOpenProviderConfig{Provider: pi.provider}
			}
		}
	default:
		var cmd tea.Cmd
		s.provInput, cmd = s.provInput.Update(msg)
		s.rebuildProvList(s.provInput.Value())
		return ActionCmd{cmd}
	}
	return nil
}

func (s *Settings) handleThemeKey(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, s.keyMap.Previous):
		if s.themeList.IsSelectedFirst() {
			s.themeList.SelectLast()
		} else {
			s.themeList.SelectPrev()
		}
		s.themeList.ScrollToSelected()
	case key.Matches(msg, s.keyMap.Next):
		if s.themeList.IsSelectedLast() {
			s.themeList.SelectFirst()
		} else {
			s.themeList.SelectNext()
		}
		s.themeList.ScrollToSelected()
	case key.Matches(msg, s.keyMap.Select):
		if item := s.themeList.SelectedItem(); item != nil {
			if ti, ok := item.(*settingsThemeItem); ok {
				cfg := s.com.Config()
				isTransparent := cfg != nil && cfg.Options != nil && cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent
				// Only dispatch if the user chose a DIFFERENT option.
				if ti.transparent != isTransparent {
					return ActionToggleTransparentBackground{}
				}
			}
		}
	}
	return nil
}

func (s *Settings) activateSelected() Action {
	if item := s.rootList.SelectedItem(); item != nil {
		if si, ok := item.(*settingsSectionItem); ok {
			return s.activateSI(si)
		}
	}
	return nil
}

func (s *Settings) activateSI(si *settingsSectionItem) Action {
	if si.dialogID != "" {
		return ActionOpenDialog{DialogID: si.dialogID}
	}
	s.gotoView(si.subView)
	return nil
}

func (s *Settings) gotoView(v settingsView) {
	s.view = v
	switch v {
	case settingsViewProviders:
		s.provInput.SetValue("")
		s.provInput.Focus()
		s.rebuildProvList("")
	case settingsViewTheme:
		s.rebuildThemeList()
	case settingsViewWebSearch:
		s.infoLines = s.infoWebSearch()
	case settingsViewTools:
		s.infoLines = s.infoTools()
	case settingsViewMCP:
		s.infoLines = s.infoMCP()
	case settingsViewSkills:
		s.infoLines = s.infoSkills()
	}
}

func (s *Settings) gotoRoot() {
	s.view = settingsViewRoot
	s.input.Focus()
	s.rebuildRootList(s.input.Value())
}

// ─── List rebuild ──────────────────────────────────────────────────────────

func (s *Settings) rebuildRootList(filter string) {
	items := make([]list.FilterableItem, 0, len(s.sections))
	for _, sec := range s.sections {
		sec := sec
		items = append(items, &settingsSectionItem{
			Versioned: list.NewVersioned(),
			id:        sec.id, name: sec.name, desc: sec.desc,
			shortcut: sec.shortcut, dialogID: sec.dialogID, subView: sec.subView,
			t: s.com.Styles,
		})
	}
	s.rootList.SetItems(items...)
	s.rootList.SetFilter(filter)
	s.rootList.ScrollToTop()
	if filter == "" {
		s.rootList.SetSelected(0)
	}
}

func (s *Settings) rebuildProvList(filter string) {
	cfg := s.com.Config()
	items := make([]list.FilterableItem, 0, len(s.providers))
	for _, p := range s.providers {
		p := p
		configured := false
		if cfg != nil {
			if pc, ok := cfg.Providers.Get(string(p.ID)); ok {
				configured = pc.APIKey != "" || pc.OAuthToken != nil
			}
		}
		items = append(items, &settingsProviderItem{
			Versioned: list.NewVersioned(),
			provider: p, configured: configured,
			t: s.com.Styles,
		})
	}
	s.provList.SetItems(items...)
	s.provList.SetFilter(filter)
	s.provList.ScrollToTop()
	s.provList.SetSelected(0)
}

func (s *Settings) rebuildThemeList() {
	cfg := s.com.Config()
	isTransparent := cfg != nil && cfg.Options != nil &&
		cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent
	items := []list.FilterableItem{
		&settingsThemeItem{Versioned: list.NewVersioned(), transparent: true, com: s.com},
		&settingsThemeItem{Versioned: list.NewVersioned(), transparent: false, com: s.com},
	}
	s.themeList.SetItems(items...)
	s.themeList.ScrollToTop()
	if isTransparent {
		s.themeList.SetSelected(0)
	} else {
		s.themeList.SetSelected(1)
	}
}

// ─── Draw ──────────────────────────────────────────────────────────────────

func (s *Settings) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles
	s.windowWidth = area.Dx()

	// Outer width — capped, respects dialog frame.
	width := max(0, min(settingsDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerW := width - t.Dialog.View.GetHorizontalFrameSize()
	inputW := max(0, innerW-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1)

	// Fixed height budget components.
	titleH     := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight
	inputH     := t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight
	helpH      := t.Dialog.HelpView.GetVerticalFrameSize()
	viewFrameH := t.Dialog.View.GetVerticalFrameSize()
	listMarginH := t.Dialog.List.GetVerticalMargins() // = 1 (bottom margin)

	// Structural constants for the consistent layout every view uses:
	//   sep(1) + subtitle(1) + blank(1) = subBlock
	//   sep(1) above help = sepAbove
	const subBlock = 3
	const sepAbove = 1

	// Thin horizontal separator (header separator style).
	sep := t.Header.Separator.Render(strings.Repeat("─", innerW))

	// ── Dynamic height: measure content, size dialog to fit ──────────────────

	// Phase 1 — measure actual content height for the active view.
	var measuredContentH int
	switch s.view {
	case settingsViewRoot:
		s.rootList.SetSize(innerW, 9999)
		measuredContentH = s.rootList.TotalHeight()
	case settingsViewProviders:
		s.provList.SetSize(innerW, 9999)
		measuredContentH = s.provList.TotalHeight()
	case settingsViewTheme:
		s.themeList.SetSize(innerW, 9999)
		measuredContentH = s.themeList.TotalHeight()
	default: // info views
		measuredContentH = len(s.infoLines)
	}

	// Phase 2 — compute fixed overhead per view type and final dialog height.
	//   search views (root/providers): title+input+subBlock+sepAbove+help+frame+listMargin
	//   theme (no input):              title+subBlock+sepAbove+help+frame+listMargin
	//   info views (no input, no sub): title+sep+sepAbove+help+frame
	var overhead int
	switch s.view {
	case settingsViewRoot, settingsViewProviders:
		overhead = titleH + inputH + subBlock + sepAbove + helpH + viewFrameH + listMarginH
	case settingsViewTheme:
		overhead = titleH + subBlock + sepAbove + helpH + viewFrameH + listMarginH
	default:
		overhead = titleH + 1 + sepAbove + helpH + viewFrameH
	}

	maxTermH := max(0, area.Dy()-t.Dialog.View.GetVerticalBorderSize())
	height := max(overhead+1, min(settingsDialogMaxHeight, min(maxTermH, overhead+measuredContentH)))
	finalContentH := max(1, height-overhead)

	// Phase 3 — set final list sizes.
	switch s.view {
	case settingsViewRoot:
		s.rootList.SetSize(innerW, finalContentH)
	case settingsViewProviders:
		s.provList.SetSize(innerW, finalContentH)
	case settingsViewTheme:
		s.themeList.SetSize(innerW, finalContentH)
	}

	// ── Build render context ──────────────────────────────────────────────────

	rc := NewRenderContext(t, width)
	orange := lipgloss.NewStyle().Bold(true).Foreground(t.Logo.FieldColor)
	rc.Parts = []string{rc.TitleStyle.Render(orange.Render(s.viewTitle()))}

	switch s.view {
	case settingsViewRoot:
		s.input.SetWidth(inputW)
		rc.AddPart(t.Dialog.InputPrompt.Render(s.input.View()))
		rc.AddPart(sep)
		rc.AddPart(t.Dialog.SecondaryText.Render("  choose a section"))
		rc.Parts = append(rc.Parts, "")
		rc.AddPart(t.Dialog.List.Height(s.rootList.Height()).Render(s.rootList.Render()))

	case settingsViewProviders:
		s.provInput.SetWidth(inputW)
		rc.AddPart(t.Dialog.InputPrompt.Render(s.provInput.View()))
		rc.AddPart(sep)
		rc.AddPart(t.Dialog.SecondaryText.Render("  select a provider — enter to configure its API key"))
		rc.Parts = append(rc.Parts, "")
		rc.AddPart(t.Dialog.List.Height(s.provList.Height()).Render(s.provList.Render()))

	case settingsViewTheme:
		// Theme has no search input — sep → subtitle → blank → list.
		rc.AddPart(sep)
		rc.AddPart(t.Dialog.SecondaryText.Render("  choose a background style"))
		rc.Parts = append(rc.Parts, "")
		rc.AddPart(t.Dialog.List.Height(s.themeList.Height()).Render(s.themeList.Render()))

	default:
		rc.AddPart(sep)
		rc.AddPart(lipgloss.NewStyle().Width(innerW).Render(
			strings.Join(s.infoLines, "\n"),
		))
	}

	rc.Parts = append(rc.Parts, sep)
	rc.Help = s.help.View(s)

	view := rc.Render()
	cur := s.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func (s *Settings) viewTitle() string {
	switch s.view {
	case settingsViewProviders:
		return "Settings  ›  Providers"
	case settingsViewTheme:
		return "Settings  ›  Theme"
	case settingsViewWebSearch:
		return "Settings  ›  Web Search"
	case settingsViewTools:
		return "Settings  ›  Tools"
	case settingsViewMCP:
		return "Settings  ›  MCP"
	case settingsViewSkills:
		return "Settings  ›  Skills"
	default:
		return "Settings"
	}
}

// ─── Info builders ─────────────────────────────────────────────────────────

func (s *Settings) infoWebSearch() []string {
	t := s.com.Styles
	accent := lipgloss.NewStyle().Foreground(t.Logo.FieldColor).Bold(true)
	muted := t.Sidebar.WorkingDir
	return []string{
		"",
		accent.Render("  Current provider:") + "  " + muted.Render("DuckDuckGo  (built-in · no API key required)"),
		"",
		muted.Render("  Web search is available to all agents via the web_search tool."),
		muted.Render("  DuckDuckGo is used by default and requires no configuration."),
		"",
		accent.Render("  Coming soon:"),
		muted.Render("    Brave Search, Tavily, and SerpAPI — configure a premium provider"),
		muted.Render("    for better results and higher rate limits."),
		"",
		muted.Render("  Press esc or ← to go back."),
	}
}

func (s *Settings) infoTools() []string {
	t := s.com.Styles
	accent := lipgloss.NewStyle().Foreground(t.Logo.FieldColor).Bold(true)
	muted := t.Sidebar.WorkingDir
	return []string{
		"",
		accent.Render("  Inline tool previews:"),
		muted.Render("    Expand tool calls in chat with space or click the expander."),
		"",
		accent.Render("  Tool details pane:"),
		muted.Render("    Open the right-side details pane with ctrl+d."),
		"",
		accent.Render("  Disable specific tools:"),
		muted.Render("    Set options.disabled_tools in crush.json to hide tools from the agent."),
		"",
		muted.Render("  Press esc or ← to go back."),
	}
}

func (s *Settings) infoMCP() []string {
	t := s.com.Styles
	accent := lipgloss.NewStyle().Foreground(t.Logo.FieldColor).Bold(true)
	muted := t.Sidebar.WorkingDir
	cfg := s.com.Config()
	mcpCount := 0
	if cfg != nil {
		mcpCount = len(cfg.MCP)
	}
	return []string{
		"",
		accent.Render(fmt.Sprintf("  Active MCP servers: %d", mcpCount)),
		muted.Render("    Configured servers are available to the agent during execution."),
		"",
		accent.Render("  Configuration:"),
		muted.Render("    Edit the mcp section in crush.json / CRUSH.md to add servers."),
		"",
		accent.Render("  Docker MCP:"),
		muted.Render("    Use Commands → Enable Docker MCP Catalog to add Docker-hosted tools."),
		"",
		muted.Render("  Press esc or ← to go back."),
	}
}

func (s *Settings) infoSkills() []string {
	t := s.com.Styles
	accent := lipgloss.NewStyle().Foreground(t.Logo.FieldColor).Bold(true)
	muted := t.Sidebar.WorkingDir
	cfg := s.com.Config()
	var paths []string
	if cfg != nil && cfg.Options != nil {
		paths = cfg.Options.SkillsPaths
	}
	lines := []string{
		"",
		accent.Render("  Run a skill:"),
		muted.Render("    Type /skill_name directly in chat to invoke a skill."),
		"",
		accent.Render("  Custom skill paths:"),
	}
	if len(paths) == 0 {
		lines = append(lines, muted.Render("    (none configured)"))
	} else {
		for _, p := range paths {
			lines = append(lines, muted.Render("    • "+p))
		}
	}
	lines = append(lines,
		"",
		muted.Render("    Add paths via options.skills_paths in crush.json."),
		"",
		muted.Render("  Press esc or ← to go back."),
	)
	return lines
}

// ─── help.KeyMap ──────────────────────────────────────────────────────────

func (s *Settings) ShortHelp() []key.Binding {
	if s.view == settingsViewRoot {
		return []key.Binding{s.keyMap.Next, s.keyMap.Select, s.keyMap.Close}
	}
	return []key.Binding{s.keyMap.Next, s.keyMap.Select, s.keyMap.Back}
}

func (s *Settings) FullHelp() [][]key.Binding {
	if s.view == settingsViewRoot {
		return [][]key.Binding{{s.keyMap.Next, s.keyMap.Previous, s.keyMap.Select}, {s.keyMap.Close}}
	}
	return [][]key.Binding{{s.keyMap.Next, s.keyMap.Previous, s.keyMap.Select}, {s.keyMap.Back}}
}

// ─── settingsSectionItem ───────────────────────────────────────────────────

type settingsSectionItem struct {
	*list.Versioned
	id, name, desc, shortcut, dialogID string
	subView                            settingsView
	focused                            bool
	match                              fuzzy.Match
	t                                  *styles.Styles
}

func (i *settingsSectionItem) Filter() string { return i.name + " " + i.desc }
func (i *settingsSectionItem) ID() string     { return i.id }
func (i *settingsSectionItem) Finished() bool { return true }

func (i *settingsSectionItem) SetFocused(f bool) {
	if i.focused == f {
		return
	}
	i.focused = f
	i.Bump()
}

func (i *settingsSectionItem) SetMatch(m fuzzy.Match) {
	i.match = m
	i.Bump()
}

// Render builds a full-width row: bold name on the left, muted description
// and shortcut on the right. Inline ANSI attribute codes (not full resets)
// are used for bold and grey-fg so that the outer style's orange background
// is never interrupted — this is what produces the continuous highlight bar.
//
// Items are indented by 3 spaces so they nest visually under the "choose a
// section" subtitle (which sits at a 2-space indent), matching the layout
// from the original command palette design.
func (i *settingsSectionItem) Render(width int) string {
	t := i.t
	style := t.Dialog.NormalItem
	scStyle := t.Dialog.ListItem.InfoBlurred
	if i.focused {
		// Soft selection: dark warm-orange background, normal text — readable, not harsh.
		style = t.Dialog.SelectedItem.
			Background(lipgloss.Color(settingsCardSelectedBg)).
			Foreground(t.Dialog.NormalItem.GetForeground())
		scStyle = t.Dialog.ListItem.InfoFocused
	}
	// Width must be set explicitly so lipgloss fills the full row with background color.
	style = style.Width(width)

	// Available inner width (style has Padding(0,1) = 2 chars total).
	hpad := style.GetHorizontalPadding()
	lineWidth := width - hpad

	// 4-space indent prefix: combined with the 1-char left padding from the
	// style, items sit at col 5 from the dialog edge — visually nested under
	// the "choose a section" subtitle at col 3.
	const prefix = "    "
	const prefixW = 4

	// Right: shortcut in info style (pre-rendered, placed at far-right edge).
	var infoText string
	infoWidth := 0
	if i.shortcut != "" {
		infoText = scStyle.Render(" " + i.shortcut + " ")
		infoWidth = lipgloss.Width(infoText)
	}

	// Name: bold via attribute-only codes so the outer background is unbroken.
	// ansi.Style{}.Bold().String()   = "\x1b[1m"  (bold on)
	// ansi.Style{}.Normal().String() = "\x1b[22m" (intensity reset, NOT a full reset)
	boldOn := ansi.Style{}.Bold().String()
	boldOff := ansi.Style{}.Normal().String()
	nameStr := boldOn + i.name + boldOff
	nameWidth := lipgloss.Width(i.name) // visual width ignores escape codes

	// Description: grey foreground via attribute-only codes.
	// ansi.Style{}.ForegroundColor(c).String()      = "\x1b[38;2;r;g;bm"  (set fg)
	// ansi.Style{}.DefaultForegroundColor().String() = "\x1b[39m"          (reset fg only)
	var descStr string
	descWidth := 0
	if i.desc != "" {
		greyColor := t.Sidebar.WorkingDir.GetForeground()
		greyOn := ansi.Style{}.ForegroundColor(greyColor).String()
		greyOff := ansi.Style{}.DefaultForegroundColor().String()
		const sep = "  "
		maxDesc := lineWidth - prefixW - nameWidth - len(sep) - infoWidth - 1
		if maxDesc > 2 {
			desc := ansi.Truncate(i.desc, maxDesc, "…")
			descStr = sep + greyOn + desc + greyOff
			descWidth = len(sep) + lipgloss.Width(desc)
		}
	}

	// Gap fills remaining space; comes between left content and right info.
	gap := strings.Repeat(" ", max(0, lineWidth-prefixW-nameWidth-descWidth-infoWidth))

	// Single Render call: outer style applies bg/fg uniformly; inner ANSI codes
	// only toggle attributes without full resets, so the orange bg is unbroken.
	return style.Render(prefix + nameStr + descStr + gap + infoText)
}

// ─── settingsProviderItem ──────────────────────────────────────────────────

type settingsProviderItem struct {
	*list.Versioned
	provider   catwalk.Provider
	configured bool
	focused    bool
	match      fuzzy.Match
	t          *styles.Styles
}

func (i *settingsProviderItem) Filter() string { return i.provider.Name + " " + string(i.provider.ID) }
func (i *settingsProviderItem) ID() string     { return string(i.provider.ID) }
func (i *settingsProviderItem) Finished() bool { return false }

func (i *settingsProviderItem) SetFocused(f bool) {
	if i.focused == f {
		return
	}
	i.focused = f
	i.Bump()
}

func (i *settingsProviderItem) SetMatch(m fuzzy.Match) {
	i.match = m
	i.Bump()
}

func (i *settingsProviderItem) Render(width int) string {
	t := i.t
	style := t.Dialog.NormalItem
	if i.focused {
		style = t.Dialog.SelectedItem.
			Background(lipgloss.Color(settingsCardSelectedBg)).
			Foreground(t.Dialog.NormalItem.GetForeground())
	}
	style = style.Width(width)

	hpad := style.GetHorizontalPadding()
	lineWidth := width - hpad

	const prefix = "    "
	const prefixW = 4

	// Status badge at far right.
	var statusStyle lipgloss.Style
	var statusText string
	if i.configured {
		statusStyle = lipgloss.NewStyle().Foreground(t.ToolCallSuccess.GetForeground())
		statusText = "✓ configured"
	} else {
		statusStyle = lipgloss.NewStyle().Foreground(t.Tool.IconError.GetForeground())
		statusText = "✗ not configured"
	}
	infoText := statusStyle.Render(" "+statusText) + "  "
	infoWidth := lipgloss.Width(infoText)

	// Provider name: bold via attribute-only codes.
	boldOn := ansi.Style{}.Bold().String()
	boldOff := ansi.Style{}.Normal().String()
	nameStr := boldOn + i.provider.Name + boldOff
	nameWidth := lipgloss.Width(i.provider.Name)

	gap := strings.Repeat(" ", max(0, lineWidth-prefixW-nameWidth-infoWidth))
	return style.Render(prefix + nameStr + gap + infoText)
}

// ─── settingsThemeItem ─────────────────────────────────────────────────────

// settingsThemeItem represents one background style option in the Theme sub-view.
// It reads the live config on every Render so the active indicator (●/○) always
// reflects the current setting without requiring an explicit list rebuild.
type settingsThemeItem struct {
	*list.Versioned
	// transparent=true → "Terminal background" option; false → "Solid background".
	transparent bool
	com         *common.Common
	focused     bool
	match       fuzzy.Match
}

func (i *settingsThemeItem) Filter() string {
	if i.transparent {
		return "terminal background transparent theme"
	}
	return "solid background dark theme"
}
func (i *settingsThemeItem) ID() string {
	if i.transparent { return "theme_transparent" }
	return "theme_solid"
}
// Finished returns false so the list always calls Render and picks up config changes.
func (i *settingsThemeItem) Finished() bool { return false }

func (i *settingsThemeItem) SetFocused(f bool) {
	if i.focused == f {
		return
	}
	i.focused = f
	i.Bump()
}

func (i *settingsThemeItem) SetMatch(m fuzzy.Match) {
	i.match = m
	i.Bump()
}

func (i *settingsThemeItem) Render(width int) string {
	t := i.com.Styles
	style := t.Dialog.NormalItem
	if i.focused {
		style = t.Dialog.SelectedItem.
			Background(lipgloss.Color(settingsCardSelectedBg)).
			Foreground(t.Dialog.NormalItem.GetForeground())
	}
	style = style.Width(width)

	// Determine whether this option is currently active.
	cfg := i.com.Config()
	isTransparent := cfg != nil && cfg.Options != nil &&
		cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent
	isActive := i.transparent == isTransparent

	hpad := style.GetHorizontalPadding()
	lineWidth := width - hpad

	const prefix = "    "
	const prefixW = 4

	// Name and description.
	var name, desc string
	if i.transparent {
		name = "Terminal background"
		desc = "use your terminal's own color scheme (default)"
	} else {
		name = "Solid background"
		desc = "use nexus built-in dark color scheme"
	}

	boldOn := ansi.Style{}.Bold().String()
	boldOff := ansi.Style{}.Normal().String()
	nameStr := boldOn + name + boldOff
	nameWidth := lipgloss.Width(name)

	// Active indicator at far right: ● green when active, nothing when inactive.
	// Placed at the right edge like the provider status badge so it never
	// interrupts the orange background fill.
	var infoText string
	infoWidth := 0
	if isActive {
		checkStyle := lipgloss.NewStyle().Foreground(t.ToolCallSuccess.GetForeground())
		infoText = checkStyle.Render("●") + "  "
		infoWidth = 3
	}

	// Description in grey using fg-only ANSI codes (preserves outer background).
	greyColor := t.Sidebar.WorkingDir.GetForeground()
	greyOn := ansi.Style{}.ForegroundColor(greyColor).String()
	greyOff := ansi.Style{}.DefaultForegroundColor().String()
	const sep = "  "
	maxDesc := lineWidth - prefixW - nameWidth - len(sep) - infoWidth - 1
	var descStr string
	descWidth := 0
	if maxDesc > 2 {
		desc = ansi.Truncate(desc, maxDesc, "…")
		descStr = sep + greyOn + desc + greyOff
		descWidth = len(sep) + lipgloss.Width(desc)
	}

	gap := strings.Repeat(" ", max(0, lineWidth-prefixW-nameWidth-descWidth-infoWidth))
	return style.Render(prefix + nameStr + descStr + gap + infoText)
}
