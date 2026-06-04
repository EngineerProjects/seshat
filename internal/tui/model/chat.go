package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wrap"
)

// Message kinds for the chat display.
const (
	msgKindUser      = "user"
	msgKindAssistant = "assistant"
	msgKindTool      = "tool"
	msgKindError     = "error"
	msgKindSystem    = "system"
)

// chatMessage holds a single displayable message in the chat.
type chatMessage struct {
	kind      string
	content   string // final or accumulated content
	label     string // "You", "Nexus", tool name, etc.
	timestamp time.Time
	done      bool // false while streaming

	// draw cache: last rendered output for this message + width
	cachedWidth  int
	cachedRender string
}

// chat is the scrollable chat history view.
type chat struct {
	styles   Styles
	viewport viewport.Model
	renderer *glamour.TermRenderer
	messages []*chatMessage
	width    int
	height   int
	follow   bool // auto-scroll to bottom on new content
	ready    bool
}

func newChat(styles Styles, width, height int) *chat {
	vp := viewport.New(width, height)
	vp.SetContent("")

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(min(width-4, 100)),
	)

	return &chat{
		styles:   styles,
		viewport: vp,
		renderer: renderer,
		follow:   true,
		width:    width,
		height:   height,
	}
}

func (c *chat) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.viewport.Width = width
	c.viewport.Height = height

	// Rebuild renderer with new width.
	if r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(min(width-4, 100)),
	); err == nil {
		c.renderer = r
	}

	// Invalidate all draw caches.
	for _, m := range c.messages {
		m.cachedWidth = 0
	}
	c.refresh()
}

// AddUserMessage appends a user turn.
func (c *chat) AddUserMessage(text string) {
	c.messages = append(c.messages, &chatMessage{
		kind:      msgKindUser,
		content:   text,
		label:     "You",
		timestamp: time.Now(),
		done:      true,
	})
	c.refresh()
}

// StartAssistantMessage adds a new (empty) assistant message for streaming.
func (c *chat) StartAssistantMessage() {
	c.messages = append(c.messages, &chatMessage{
		kind:      msgKindAssistant,
		content:   "",
		label:     "Nexus",
		timestamp: time.Now(),
		done:      false,
	})
	c.refresh()
}

// AppendChunk appends a streaming text delta to the last assistant message.
func (c *chat) AppendChunk(text string, isThinking bool) {
	for i := len(c.messages) - 1; i >= 0; i-- {
		m := c.messages[i]
		if m.kind == msgKindAssistant && !m.done {
			m.content += text
			m.cachedWidth = 0 // invalidate cache
			c.refresh()
			return
		}
	}
	// No open assistant message; start one.
	c.StartAssistantMessage()
	c.AppendChunk(text, isThinking)
}

// FinishAssistantMessage marks the last open assistant message as done.
func (c *chat) FinishAssistantMessage() {
	for i := len(c.messages) - 1; i >= 0; i-- {
		if c.messages[i].kind == msgKindAssistant && !c.messages[i].done {
			c.messages[i].done = true
			c.messages[i].cachedWidth = 0
			c.refresh()
			return
		}
	}
}

// AddToolProgress adds/updates a tool progress line.
func (c *chat) AddToolProgress(toolName, status, label string) {
	// Look for an existing running tool entry to update.
	for _, m := range c.messages {
		if m.kind == msgKindTool && m.label == toolName && !m.done {
			m.content = label
			m.done = status == "done" || status == "error"
			if status == "error" {
				m.kind = msgKindError
			}
			m.cachedWidth = 0
			c.refresh()
			return
		}
	}
	// New tool entry.
	c.messages = append(c.messages, &chatMessage{
		kind:      msgKindTool,
		content:   label,
		label:     toolName,
		timestamp: time.Now(),
		done:      status == "done" || status == "error",
	})
	c.refresh()
}

// AddError appends an error message.
func (c *chat) AddError(err error) {
	c.messages = append(c.messages, &chatMessage{
		kind:      msgKindError,
		content:   err.Error(),
		label:     "error",
		timestamp: time.Now(),
		done:      true,
	})
	c.refresh()
}

