// Package pki gère la CA interne qui authentifie les agents en mTLS :
// génération/chargement de la CA, du certificat serveur du listener WSS,
// et signature des certificats clients à l'enrollment.
//
// L'agent C ne valide pas la chaîne de confiance du certificat serveur — il
// épingle son empreinte SHA-256 (voir leo_crypto_x509_fingerprint_matches et
// ca_fingerprint dans agent/include/leo_agent.h). Un certificat serveur signé
// par cette CA interne convient donc très bien, une chaîne publique n'est pas
// nécessaire. Le certificat client de chaque agent, en revanche, est vérifié
// normalement par Go (tls.RequireAndVerifyClientCert côté serveur) : CN=agent
// ID, OU=tenantID, lus par AgentWSHandler.parseCert pour authentifier la
// connexion WSS.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	caValidity     = 10 * 365 * 24 * time.Hour
	serverValidity = 2 * 365 * 24 * time.Hour
	agentValidity  = 5 * 365 * 24 * time.Hour
)

// CA regroupe le certificat et la clé privée de la CA interne.
type CA struct {
	Cert *x509.Certificate
	Key  *ecdsa.PrivateKey
}

// CertPEM retourne le certificat CA encodé en PEM.
func (ca *CA) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Cert.Raw})
}

// EnsureCA charge la CA depuis certPath/keyPath si présente, sinon en génère
// une nouvelle (10 ans de validité, ECDSA P-256) et l'écrit sur disque
// (clé privée en 0600).
func EnsureCA(certPath, keyPath string) (*CA, error) {
	if ca, err := loadCA(certPath, keyPath); err == nil {
		return ca, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("génération clé CA : %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Leo-One Internal CA", Organization: []string{"Leo-One RMM"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("création certificat CA : %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parsing certificat CA généré : %w", err)
	}

	if err := writeDERPEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal clé CA : %w", err)
	}
	if err := writeDERPEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return nil, err
	}

	return &CA{Cert: cert, Key: key}, nil
}

func loadCA(certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("PEM certificat CA invalide")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("PEM clé CA invalide")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	return &CA{Cert: cert, Key: key}, nil
}

// EnsureServerCert charge le certificat du listener WSS depuis certPath/
// keyPath s'il est présent et encore valide, sinon en génère un nouveau signé
// par la CA pour les SAN fournis (hostnames et/ou IPs).
func EnsureServerCert(ca *CA, certPath, keyPath string, sans []string) (tls.Certificate, error) {
	if tlsCert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, err2 := x509.ParseCertificate(tlsCert.Certificate[0]); err2 == nil && time.Now().Before(leaf.NotAfter) {
			tlsCert.Leaf = leaf
			return tlsCert, nil
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("génération clé serveur : %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Leo-One WSS Server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(serverValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, san := range sans {
		if ip := net.ParseIP(san); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if san != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, san)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("création certificat serveur : %w", err)
	}

	if err := writeDERPEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal clé serveur : %w", err)
	}
	if err := writeDERPEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600); err != nil {
		return tls.Certificate{}, err
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

// Fingerprint retourne l'empreinte SHA-256 (hex minuscule, 64 caractères) du
// certificat — format attendu par ca_fingerprint côté agent.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("%x", sum)
}

// IssueAgentCert génère une paire de clés et un certificat client signé par
// la CA pour un agent : CN=agentID, OU=tenantID. Le certificat retourné (en
// plus du PEM) permet à l'appelant d'enregistrer son numéro de série et son
// empreinte dans agent_certificates, pour l'audit et la révocation — voir
// AgentHandler.Enroll et AgentWSHandler.checkNotRevoked.
func IssueAgentCert(ca *CA, agentID, tenantID string) (certPEM, keyPEM []byte, cert *x509.Certificate, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("génération clé agent : %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         agentID,
			OrganizationalUnit: []string{tenantID},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(agentValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("création certificat agent : %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal clé agent : %w", err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parsing certificat agent généré : %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, cert, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func writeDERPEM(path, blockType string, der []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("création répertoire %s : %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("ouverture %s : %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}
