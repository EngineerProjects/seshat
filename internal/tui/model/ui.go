// Package model implements the BubbleTea TUI for nexus-engine, adapted from
// Charm's crush project architecture (BubbleTea state machine, workspace
// abstraction, draw cache, permission dialog, session browser).
package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EngineerProjects/nexus-engine/internal/tui"
)

// uiState is the coarse state machine for the TUI.
type uiState uint8

const (
	stateWelcome  uiState = iota // before any session is active
	stateChat                    // active session, chat visible
	stateSessions                // session browser overlay
	statePermission              // permission dialog overlay (on top of chat)
)

const (
	headerHeight = 1
	footerHeight = 1
	inputMinH    = 3
	inputMaxH    = 7
	inputPadding = 2 // border top + bottom
)

// Model is the top-level BubbleTea model for nexus-engine's TUI.
type Model struct {
	workspace tui.Workspace
	ctx       context.Context
	cancel    context.CancelFunc

	state   uiState
	keys    KeyMap
	styles  Styles

	width  int
	height int

	// Components
	chat       *chat
	sessions   *sessionList
	permission *permissionDialog
	input      textarea.Model
	spinner    spinner.Model

	// Agent state
	busy          bool
	activeSession string
	lastErr       error

	// Accumulated text input for permission dialogs (text/choice type)
	permInput string
}

// New creates a new TUI model.
func New(ws tui.Workspace, ctx context.Context) Model {
	ctx, cancel := context.WithCancel(ctx)

	styles := DefaultStyles()
	keys := DefaultKeys()

	// Textarea
	ta := textarea.New()
	ta.Placeholder = "Type a message… (enter to send, shift+enter for newline)"
	ta.Focus()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(inputMinH)

	// Spinner
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorYellow)

	return Model{
		workspace:  ws,
		ctx:        ctx,
		cancel:     cancel,
		state:      stateWelcome,
		keys:       keys,
		styles:     styles,
		chat:       newChat(styles, 80, 20),
		sessions:   newSessionList(styles),
		permission: newPermissionDialog(styles),
		input:      ta,
		spinner:    sp,
	}
}

