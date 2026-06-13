package task

import (
	"fmt"

	tool "github.com/EngineerProjects/nexus-engine/internal/tools/registry"
)

func resolveTaskSessionID(input tool.CallInput) (string, error) {
	toolCtx := input.ToolContextValue()
	if toolCtx.SessionID != "" {
		return string(toolCtx.SessionID), nil
	}
	if input.SessionID != "" {
		return string(input.SessionID), nil
	}
	return "", fmt.Errorf("session ID is required for task tools")
}
