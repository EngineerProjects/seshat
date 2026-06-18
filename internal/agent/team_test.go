package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/EngineerProjects/nexus-engine/internal/agent"
	"github.com/EngineerProjects/nexus-engine/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTeamRegistry(t *testing.T) (*agent.TeamRegistry, *agent.ProfileRegistry) {
	t.Helper()
	database, err := db.Open(context.Background(), db.Config{
		Driver:      db.DriverSQLite,
		DSN:         ":memory:",
		AutoMigrate: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	profiles := agent.NewProfileRegistry(database)
	require.NoError(t, profiles.Seed(context.Background()))

	teams := agent.NewTeamRegistry(database, profiles)
	return teams, profiles
}

// ─── NewTeam ─────────────────────────────────────────────────────────────────

func TestNewTeam_GeneratesUUID(t *testing.T) {
	a := agent.NewTeam("Alpha", "")
	b := agent.NewTeam("Beta", "")
	if a.ID == "" || b.ID == "" {
		t.Fatal("NewTeam must generate a non-empty ID")
	}
	if a.ID == b.ID {
		t.Fatal("NewTeam must generate unique IDs")
	}
}

// ─── Create / Get ─────────────────────────────────────────────────────────────

func TestTeamRegistry_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	team := agent.NewTeam("Product", "Product squad")
	require.NoError(t, reg.Create(ctx, team))

	got, err := reg.Get(ctx, team.ID)
	require.NoError(t, err)
	assert.Equal(t, team.ID, got.ID)
	assert.Equal(t, "Product", got.Name)
	assert.Equal(t, "Product squad", got.Description)
}

func TestTeamRegistry_GetByName(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	team := agent.NewTeam("Engineering", "")
	require.NoError(t, reg.Create(ctx, team))

	got, err := reg.GetByName(ctx, "Engineering")
	require.NoError(t, err)
	assert.Equal(t, team.ID, got.ID)
}

func TestTeamRegistry_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	_, err := reg.Get(ctx, "nonexistent-uuid")
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrTeamNotFound))
}

func TestTeamRegistry_Create_EmptyName(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	team := agent.NewTeam("", "")
	err := reg.Create(ctx, team)
	require.Error(t, err)
}

func TestTeamRegistry_Create_DuplicateName(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	a := agent.NewTeam("Shared", "first")
	require.NoError(t, reg.Create(ctx, a))

	b := agent.NewTeam("Shared", "second")
	err := reg.Create(ctx, b)
	require.Error(t, err, "duplicate team name must be rejected")
}

// ─── List ─────────────────────────────────────────────────────────────────────

func TestTeamRegistry_List(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	require.NoError(t, reg.Create(ctx, agent.NewTeam("Zebra", "")))
	require.NoError(t, reg.Create(ctx, agent.NewTeam("Alpha", "")))
	require.NoError(t, reg.Create(ctx, agent.NewTeam("Mango", "")))

	teams, err := reg.List(ctx)
	require.NoError(t, err)
	require.Len(t, teams, 3)
	// Ordered by name
	assert.Equal(t, "Alpha", teams[0].Name)
	assert.Equal(t, "Mango", teams[1].Name)
	assert.Equal(t, "Zebra", teams[2].Name)
}

// ─── Update ───────────────────────────────────────────────────────────────────

func TestTeamRegistry_Update(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	team := agent.NewTeam("OldName", "old desc")
	require.NoError(t, reg.Create(ctx, team))

	team.Name = "NewName"
	team.Description = "new desc"
	require.NoError(t, reg.Update(ctx, team))

	got, err := reg.Get(ctx, team.ID)
	require.NoError(t, err)
	assert.Equal(t, "NewName", got.Name)
	assert.Equal(t, "new desc", got.Description)
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestTeamRegistry_Delete(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	team := agent.NewTeam("Temp", "")
	require.NoError(t, reg.Create(ctx, team))

	require.NoError(t, reg.Delete(ctx, team.ID))

	_, err := reg.Get(ctx, team.ID)
	assert.True(t, errors.Is(err, agent.ErrTeamNotFound))
}

// ─── AddMember / RemoveMember / Members ──────────────────────────────────────

func TestTeamRegistry_AddAndRemoveMember(t *testing.T) {
	ctx := context.Background()
	reg, profiles := setupTeamRegistry(t)

	team := agent.NewTeam("Alpha", "")
	require.NoError(t, reg.Create(ctx, team))

	// Find a built-in profile to assign
	all, err := profiles.List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, all)
	aria := all[0]

	require.NoError(t, reg.AddMember(ctx, team.ID, aria.ID))

	members, err := reg.Members(ctx, team.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, aria.ID, members[0].ID)

	require.NoError(t, reg.RemoveMember(ctx, aria.ID))

	members, err = reg.Members(ctx, team.ID)
	require.NoError(t, err)
	assert.Empty(t, members)
}

func TestTeamRegistry_AddMember_ReassignsTeam(t *testing.T) {
	ctx := context.Background()
	reg, profiles := setupTeamRegistry(t)

	alpha := agent.NewTeam("Alpha", "")
	beta := agent.NewTeam("Beta", "")
	require.NoError(t, reg.Create(ctx, alpha))
	require.NoError(t, reg.Create(ctx, beta))

	all, err := profiles.List(ctx)
	require.NoError(t, err)
	aria := all[0]

	require.NoError(t, reg.AddMember(ctx, alpha.ID, aria.ID))
	require.NoError(t, reg.AddMember(ctx, beta.ID, aria.ID))

	alphaMembers, _ := reg.Members(ctx, alpha.ID)
	betaMembers, _ := reg.Members(ctx, beta.ID)

	assert.Empty(t, alphaMembers, "agent should no longer be in alpha after re-assignment")
	assert.Len(t, betaMembers, 1)
}

func TestTeamRegistry_AddMember_EmptyArgs(t *testing.T) {
	ctx := context.Background()
	reg, _ := setupTeamRegistry(t)

	assert.Error(t, reg.AddMember(ctx, "", "agent-id"))
	assert.Error(t, reg.AddMember(ctx, "team-id", ""))
	assert.Error(t, reg.RemoveMember(ctx, ""))
}