// ─── BubbleTea interface ──────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
		m.loadSessions(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	// ── Window resize ──────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.relayout()

	// ── Spinner tick ───────────────────────────────────────────────────────
	case spinner.TickMsg:
		if m.busy {
			newSp, cmd := m.spinner.Update(msg)
			m.spinner = newSp
			cmds = append(cmds, cmd)
		}

	// ── Workspace events ───────────────────────────────────────────────────

	case tui.ChunkMsg:
		if m.state == stateChat || m.state == statePermission {
			m.chat.AppendChunk(msg.Text, msg.IsThinking)
		}

	case tui.ToolProgressMsg:
		label := msg.Label
		if label == "" {
			label = msg.Status
		}
		m.chat.AddToolProgress(msg.ToolName, msg.Status, label)

	case tui.TurnStartMsg:
		m.busy = true
		m.chat.StartAssistantMessage()
		cmds = append(cmds, m.spinner.Tick)

	case tui.TurnDoneMsg:
		m.busy = false
		m.chat.FinishAssistantMessage()
		if msg.Err != nil {
			m.chat.AddError(msg.Err)
		}

	case tui.PromptRequestMsg:
		m.permission.SetPending(&msg)
		m.permInput = ""
		m.state = statePermission

	case tui.SessionListMsg:
		if msg.Err == nil {
			m.sessions.SetSessions(msg.Sessions)
		}

	case tui.SessionCreatedMsg:
		if msg.Err != nil {
			m.lastErr = msg.Err
		} else {
			m.activeSession = msg.ID
			m.state = stateChat
			m.chat.Clear()
			m.chat.AddSystem("New session · " + shortID(msg.ID))
			m.input.Focus()
		}

	case tui.SessionLoadedMsg:
		if msg.Err != nil {
			m.lastErr = msg.Err
		} else {
			m.activeSession = msg.ID
			m.state = stateChat
			m.chat.Clear()
			m.chat.AddSystem("Resumed session · " + shortID(msg.ID))
			m.input.Focus()
		}
		if m.state != statePermission {
			// close session browser after loading
		}

	case tui.ErrMsg:
		m.lastErr = msg.Err

	// ── Keyboard ───────────────────────────────────────────────────────────
	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Propagate to textarea when in chat state.
	if m.state == stateChat {
		newInput, cmd := m.input.Update(msg)
		m.input = newInput
		cmds = append(cmds, cmd)
		// Resize input based on content.
		m = m.resizeInput()
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	switch m.state {
	case stateWelcome:
		return m.viewWelcome()
	case stateSessions:
		return m.viewSessions()
	case stateChat, statePermission:
		return m.viewChat()
	default:
		return m.viewChat()
	}
}

// ─── Key handling ─────────────────────────────────────────────────────────────

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// ── Permission dialog ────────────────────────────────────────────────
	if m.state == statePermission && m.permission.HasPending() {
		switch {
		case k == "y" || k == "Y":
			m.permission.Resolve(true, false)
			m.state = stateChat
		case k == "n" || k == "N" || k == "esc":
			m.permission.Resolve(false, true)
			m.state = stateChat
		case k == "a" || k == "A":
			m.permission.Resolve("always", false)
			m.state = stateChat
		default:
			m.permInput += k
		}
		return nil
	}

	// ── Session browser ──────────────────────────────────────────────────
	if m.state == stateSessions {
		switch {
		case k == "esc" || k == "ctrl+s":
			m.state = m.prevChatState()
		case k == "up" || k == "k":
			m.sessions.Up()
		case k == "down" || k == "j":
			m.sessions.Down()
		case k == "enter":
			id := m.sessions.Selected()
			if id != "" {
				m.state = stateChat
				return m.loadSession(id)
			}
		case k == "d" || k == "delete":
			id := m.sessions.DeleteSelected()
			if id != "" {
				return m.deleteSession(id)
			}
		case k == "backspace":
			m.sessions.DeleteFilter()
		default:
			if len(k) == 1 {
				m.sessions.TypeFilter(k)
			}
		}
		return nil
	}

	// ── Global shortcuts (all states) ────────────────────────────────────
	switch k {
	case "ctrl+c":
		if m.busy {
			m.workspace.Cancel()
			return nil
		}
		m.cancel()
		return tea.Quit
	case "ctrl+q":
		m.cancel()
		return tea.Quit
	case "ctrl+s":
		if m.state == stateChat || m.state == stateWelcome {
			m.state = stateSessions
			return m.loadSessions()
		}
	case "ctrl+n":
		return m.createSession()
	}

	// ── Chat / welcome state ─────────────────────────────────────────────
	if m.state == stateChat || m.state == stateWelcome {
		switch k {
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" || m.busy {
				return nil
			}
			m.input.Reset()
			m.chat.AddUserMessage(text)
			m.workspace.Submit(m.ctx, text)
			return nil

		case "shift+enter", "alt+enter":
			// Let the textarea handle newline insertion.
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return cmd

		case "up":
			// If input is empty and at top, scroll chat.
			if m.input.Value() == "" {
				m.chat.ScrollUp(3)
				return nil
			}
		case "down":
			if m.input.Value() == "" {
				m.chat.ScrollDown(3)
				return nil
			}
		case "pgup":
			m.chat.PageUp()
		case "pgdown":
			m.chat.PageDown()
		case "home":
			m.chat.GotoTop()
		case "end":
			m.chat.GotoBottom()
		}
	}

	return nil
}

// ─── Views ────────────────────────────────────────────────────────────────────

func (m Model) viewWelcome() string {
	logo := m.styles.Logo.Render("◉ NEXUS")

	tagline := m.styles.HeaderModel.Render("One runtime. Any LLM. Any language.")

	hint := strings.Join([]string{
		m.styles.Key.Render("ctrl+n") + " " + m.styles.Desc.Render("new session"),
		m.styles.Key.Render("ctrl+s") + " " + m.styles.Desc.Render("sessions"),
		m.styles.Key.Render("ctrl+q") + " " + m.styles.Desc.Render("quit"),
	}, "  ")

	body := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height-2).
		Align(lipgloss.Center, lipgloss.Center).
		Render(logo + "\n\n" + tagline + "\n\n" + hint)

	return m.header() + "\n" + body
}

func (m Model) viewChat() string {
	header := m.header()
	footer := m.footer()
	inputView := m.inputView()

	chatH := m.height - headerHeight - footerHeight - lipgloss.Height(inputView)
	m.chat.SetSize(m.width, max(1, chatH))
	chatView := m.chat.View()

	base := strings.Join([]string{
		header,
		chatView,
		inputView,
		footer,
	}, "\n")

	// Overlay the permission dialog if active.
	if m.state == statePermission && m.permission.HasPending() {
		overlay := m.permission.View()
		return overlayOn(base, overlay, m.width, m.height)
	}

	return base
}

