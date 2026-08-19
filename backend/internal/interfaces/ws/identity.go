package ws

import (
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/pkg/pki"
)

// extractIdentityFromCert extrait l'agent_id (CN) et le tenant_id (OU[0])
// du certificat client présenté dans le handshake mTLS, et vérifie qu'il
// n'a pas été révoqué. Partagé par AgentWSHandler (canal de contrôle,
// /ws/agent) et RemoteDesktopWSHandler (connexion dédiée bureau à distance,
// /ws/remote-desktop) — même confiance mTLS, même convention de nommage des
// certificats, tous deux sur le listener mTLS :8081 (voir cmd/server/main.go).
//
// Convention de nommage des certificats émis par le CA interne :
//
//	CN  = agent_id (UUID v4)
//	OU  = tenant_id (UUID v4)
//	O   = "leo-one"
func extractIdentityFromCert(r *http.Request, pool *pgxpool.Pool, logger *slog.Logger) (agentID, tenantID string, err error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		// En développement sans mTLS, on accepte les headers X-Agent-ID / X-Tenant-ID.
		// JAMAIS en production.
		agentID = r.Header.Get("X-Agent-ID")
		tenantID = r.Header.Get("X-Tenant-ID")
		if agentID != "" && tenantID != "" {
			logger.Warn("Authentification par header (mode dev) — désactiver en production")
			return agentID, tenantID, nil
		}
		return "", "", errorf("pas de certificat client TLS présenté")
	}

	cert := r.TLS.PeerCertificates[0]
	agentID, tenantID, err = parseCert(cert)
	if err != nil {
		return "", "", err
	}
	if err := checkNotRevoked(r.Context(), pool, cert); err != nil {
		return "", "", err
	}
	return agentID, tenantID, nil
}

// checkNotRevoked vérifie que le certificat présenté correspond bien à un
// certificat émis à l'enrollment (agent_certificates.thumbprint) et non
// révoqué depuis. Une signature CA valide ne suffit pas : c'est ce lookup
// qui permet de couper l'accès d'un agent décommissionné ou compromis avant
// l'expiration de son certificat (5 ans — voir pki.agentValidity).
func checkNotRevoked(ctx context.Context, pool *pgxpool.Pool, cert *x509.Certificate) error {
	thumbprint := pki.Fingerprint(cert)

	var revokedAt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT revoked_at FROM agent_certificates WHERE thumbprint = $1
	`, thumbprint).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return errorf("certificat inconnu (non émis par l'enrollment)")
	}
	if err != nil {
		return err
	}
	if revokedAt != nil {
		return errorf("certificat révoqué")
	}
	return nil
}

func parseCert(cert *x509.Certificate) (agentID, tenantID string, err error) {
	agentID = cert.Subject.CommonName
	if agentID == "" {
		return "", "", errorf("CN vide dans le certificat client")
	}

	if len(cert.Subject.OrganizationalUnit) == 0 {
		return "", "", errorf("OU manquant dans le certificat client")
	}
	tenantID = cert.Subject.OrganizationalUnit[0]

	return agentID, tenantID, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func errorf(msg string) error {
	return &wsError{msg: msg}
}

type wsError struct{ msg string }

func (e *wsError) Error() string { return e.msg }
