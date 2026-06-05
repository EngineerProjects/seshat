package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func selectionRenderParts(style lipgloss.Style) (string, string) {
	sample := style.Render("x")
	idx := strings.Index(sample, "x")
	if idx < 0 {
		return "", ""
	}
	return sample[:idx], sample[idx+1:]
}

func applySelectionStyle(s string, style lipgloss.Style) string {
	if s == "" {
		return ""
	}
	prefix, suffix := selectionRenderParts(style)
	if prefix == "" && suffix == "" {
		return s
	}
	out := prefix + s
	out = strings.ReplaceAll(out, "[0m", "[0m"+prefix)
	out = strings.ReplaceAll(out, "[m", "[m"+prefix)
	return out + suffix
}