func (m Model) viewSessions() string {
	m.sessions.SetSize(m.width, m.height)
	overlay := m.sessions.centred()

	// Render the underlying chat/welcome as the backdrop.
	var backdrop string
	if m.activeSession != "" {
		backdrop = m.viewChat()
	} else {
		backdrop = m.viewWelcome()
	}

	return overlayOn(backdrop, overlay, m.width, m.height)
}

func (m Model) header() string {
	logo := m.styles.Logo.Render("◉ NEXUS")
	sep := m.styles.HeaderSep.Render("  │  ")
	model := m.styles.HeaderModel.Render(m.workspace.ModelString())

	var status string
	if m.busy {
		status = m.spinner.View() + " " + m.styles.HeaderBusy.Render("working")
	} else if m.activeSession != "" {
		status = m.styles.HeaderReady.Render("●") + " " + m.styles.HeaderID.Render(shortID(m.activeSession))
	} else {
		status = m.styles.HeaderReady.Render("ready")
	}

	right := status
	left := logo + sep + model

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m Model) footer() string {
	items := []string{
		m.styles.Key.Render("ctrl+n") + " " + m.styles.Desc.Render("new"),
		m.styles.Key.Render("ctrl+s") + " " + m.styles.Desc.Render("sessions"),
		m.styles.Key.Render("ctrl+c") + " " + m.styles.Desc.Render("cancel/quit"),
	}
	line := strings.Join(items, "  ")
	return m.styles.Footer.Render(line)
}

func (m Model) inputView() string {
	return m.styles.InputBorder.Width(m.width - 2).Render(m.input.View())
}

// ─── Layout helpers ───────────────────────────────────────────────────────────

func (m Model) relayout() Model {
	inputW := m.width - 4 // 2 border + 2 padding
	if inputW < 10 {
		inputW = 10
	}
	m.input.SetWidth(inputW)
	m.sessions.SetSize(m.width, m.height)
	m.permission.SetSize(m.width, m.height)
	m.chat.SetSize(m.width, max(1, m.height-headerHeight-footerHeight-inputMinH-inputPadding))
	return m
}

func (m Model) resizeInput() Model {
	lines := strings.Count(m.input.Value(), "\n") + 1
	h := clamp(lines, inputMinH, inputMaxH)
	m.input.SetHeight(h)
	return m
}

func (m Model) prevChatState() uiState {
	if m.activeSession != "" {
		return stateChat
	}
	return stateWelcome
}

// ─── Workspace commands ───────────────────────────────────────────────────────

func (m Model) loadSessions() tea.Cmd {
	return func() tea.Msg {
		m.workspace.ListSessions(m.ctx)
		return nil
	}
}

func (m Model) createSession() tea.Cmd {
	return func() tea.Msg {
		m.workspace.CreateSession(m.ctx)
		return nil
	}
}

func (m Model) loadSession(id string) tea.Cmd {
	return func() tea.Msg {
		m.workspace.LoadSession(m.ctx, id)
		return nil
	}
}

func (m Model) deleteSession(id string) tea.Cmd {
	return func() tea.Msg {
		_ = m.workspace.DeleteSession(m.ctx, id)
		m.workspace.ListSessions(m.ctx)
		return nil
	}
}

// ─── Overlay compositor ───────────────────────────────────────────────────────

// overlayOn places overlay centred on a dimmed base using line-by-line merging.
func overlayOn(base, overlay string, width, height int) string {
	if overlay == "" {
		return base
	}

	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")
	overlayH := len(overlayLines)

	for len(baseLines) < height {
		baseLines = append(baseLines, strings.Repeat(" ", width))
	}

	topOffset := max(0, (height-overlayH)/2)

	dim := lipgloss.NewStyle().Faint(true)

	for i, line := range baseLines {
		overlayRow := i - topOffset
		if overlayRow >= 0 && overlayRow < overlayH {
			// Replace this row with the overlay line (already includes its own padding).
			baseLines[i] = overlayLines[overlayRow]
		} else {
			baseLines[i] = dim.Render(line)
		}
	}

	return strings.Join(baseLines, "\n")
}

// ─── Utilities ────────────────────────────────────────────────────────────────

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Run starts the BubbleTea program and blocks until it exits.
func Run(ws tui.Workspace, ctx context.Context) error {
	m := New(ws, ctx)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	ws.Subscribe(p)
	_, err := p.Run()
	return err
}

// Unused but satisfies the compiler — will be used by session browser footer.
var _ = fmt.Sprintf
