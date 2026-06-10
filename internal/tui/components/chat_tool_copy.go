package components

import (
	"fmt"
	"path/filepath"
	"strings"
)

// formatForCopy returns a Markdown representation of the tool call suitable for
// pasting into documents or other tools. Mirrors Crush's formatToolForCopy()
// pattern but adapted to our metadata-map architecture.
func (t *toolItem) formatForCopy() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("## %s", toolDisplayName(t.name)))

	if params := t.formatParamsForCopy(); params != "" {
		parts = append(parts, "### Parameters:")
		parts = append(parts, params)
	}

	switch {
	case t.status == "failed" || t.status == "error":
		parts = append(parts, "### Error:")
		if msg := t.resultContent(); msg != "" {
			parts = append(parts, msg)
		} else {
			parts = append(parts, t.label)
		}
	case t.isDone():
		if result := t.formatResultForCopy(); result != "" {
			parts = append(parts, "### Result:")
			parts = append(parts, result)
		}
	case t.awaitingPermission:
		parts = append(parts, "### Status:")
		parts = append(parts, "Awaiting permission…")
	default:
		parts = append(parts, "### Status:")
		parts = append(parts, "Running…")
	}

	return strings.Join(parts, "\n\n")
}

// formatParamsForCopy returns a brief human-readable parameter description.
func (t *toolItem) formatParamsForCopy() string {
	input := t.toolInput()
	switch t.name {
	case "bash":
		if cmd := strings.TrimSpace(stringFromMap(input, "command")); cmd != "" {
			return fmt.Sprintf("**Command:** %s", cmd)
		}
	case "read_file":
		if path := stringFromMap(input, "file_path"); path != "" {
			return fmt.Sprintf("**File:** %s", path)
		}
	case "write_file", "edit_file", "apply_patch":
		if path := stringFromMap(input, "file_path"); path != "" {
			return fmt.Sprintf("**File:** %s", path)
		}
	case "web_fetch":
		if url := strings.TrimSpace(stringFromMap(input, "url")); url == "" {
			url = strings.TrimSpace(stringFromMap(t.metadata, "url"))
			if url != "" {
				return fmt.Sprintf("**URL:** %s", url)
			}
		} else {
			return fmt.Sprintf("**URL:** %s", url)
		}
	case "web_search":
		if q := strings.TrimSpace(stringFromMap(input, "query")); q == "" {
			q = strings.TrimSpace(stringFromMap(t.metadata, "query"))
			if q != "" {
				return fmt.Sprintf("**Query:** %s", q)
			}
		} else {
			return fmt.Sprintf("**Query:** %s", q)
		}
	case "spawn_agent":
		if prompt := strings.TrimSpace(stringFromMap(input, "prompt")); prompt != "" {
			return fmt.Sprintf("**Prompt:** %s", prompt)
		}
	}
	if t.label != "" && t.label != t.status {
		return fmt.Sprintf("**Label:** %s", t.label)
	}
	return ""
}

// formatResultForCopy returns the tool result as a code-fenced Markdown block.
func (t *toolItem) formatResultForCopy() string {
	input := t.toolInput()
	switch t.name {
	case "bash":
		output := stringFromMap(t.metadata, "content")
		if output == "" {
			output = stringFromMap(t.metadata, "result")
		}
		if output == "" {
			return ""
		}
		return fmt.Sprintf("```bash\n%s\n```", output)

	case "read_file":
		body := stringFromMap(t.metadata, "content")
		if body == "" {
			body = stringFromMap(t.metadata, "result")
		}
		if body == "" {
			return ""
		}
		path := stringFromMap(input, "file_path")
		lang := langForPath(path)
		return fmt.Sprintf("```%s\n%s\n```", lang, body)

	case "write_file":
		content := stringFromMap(input, "content")
		if content == "" {
			content = stringFromMap(t.metadata, "content")
		}
		if content == "" {
			return ""
		}
		path := stringFromMap(input, "file_path")
		lang := langForPath(path)
		return fmt.Sprintf("```%s\n%s\n```", lang, content)

	case "edit_file", "apply_patch":
		diff := t.unifiedDiff()
		if diff == "" {
			diff = stringFromMap(t.metadata, "diff")
		}
		if diff != "" {
			if stats := t.changeStatsText(); stats != "" {
				return fmt.Sprintf("Changes: %s\n\n```diff\n%s\n```", stats, diff)
			}
			return fmt.Sprintf("```diff\n%s\n```", diff)
		}
		if content := stringFromMap(t.metadata, "content"); content != "" {
			path := stringFromMap(input, "file_path")
			lang := langForPath(path)
			return fmt.Sprintf("```%s\n%s\n```", lang, content)
		}
		return ""

	case "web_fetch", "web_search":
		if content := stringFromMap(t.metadata, "content"); content != "" {
			return fmt.Sprintf("```\n%s\n```", content)
		}
		return ""

	default:
		if content := t.resultContent(); content != "" {
			return fmt.Sprintf("```\n%s\n```", content)
		}
		return ""
	}
}

// langForPath maps a file extension to a Markdown code-fence language tag.
func langForPath(path string) string {
	if path == "" {
		return ""
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".jsx":
		return "jsx"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx", ".h", ".hpp":
		return "cpp"
	case ".sh", ".bash":
		return "bash"
	case ".zsh":
		return "zsh"
	case ".json", ".jsonc":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".scss", ".sass":
		return "scss"
	case ".md", ".mdx":
		return "markdown"
	case ".sql":
		return "sql"
	case ".proto":
		return "protobuf"
	case ".tf", ".tfvars":
		return "hcl"
	case ".dockerfile":
		return "dockerfile"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".lua":
		return "lua"
	case ".r":
		return "r"
	}
	if strings.ToLower(filepath.Base(path)) == "dockerfile" {
		return "dockerfile"
	}
	return ""
}
