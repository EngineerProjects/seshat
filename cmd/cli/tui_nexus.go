package main

import (
	"context"
	"io"
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	crushcommon "github.com/EngineerProjects/nexus-engine/internal/nexustui/ui/common"
	uimodel "github.com/EngineerProjects/nexus-engine/internal/nexustui/ui/model"
	crushws "github.com/EngineerProjects/nexus-engine/internal/nexustui/workspace"
)

// runNexusTUI starts the Crush-based TUI. It reuses the same
// SDK configuration and session wiring as the original TUI but delegates
// all rendering to the copied Crush UI layer.
func runNexusTUI(ctx context.Context, options runtimeOptions, initialSessionID string, continueLast bool) error {
	if err := validateProviderSetup(options); err != nil {
		return err
	}

	options.Monitoring = buildTUIMonitoring()
	if lf := openCLILogFile(); lf != nil {
		log.SetOutput(lf)
	} else {
		log.SetOutput(io.Discard)
	}

	modelStr := ""
	if options.Model.Provider != "" {
		modelStr = string(options.Model.Provider) + ":" + options.Model.Model
	}

	ws := crushws.NewNexusWorkspace(nil, options.WorkingDir, modelStr)

	client, err := newClient(
		options,
		ws.PromptFn,
		ws.OnProgress,
		ws.OnChunk,
		ws.OnRuntimeEvent,
		ws.OnSessionTitled,
	)
	if err != nil {
		return err
	}
	ws.SetSDKClient(client)

	com := crushcommon.DefaultCommon(ws)
	uiModel := uimodel.New(com, initialSessionID, continueLast)

	var env uv.Environ = os.Environ()
	p := tea.NewProgram(
		uiModel,
		tea.WithEnvironment(env),
		tea.WithContext(ctx),
		tea.WithFilter(uimodel.MouseEventFilter),
	)
	go ws.Subscribe(p)

	_, runErr := p.Run()

	ws.Shutdown()
	return runErr
}
