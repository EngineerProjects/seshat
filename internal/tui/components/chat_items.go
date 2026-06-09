package components

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/EngineerProjects/nexus-engine/internal/tui/components/list"
	"github.com/muesli/reflow/wrap"
)

type msgItem interface {
	list.Item
	invalidate()
}

type userItem struct {
	list.Versioned
	c         *Chat
	content   string
	timestamp time.Time
	cacheW    int
	cacheR    string
}

func (u *userItem) Finished() bool { return true }
func (u *userItem) invalidate()    { u.cacheW = 0; u.Bump() }

func (u *userItem) Render(width int) string {
	if u.cacheW == width && u.cacheR != "" {
		return u.cacheR
	}

	timeStr := ""
	if !u.timestamp.IsZero() {
		timeStr = u.timestamp.Format("15:04:05")
	}
	left := "👤 You"
	leftStyled := u.c.styles.UserLabel.Render(left)
	rightStyled := ""
	if timeStr != "" {
		rightStyled = u.c.styles.MsgTimestamp.Render(timeStr)
	}

	header := leftStyled
	if rightStyled != "" {
		padding := width - lipgloss.Width(leftStyled) - lipgloss.Width(rightStyled)
		if padding > 0 {
			header += strings.Repeat(" ", padding) + rightStyled
		} else {
			header += " " + rightStyled
		}
	}

	bar := u.c.styles.UserMarker.Render("│")
	prefix := "  " + bar + " "

	bodyWidth := max(12, width-4)
	wrapped := strings.Split(wrap.String(u.content, bodyWidth), "\n")
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	for i := 0; i < len(wrapped); i++ {
		wrapped[i] = prefix + u.c.styles.UserMsg.Render(wrapped[i])
	}
	body := strings.Join(wrapped, "\n")

	r := header + "\n" + body
	u.cacheW = width
	u.cacheR = r
	return r
}

type systemItem struct {
	list.Versioned
	c       *Chat
	content string
}

func (s *systemItem) Finished() bool { return true }
func (s *systemItem) invalidate()    {}
func (s *systemItem) Render(width int) string {
	return s.c.styles.MsgTimestamp.Render("─ " + s.content)
}

type errorItem struct {
	list.Versioned
	c       *Chat
	content string
}

func (e *errorItem) Finished() bool { return true }
func (e *errorItem) invalidate()    {}
func (e *errorItem) Render(width int) string {
	return e.c.styles.ToolError.Render("✗ " + e.content)
}

type toolRegion struct {
	startLine     int
	endLine       int
	msgIndex      int
	expanderStart int
	expanderEnd   int
	detailStart   int
	detailEnd     int
}

type thinkingRegion struct {
	startLine int
	endLine   int
	msgIndex  int
}

type itemRegion struct {
	startLine int
	endLine   int
}