// AddSystem adds an informational system message.
func (c *chat) AddSystem(text string) {
	c.messages = append(c.messages, &chatMessage{
		kind:      msgKindSystem,
		content:   text,
		label:     "",
		timestamp: time.Now(),
		done:      true,
	})
	c.refresh()
}

// Clear removes all messages.
func (c *chat) Clear() {
	c.messages = c.messages[:0]
	c.refresh()
}

// ScrollUp scrolls the viewport up by n lines.
func (c *chat) ScrollUp(n int) {
	c.follow = false
	c.viewport.LineUp(n)
}

// ScrollDown scrolls the viewport down by n lines.
func (c *chat) ScrollDown(n int) {
	c.viewport.LineDown(n)
	c.follow = c.viewport.AtBottom()
}

// PageUp scrolls up one page.
func (c *chat) PageUp() {
	c.follow = false
	c.viewport.HalfViewUp()
}

// PageDown scrolls down one page.
func (c *chat) PageDown() {
	c.viewport.HalfViewDown()
	c.follow = c.viewport.AtBottom()
}

// GotoTop scrolls to the top.
func (c *chat) GotoTop() {
	c.follow = false
	c.viewport.GotoTop()
}

// GotoBottom scrolls to the bottom and re-enables follow.
func (c *chat) GotoBottom() {
	c.follow = true
	c.viewport.GotoBottom()
}

// View renders the chat viewport.
func (c *chat) View() string {
	return c.viewport.View()
}

// refresh rebuilds the viewport content from all messages.
func (c *chat) refresh() {
	var sb strings.Builder
	for i, m := range c.messages {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(c.renderMessage(m))
	}
	content := sb.String()
	c.viewport.SetContent(content)
	if c.follow {
		c.viewport.GotoBottom()
	}
}

// renderMessage renders a single message, using a draw cache when possible.
func (c *chat) renderMessage(m *chatMessage) string {
	if m.cachedWidth == c.width && m.cachedRender != "" && m.done {
		return m.cachedRender
	}

	var s strings.Builder
	switch m.kind {
	case msgKindUser:
		label := c.styles.UserLabel.Render("You")
		ts := c.styles.MsgTimestamp.Render(m.timestamp.Format("15:04"))
		s.WriteString(fmt.Sprintf("%s %s\n", label, ts))
		body := wrap.String(m.content, c.width-2)
		s.WriteString(c.styles.UserMsg.Render(body))

	case msgKindAssistant:
		label := c.styles.AssistantLabel.Render("Nexus")
		ts := c.styles.MsgTimestamp.Render(m.timestamp.Format("15:04"))
		s.WriteString(fmt.Sprintf("%s %s\n", label, ts))
		if m.content != "" {
			rendered, err := c.renderer.Render(m.content)
			if err != nil {
				rendered = m.content
			}
			s.WriteString(strings.TrimRight(rendered, "\n"))
		} else {
			s.WriteString(c.styles.MsgTimestamp.Render("…"))
		}

	case msgKindTool:
		icon := c.styles.ToolProgress.Render("●")
		if m.done {
			icon = c.styles.ToolDone.Render("✓")
		}
		name := c.styles.ToolProgress.Render(m.label)
		if m.done {
			name = c.styles.ToolDone.Render(m.label)
		}
		line := fmt.Sprintf("%s %s", icon, name)
		if m.content != "" {
			line += c.styles.MsgTimestamp.Render(" · "+m.content)
		}
		s.WriteString(line)

	case msgKindError:
		s.WriteString(c.styles.ToolError.Render("✗ " + m.label + ": " + m.content))

	case msgKindSystem:
		s.WriteString(c.styles.MsgTimestamp.Render("─ " + m.content))

	default:
		s.WriteString(m.content)
	}

	rendered := s.String()
	if m.done {
		m.cachedWidth = c.width
		m.cachedRender = rendered
	}
	return rendered
}

// headerLine builds the horizontal rule above the chat.
func headerLine(style lipgloss.Style, width int) string {
	return style.Render(strings.Repeat("─", max(0, width)))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
