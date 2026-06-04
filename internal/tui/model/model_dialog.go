package model

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/EngineerProjects/nexus-engine/internal/tui"
)

// modelDialog is the Ctrl+M model selection overlay.
// Inspired by crush's dialog/models.go.
type modelDialog struct {
	styles   Styles
	models   []tui.ProviderModel
	filtered []tui.ProviderModel
	filter   string
	cursor   int
	width    int
	height   int
}

func newModelDialog(styles Styles) *modelDialog {
	return &modelDialog{styles: styles}
}

func (d *modelDialog) SetModels(models []tui.ProviderModel) {
	d.models = models
	d.cursor = 0
	d.applyFilter()
}

func (d *modelDialog) SetSize(width, height int) {
	d.width = width
	d.height = height
}

func (d *modelDialog) TypeFilter(ch string)  { d.filter += ch; d.cursor = 0; d.applyFilter() }
func (d *modelDialog) DeleteFilter()         {
	if len(d.filter) > 0 {
		d.filter = d.filter[:len(d.filter)-1]
		d.cursor = 0
		d.applyFilter()
	}
}
func (d *modelDialog) ClearFilter() { d.filter = ""; d.cursor = 0; d.applyFilter() }
func (d *modelDialog) Up()          { if d.cursor > 0 { d.cursor-- } }
func (d *modelDialog) Down()        { if d.cursor < len(d.filtered)-1 { d.cursor++ } }

// Selected returns the selected model, or nil.
func (d *modelDialog) Selected() *tui.ProviderModel {
	if d.cursor >= 0 && d.cursor < len(d.filtered) {
		m := d.filtered[d.cursor]
		return &m
	}
	return nil
}

func (d *modelDialog) applyFilter() {
	if d.filter == "" {
		d.filtered = make([]tui.ProviderModel, len(d.models))
		copy(d.filtered, d.models)
		return
	}
	needle := strings.ToLower(d.filter)
	d.filtered = d.filtered[:0]
	for _, m := range d.models {
		if strings.Contains(strings.ToLower(m.DisplayName), needle) ||
			strings.Contains(strings.ToLower(m.Description), needle) {
			d.filtered = append(d.filtered, m)
		}
	}
}

func (d *modelDialog) View() string {
	const boxWidth = 70
	const maxItems = 12
	w := min(boxWidth, d.width-4)

	title := d.styles.BrowserTitle.Render("  Select Model")
	filterLine := d.styles.BrowserFilter.Width(w - 2).Render("/ " + d.filter + "█")
	sep := d.styles.MsgTimestamp.Render(strings.Repeat("─", w-2))

	start := max(0, d.cursor-maxItems+1)
	end := min(len(d.filtered), start+maxItems)

	var rows []string
	for i := start; i < end; i++ {
		m := d.filtered[i]
		ctx := ""
		if m.Context > 0 {
			ctx = fmt.Sprintf(" %dk ctx", m.Context/1000)
		}
		line := m.DisplayName + ctx
		if len(m.Description) > 0 {
			maxDesc := w - 6 - len(line)
			if maxDesc > 10 {
				desc := m.Description
				if len(desc) > maxDesc {
					desc = desc[:maxDesc-1] + "…"
				}
				line += "  " + d.styles.MsgTimestamp.Render(desc)
			}
		}
		if i == d.cursor {
			rows = append(rows, d.styles.BrowserSelected.Width(w-2).Render("▶ "+line))
		} else {
			rows = append(rows, d.styles.BrowserItem.Width(w-2).Render("  "+line))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, d.styles.BrowserItem.Render("  no matches"))
	}

	hint := d.styles.Footer.Render("enter: select  esc/ctrl+m: close")
	content := strings.Join([]string{
		title,
		filterLine,
		sep,
		strings.Join(rows, "\n"),
		sep,
		hint,
	}, "\n")

	return d.styles.BrowserBorder.Width(w).Render(content)
}

func (d *modelDialog) centred() string {
	box := d.View()
	lines := strings.Split(box, "\n")
	boxH := len(lines)
	boxW := lipgloss.Width(box)
	top := max(0, (d.height-boxH)/2)
	left := max(0, (d.width-boxW)/2)
	pad := strings.Repeat(" ", left)
	var sb strings.Builder
	for i := 0; i < top; i++ {
		sb.WriteString("\n")
	}
	for i, l := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(pad + l)
	}
	return sb.String()
}
