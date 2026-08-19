package postgres

import (
	"context"
	"testing"
	"time"

	rdDomain "github.com/yourorg/leo-one/internal/domain/remotedesktop"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestRemoteDesktopRepo_CreateAndConsumeTokens_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	repo := NewRemoteDesktopRepo(pool)
	tenantID := testutil.SeedTenant(t, pool, "RD Tenant", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "rd-agent-1")

	sess := &rdDomain.Session{TenantID: tenantID, AgentID: agentID, Mode: rdDomain.ModeControl}
	viewerToken, agentToken, err := repo.Create(context.Background(), sess, time.Minute)
	if err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}
	if sess.ID == "" || sess.Status != rdDomain.StatusPending {
		t.Fatalf("session mal initialisée après Create : %+v", sess)
	}

	// Un jeton invalide ne consomme rien.
	if got, err := repo.ConsumeViewerToken(context.Background(), "not-a-real-token"); err != nil || got != nil {
		t.Fatalf("jeton invalide aurait dû retourner (nil, nil), a retourné (%v, %v)", got, err)
	}

	// Consommer le jeton agent ne rend pas encore la session active (le
	// jeton navigateur n'a pas été consommé) — status doit rester pending.
	afterAgent, err := repo.ConsumeAgentToken(context.Background(), agentToken)
	if err != nil {
		t.Fatalf("ConsumeAgentToken a échoué : %v", err)
	}
	if afterAgent == nil || afterAgent.Status != rdDomain.StatusPending {
		t.Fatalf("statut attendu pending après un seul côté connecté, obtenu : %+v", afterAgent)
	}
	if afterAgent.AgentConnectedAt == nil {
		t.Fatal("AgentConnectedAt aurait dû être renseigné")
	}

	// Consommer le jeton navigateur ensuite rend la session active.
	afterViewer, err := repo.ConsumeViewerToken(context.Background(), viewerToken)
	if err != nil {
		t.Fatalf("ConsumeViewerToken a échoué : %v", err)
	}
	if afterViewer == nil || afterViewer.Status != rdDomain.StatusActive {
		t.Fatalf("statut attendu active une fois les deux côtés connectés, obtenu : %+v", afterViewer)
	}

	// Un jeton déjà consommé ne peut pas resservir.
	if got, err := repo.ConsumeAgentToken(context.Background(), agentToken); err != nil || got != nil {
		t.Fatalf("jeton agent déjà consommé aurait dû échouer, a retourné (%v, %v)", got, err)
	}

	// ActiveForAgent retrouve la session tant qu'elle n'est pas terminée.
	active, err := repo.ActiveForAgent(context.Background(), agentID)
	if err != nil {
		t.Fatalf("ActiveForAgent a échoué : %v", err)
	}
	if active == nil || active.ID != sess.ID {
		t.Fatalf("ActiveForAgent aurait dû retrouver la session, obtenu : %+v", active)
	}

	if err := repo.MarkEnded(context.Background(), sess.ID, "test_done"); err != nil {
		t.Fatalf("MarkEnded a échoué : %v", err)
	}

	ended, err := repo.FindByID(context.Background(), tenantID, sess.ID)
	if err != nil {
		t.Fatalf("FindByID a échoué : %v", err)
	}
	if ended == nil || ended.Status != rdDomain.StatusEnded || ended.EndedReason == nil || *ended.EndedReason != "test_done" {
		t.Fatalf("session terminée inattendue : %+v", ended)
	}

	// Une fois terminée, ActiveForAgent ne la retrouve plus.
	activeAfterEnd, err := repo.ActiveForAgent(context.Background(), agentID)
	if err != nil {
		t.Fatalf("ActiveForAgent a échoué : %v", err)
	}
	if activeAfterEnd != nil {
		t.Fatalf("ActiveForAgent n'aurait plus dû retrouver de session, obtenu : %+v", activeAfterEnd)
	}

	// MarkEnded est idempotent : un second appel avec une raison différente
	// ne doit rien changer.
	if err := repo.MarkEnded(context.Background(), sess.ID, "different_reason"); err != nil {
		t.Fatalf("second MarkEnded a échoué : %v", err)
	}
	stillEnded, _ := repo.FindByID(context.Background(), tenantID, sess.ID)
	if stillEnded.EndedReason == nil || *stillEnded.EndedReason != "test_done" {
		t.Fatalf("MarkEnded n'est pas idempotent : %+v", stillEnded)
	}
}

func TestRemoteDesktopRepo_ConsumeToken_Expired_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	repo := NewRemoteDesktopRepo(pool)
	tenantID := testutil.SeedTenant(t, pool, "RD Tenant Expiry", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "rd-agent-2")

	sess := &rdDomain.Session{TenantID: tenantID, AgentID: agentID, Mode: rdDomain.ModeView}
	viewerToken, _, err := repo.Create(context.Background(), sess, -time.Second) // déjà expiré
	if err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}

	got, err := repo.ConsumeViewerToken(context.Background(), viewerToken)
	if err != nil {
		t.Fatalf("ConsumeViewerToken a échoué : %v", err)
	}
	if got != nil {
		t.Fatalf("un jeton expiré n'aurait pas dû être consommable, obtenu : %+v", got)
	}
}

// TestRemoteDesktopRepo_ActiveForAgent_IgnoresExpiredPending couvre le cas
// d'une session dont le jeton agent n'est jamais consommé (ex : l'agent
// avait déjà une session active au moment de REMOTE_DESKTOP_START — voir
// leo_rd_start côté C — auquel cas la tentative n'atteint jamais le relais
// backend, rien ne la marque explicitement "ended"). Passé son expires_at,
// elle ne doit plus bloquer la création d'une nouvelle session sur cet agent.
func TestRemoteDesktopRepo_ActiveForAgent_IgnoresExpiredPending(t *testing.T) {
	pool := testutil.TestDB(t)
	repo := NewRemoteDesktopRepo(pool)
	tenantID := testutil.SeedTenant(t, pool, "RD Tenant Pending Expiry", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "rd-agent-3")

	sess := &rdDomain.Session{TenantID: tenantID, AgentID: agentID, Mode: rdDomain.ModeView}
	_, _, err := repo.Create(context.Background(), sess, time.Minute)
	if err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}

	// Toujours "pending" et pas encore expirée : compte comme active.
	active, err := repo.ActiveForAgent(context.Background(), agentID)
	if err != nil {
		t.Fatalf("ActiveForAgent a échoué : %v", err)
	}
	if active == nil || active.ID != sess.ID {
		t.Fatalf("une session pending non expirée aurait dû compter comme active, obtenu : %+v", active)
	}

	if _, err := pool.Exec(context.Background(),
		`UPDATE remote_desktop_sessions SET expires_at = NOW() - interval '1 second' WHERE id = $1`, sess.ID,
	); err != nil {
		t.Fatalf("mise à jour d'expires_at échouée : %v", err)
	}

	active, err = repo.ActiveForAgent(context.Background(), agentID)
	if err != nil {
		t.Fatalf("ActiveForAgent a échoué : %v", err)
	}
	if active != nil {
		t.Fatalf("une session pending expirée n'aurait plus dû compter comme active, obtenu : %+v", active)
	}
}
