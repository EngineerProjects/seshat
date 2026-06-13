package model

import (
	"testing"

	"github.com/EngineerProjects/nexus-engine/internal/nexustui/session"
)

func TestEnsureSidebarTaskSelectionPrefersInProgress(t *testing.T) {
	m := &UI{session: &session.Session{Todos: []session.Todo{
		{ID: "done", Content: "Done", Status: session.TodoStatusCompleted},
		{ID: "active", Content: "Active", Status: session.TodoStatusInProgress},
		{ID: "pending", Content: "Pending", Status: session.TodoStatusPending},
	}}}
	m.ensureSidebarTaskSelection()
	if m.selectedSidebarTaskID != "active" {
		t.Fatalf("expected active task selected, got %q", m.selectedSidebarTaskID)
	}
}

func TestEnsureSidebarTaskSelectionKeepsExistingWhenPresent(t *testing.T) {
	m := &UI{selectedSidebarTaskID: "pending", session: &session.Session{Todos: []session.Todo{
		{ID: "pending", Content: "Pending", Status: session.TodoStatusPending},
		{ID: "done", Content: "Done", Status: session.TodoStatusCompleted},
	}}}
	m.ensureSidebarTaskSelection()
	if m.selectedSidebarTaskID != "pending" {
		t.Fatalf("expected pending task to remain selected, got %q", m.selectedSidebarTaskID)
	}
}
