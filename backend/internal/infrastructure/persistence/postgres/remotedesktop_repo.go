package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	rdDomain "github.com/yourorg/leo-one/internal/domain/remotedesktop"
)

// RemoteDesktopRepo implémente remotedesktop.Repository via pgx/v5.
type RemoteDesktopRepo struct {
	pool *pgxpool.Pool
}

// NewRemoteDesktopRepo crée un RemoteDesktopRepo avec le pool de connexions fourni.
func NewRemoteDesktopRepo(pool *pgxpool.Pool) *RemoteDesktopRepo {
	return &RemoteDesktopRepo{pool: pool}
}

func newSessionToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

// Create insère une nouvelle session en statut pending et génère ses deux
// jetons d'appariement.
func (r *RemoteDesktopRepo) Create(ctx context.Context, s *rdDomain.Session, ttl time.Duration) (viewerToken, agentToken string, err error) {
	ctx = ensureCtx(ctx)

	viewerToken, viewerHash, err := newSessionToken()
	if err != nil {
		return "", "", err
	}
	agentToken, agentHash, err := newSessionToken()
	if err != nil {
		return "", "", err
	}

	err = r.pool.QueryRow(ctx, `
		INSERT INTO remote_desktop_sessions
			(tenant_id, agent_id, user_id, mode, status, viewer_token_hash, agent_token_hash, expires_at)
		VALUES ($1, $2, $3, $4::remote_desktop_mode, 'pending', $5, $6, NOW() + $7::interval)
		RETURNING id, status, expires_at, created_at
	`, s.TenantID, s.AgentID, s.UserID, s.Mode, viewerHash, agentHash, ttl.String(),
	).Scan(&s.ID, &s.Status, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return "", "", err
	}

	return viewerToken, agentToken, nil
}

const sessionColumns = `
	id, tenant_id, agent_id, user_id, mode::text, status::text, expires_at,
	agent_connected_at, viewer_connected_at, ended_at, ended_reason, created_at
`

func scanSession(row pgx.Row) (*rdDomain.Session, error) {
	var s rdDomain.Session
	err := row.Scan(
		&s.ID, &s.TenantID, &s.AgentID, &s.UserID, &s.Mode, &s.Status, &s.ExpiresAt,
		&s.AgentConnectedAt, &s.ViewerConnectedAt, &s.EndedAt, &s.EndedReason, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ConsumeViewerToken valide et consomme un jeton d'appariement côté
// navigateur. Une seule requête UPDATE ... WHERE ... RETURNING, atomique :
// deux tentatives concurrentes avec le même jeton ne peuvent pas toutes deux
// réussir (la seconde ne trouve plus de ligne à viewer_connected_at IS NULL).
func (r *RemoteDesktopRepo) ConsumeViewerToken(ctx context.Context, rawToken string) (*rdDomain.Session, error) {
	return r.consumeToken(ctx, rawToken, "viewer_token_hash", "viewer_connected_at")
}

// ConsumeAgentToken est le pendant côté agent de ConsumeViewerToken.
func (r *RemoteDesktopRepo) ConsumeAgentToken(ctx context.Context, rawToken string) (*rdDomain.Session, error) {
	return r.consumeToken(ctx, rawToken, "agent_token_hash", "agent_connected_at")
}

func (r *RemoteDesktopRepo) consumeToken(ctx context.Context, rawToken, hashColumn, connectedAtColumn string) (*rdDomain.Session, error) {
	ctx = ensureCtx(ctx)

	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])

	// otherConnectedAtColumn : dans un UPDATE, les expressions du SET (y
	// compris ce CASE) voient les valeurs AVANT la requête, jamais celle que
	// la requête est elle-même en train d'écrire — inutile donc de tester
	// connectedAtColumn (garanti non-NULL après cette requête puisqu'on
	// vient de l'y assigner NOW()) : seul l'état déjà committé de l'AUTRE
	// côté détermine si la session devient active ici.
	otherConnectedAtColumn := "agent_connected_at"
	if connectedAtColumn == "agent_connected_at" {
		otherConnectedAtColumn = "viewer_connected_at"
	}

	var sessionID string
	err := r.pool.QueryRow(ctx, `
		UPDATE remote_desktop_sessions
		SET `+connectedAtColumn+` = NOW(),
		    status = CASE WHEN status = 'pending' AND `+otherConnectedAtColumn+` IS NOT NULL
		                  THEN 'active'::remote_desktop_status
		                  ELSE status END
		WHERE `+hashColumn+` = $1 AND `+connectedAtColumn+` IS NULL
		  AND status <> 'ended' AND expires_at > NOW()
		RETURNING id
	`, tokenHash).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	row := r.pool.QueryRow(ctx, `SELECT `+sessionColumns+` FROM remote_desktop_sessions WHERE id = $1`, sessionID)
	return scanSession(row)
}

// FindByID retourne une session appartenant au tenant donné.
func (r *RemoteDesktopRepo) FindByID(ctx context.Context, tenantID, sessionID string) (*rdDomain.Session, error) {
	ctx = ensureCtx(ctx)
	row := r.pool.QueryRow(ctx, `
		SELECT `+sessionColumns+` FROM remote_desktop_sessions WHERE id = $1 AND tenant_id = $2
	`, sessionID, tenantID)
	return scanSession(row)
}

// ActiveForAgent retourne la session non terminée la plus récente pour cet
// agent — une session 'pending' dont la fenêtre d'appariement (expires_at)
// est dépassée ne compte plus comme active même si rien ne l'a explicitement
// marquée 'ended' : c'est le cas notamment d'un jeton agent jamais consommé
// parce que l'agent avait déjà une session en cours au moment de la
// commande REMOTE_DESKTOP_START (voir leo_rd_start côté C, "session déjà
// active") — le relais backend ne voit jamais passer cette tentative (elle
// n'atteint jamais AttachAgent), donc rien ne la termine explicitement ;
// sans ce garde-fou sur expires_at, une telle session resterait "active"
// pour toujours et bloquerait toute nouvelle session sur cet agent.
func (r *RemoteDesktopRepo) ActiveForAgent(ctx context.Context, agentID string) (*rdDomain.Session, error) {
	ctx = ensureCtx(ctx)
	row := r.pool.QueryRow(ctx, `
		SELECT `+sessionColumns+` FROM remote_desktop_sessions
		WHERE agent_id = $1 AND status <> 'ended' AND (status <> 'pending' OR expires_at > NOW())
		ORDER BY created_at DESC
		LIMIT 1
	`, agentID)
	return scanSession(row)
}

// MarkEnded transitionne une session vers ended. Idempotent : ne touche rien
// si la session est déjà ended (ended_at garde sa valeur d'origine).
func (r *RemoteDesktopRepo) MarkEnded(ctx context.Context, sessionID, reason string) error {
	ctx = ensureCtx(ctx)
	_, err := r.pool.Exec(ctx, `
		UPDATE remote_desktop_sessions
		SET status = 'ended', ended_at = NOW(), ended_reason = $2
		WHERE id = $1 AND status <> 'ended'
	`, sessionID, reason)
	return err
}

var _ rdDomain.Repository = (*RemoteDesktopRepo)(nil)
