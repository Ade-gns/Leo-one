// Tests d'intégration PatchRepo — nécessitent une base Postgres de test
// réelle (voir internal/testutil.TestDB).
package postgres

import (
	"context"
	"testing"

	patchDomain "github.com/yourorg/leo-one/internal/domain/patch"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestPatchRepo_Upsert_And_ListByAgent(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Patch Corp", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "paris-01")
	repo := NewPatchRepo(pool)
	ctx := context.Background()

	reports := []patchDomain.Report{
		{NativeID: "bash", Title: "bash → 5.1.1", Severity: patchDomain.SeverityImportant, SizeBytes: 1024},
		{NativeID: "KB5031354", Title: "KB5031354 → cumulative update", Severity: patchDomain.SeverityCritical, SizeBytes: 2048},
	}
	if err := repo.Upsert(ctx, tenantID, agentID, reports); err != nil {
		t.Fatalf("Upsert a échoué : %v", err)
	}

	patches, _, err := repo.ListByAgent(ctx, tenantID, agentID, patchDomain.ListFilter{})
	if err != nil {
		t.Fatalf("ListByAgent a échoué : %v", err)
	}
	if len(patches) != 2 {
		t.Fatalf("len(patches) = %d, attendu 2", len(patches))
	}
	for _, p := range patches {
		if p.Status != patchDomain.StatusAvailable {
			t.Errorf("status = %s, attendu %s", p.Status, patchDomain.StatusAvailable)
		}
	}
}

func TestPatchRepo_Upsert_ReAppearingPatchResetsToAvailable(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Patch Reset Corp", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "paris-02")
	repo := NewPatchRepo(pool)
	ctx := context.Background()

	report := []patchDomain.Report{{NativeID: "bash", Title: "bash", Severity: patchDomain.SeverityImportant}}
	if err := repo.Upsert(ctx, tenantID, agentID, report); err != nil {
		t.Fatalf("Upsert (1) a échoué : %v", err)
	}
	if err := repo.MarkInstallResult(ctx, tenantID, agentID, []string{"bash"}, true); err != nil {
		t.Fatalf("MarkInstallResult a échoué : %v", err)
	}

	patches, _, _ := repo.ListByAgent(ctx, tenantID, agentID, patchDomain.ListFilter{})
	if len(patches) != 1 || patches[0].Status != patchDomain.StatusInstalled {
		t.Fatalf("statut attendu 'installed' après MarkInstallResult, obtenu %+v", patches)
	}

	// Le même paquet redevient disponible (nouvelle version à mettre à jour) :
	// l'upsert doit repasser son statut à 'available' et effacer installed_at.
	if err := repo.Upsert(ctx, tenantID, agentID, report); err != nil {
		t.Fatalf("Upsert (2) a échoué : %v", err)
	}
	patches, _, _ = repo.ListByAgent(ctx, tenantID, agentID, patchDomain.ListFilter{})
	if len(patches) != 1 {
		t.Fatalf("len(patches) = %d, attendu 1", len(patches))
	}
	if patches[0].Status != patchDomain.StatusAvailable {
		t.Errorf("status = %s, attendu %s après réapparition", patches[0].Status, patchDomain.StatusAvailable)
	}
	if patches[0].InstalledAt != nil {
		t.Errorf("installed_at devrait être nil après réapparition, obtenu %v", patches[0].InstalledAt)
	}
}

func TestPatchRepo_AvailableNativeIDs_FiltersBySeverity(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Patch Severity Corp", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "paris-03")
	repo := NewPatchRepo(pool)
	ctx := context.Background()

	reports := []patchDomain.Report{
		{NativeID: "optional-pkg", Title: "x", Severity: patchDomain.SeverityOptional},
		{NativeID: "important-pkg", Title: "x", Severity: patchDomain.SeverityImportant},
		{NativeID: "critical-pkg", Title: "x", Severity: patchDomain.SeverityCritical},
	}
	if err := repo.Upsert(ctx, tenantID, agentID, reports); err != nil {
		t.Fatalf("Upsert a échoué : %v", err)
	}

	all, err := repo.AvailableNativeIDs(ctx, tenantID, agentID, patchDomain.SeverityOptional)
	if err != nil {
		t.Fatalf("AvailableNativeIDs(optional) a échoué : %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, attendu 3", len(all))
	}

	criticalOnly, err := repo.AvailableNativeIDs(ctx, tenantID, agentID, patchDomain.SeverityCritical)
	if err != nil {
		t.Fatalf("AvailableNativeIDs(critical) a échoué : %v", err)
	}
	if len(criticalOnly) != 1 || criticalOnly[0] != "critical-pkg" {
		t.Fatalf("criticalOnly = %v, attendu [critical-pkg]", criticalOnly)
	}
}

func TestPatchRepo_MarkInstallResult_Failure(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Patch Fail Corp", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "paris-04")
	repo := NewPatchRepo(pool)
	ctx := context.Background()

	report := []patchDomain.Report{{NativeID: "bash", Title: "bash", Severity: patchDomain.SeverityImportant}}
	if err := repo.Upsert(ctx, tenantID, agentID, report); err != nil {
		t.Fatalf("Upsert a échoué : %v", err)
	}
	if err := repo.MarkInstallResult(ctx, tenantID, agentID, []string{"bash"}, false); err != nil {
		t.Fatalf("MarkInstallResult a échoué : %v", err)
	}

	patches, _, _ := repo.ListByAgent(ctx, tenantID, agentID, patchDomain.ListFilter{})
	if len(patches) != 1 || patches[0].Status != patchDomain.StatusFailed {
		t.Fatalf("statut attendu 'failed', obtenu %+v", patches)
	}
	if patches[0].InstalledAt != nil {
		t.Errorf("installed_at devrait rester nil après un échec")
	}
}

func TestPatchRepo_Summary(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Patch Summary Corp", 10)
	agentA := testutil.SeedAgent(t, pool, tenantID, "agent-a")
	agentB := testutil.SeedAgent(t, pool, tenantID, "agent-b")
	repo := NewPatchRepo(pool)
	ctx := context.Background()

	if err := repo.Upsert(ctx, tenantID, agentA, []patchDomain.Report{
		{NativeID: "p1", Title: "x", Severity: patchDomain.SeverityCritical},
		{NativeID: "p2", Title: "x", Severity: patchDomain.SeverityImportant},
	}); err != nil {
		t.Fatalf("Upsert agentA a échoué : %v", err)
	}
	if err := repo.Upsert(ctx, tenantID, agentB, []patchDomain.Report{
		{NativeID: "p3", Title: "x", Severity: patchDomain.SeverityOptional},
	}); err != nil {
		t.Fatalf("Upsert agentB a échoué : %v", err)
	}

	summary, err := repo.Summary(ctx, tenantID)
	if err != nil {
		t.Fatalf("Summary a échoué : %v", err)
	}
	if summary.AgentsWithCriticalPending != 1 {
		t.Errorf("AgentsWithCriticalPending = %d, attendu 1", summary.AgentsWithCriticalPending)
	}
	if summary.AgentsWithPendingPatches != 2 {
		t.Errorf("AgentsWithPendingPatches = %d, attendu 2", summary.AgentsWithPendingPatches)
	}
	if summary.TotalPendingPatches != 3 {
		t.Errorf("TotalPendingPatches = %d, attendu 3", summary.TotalPendingPatches)
	}
}
