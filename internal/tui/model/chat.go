package model

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
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
	content   string
	label     string
	timestamp time.Time
	done      bool

	// draw cache: memoized render keyed by width + content
	cachedWidth  int
	cachedRender string
}

// chat is the scrollable chat history view.
type chat struct {
	styles   Styles
	viewport *viewport.Model // pointer so SetWidth/SetHeight (pointer receivers) work
	renderer *glamour.TermRenderer
	messages []*chatMessage
	width    int
	height   int
	follow   bool
}

func newChat(styles Styles, width, height int) *chat {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	vp.SetContent("")

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(min(width-4, 100)),
	)

	return &chat{
		styles:   styles,
		viewport: &vp,
		renderer: renderer,
		follow:   true,
		width:    width,
		height:   height,
	}
}

func (c *chat) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.viewport.SetWidth(width)
	c.viewport.SetHeight(height)

	if r, err := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithWordWrap(min(width-4, 100)),
	); err == nil {
		c.renderer = r
	}

	for _, m := range c.messages {
		m.cachedWidth = 0
	}
	c.refresh()
}

func (c *chat) AddUserMessage(text string) {
	c.messages = append(c.messages, &chatMessage{
		kind: msgKindUser, content: text, label: "You",
		timestamp: time.Now(), done: true,
	})
	c.refresh()
}

func (c *chat) StartAssistantMessage() {
	c.messages = append(c.messages, &chatMessage{
		kind: msgKindAssistant, content: "", label: "Nexus",
		timestamp: time.Now(), done: false,
	})
	c.refresh()
}

func (c *chat) AppendChunk(text string, _ bool) {
	for i := len(c.messages) - 1; i >= 0; i-- {
		m := c.messages[i]
		if m.kind == msgKindAssistant && !m.done {
			m.content += text
			m.cachedWidth = 0
			c.refresh()
			return
		}
	}
	c.StartAssistantMessage()
	c.AppendChunk(text, false)
}

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

func (c *chat) AddToolProgress(toolName, status, label string) {
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
	c.messages = append(c.messages, &chatMessage{
		kind: msgKindTool, content: label, label: toolName,
		timestamp: time.Now(), done: status == "done" || status == "error",
	})
	c.refresh()
}

func (c *chat) AddError(err error) {
	c.messages = append(c.messages, &chatMessage{
		kind: msgKindError, content: err.Error(), label: "error",
		timestamp: time.Now(), done: true,
	})
	c.refresh()
}

func (c *chat) AddSystem(text string) {
	c.messages = append(c.messages, &chatMessage{
		kind: msgKindSystem, content: text,
		timestamp: time.Now(), done: true,
	})
	c.refresh()
}

func (c *chat) Clear() {
	c.messages = c.messages[:0]
	c.refresh()
}

func (c *chat) ScrollUp(n int)   { c.follow = false; c.viewport.ScrollUp(n) }
func (c *chat) ScrollDown(n int) { c.viewport.ScrollDown(n); c.follow = c.viewport.AtBottom() }
func (c *chat) PageUp()          { c.follow = false; c.viewport.HalfPageUp() }
func (c *chat) PageDown()        { c.viewport.HalfPageDown(); c.follow = c.viewport.AtBottom() }
func (c *chat) GotoTop()         { c.follow = false; c.viewport.GotoTop() }
func (c *chat) GotoBottom()      { c.follow = true; c.viewport.GotoBottom() }

func (c *chat) View() string { return c.viewport.View() }

func (c *chat) refresh() {
	var sb strings.Builder
	for i, m := range c.messages {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(c.renderMessage(m))
	}
	c.viewport.SetContent(sb.String())
	if c.follow {
		c.viewport.GotoBottom()
	}
}

func (c *chat) renderMessage(m *chatMessage) string {
	if m.cachedWidth == c.width && m.cachedRender != "" && m.done {
		return m.cachedRender
	}

	var s strings.Builder
	switch m.kind {
	case msgKindUser:
		s.WriteString(fmt.Sprintf("%s %s\n",
			c.styles.UserLabel.Render("You"),
			c.styles.MsgTimestamp.Render(m.timestamp.Format("15:04")),
		))
		s.WriteString(c.styles.UserMsg.Render(wrap.String(m.content, c.width-2)))

	case msgKindAssistant:
		s.WriteString(fmt.Sprintf("%s %s\n",
			c.styles.AssistantLabel.Render("Nexus"),
			c.styles.MsgTimestamp.Render(m.timestamp.Format("15:04")),
		))
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
		name := c.styles.ToolProgress.Render(m.label)
		if m.done {
			icon = c.styles.ToolDone.Render("✓")
			name = c.styles.ToolDone.Render(m.label)
		}
		line := icon + " " + name
		if m.content != "" {
			line += c.styles.MsgTimestamp.Render(" · " + m.content)
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
