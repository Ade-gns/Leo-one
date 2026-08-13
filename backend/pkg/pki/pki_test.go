package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func certKeyPaths(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

func TestEnsureCA_GeneratesNewCA(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)

	ca, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	if !ca.Cert.IsCA {
		t.Error("le certificat généré devrait avoir IsCA=true")
	}
	if ca.Cert.Subject.CommonName != "Leo-One Internal CA" {
		t.Errorf("CommonName = %q, attendu \"Leo-One Internal CA\"", ca.Cert.Subject.CommonName)
	}
	if time.Until(ca.Cert.NotAfter) < 9*365*24*time.Hour {
		t.Error("la CA générée devrait être valide au moins ~10 ans")
	}

	// Fichiers écrits sur disque
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("le certificat CA n'a pas été écrit : %v", err)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("la clé CA n'a pas été écrite : %v", err)
	}
	// La clé privée doit être en 0600 (lecture propriétaire uniquement).
	if perm := keyInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions clé CA = %o, attendu 0600", perm)
	}
}

func TestEnsureCA_LoadsExistingCA(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)

	first, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("premier EnsureCA a échoué : %v", err)
	}

	second, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("second EnsureCA (rechargement) a échoué : %v", err)
	}

	if !first.Cert.Equal(second.Cert) {
		t.Error("EnsureCA appelé deux fois devrait recharger la même CA, pas en générer une nouvelle")
	}
	if first.Cert.SerialNumber.Cmp(second.Cert.SerialNumber) != 0 {
		t.Error("le numéro de série devrait être identique après rechargement")
	}
}

func TestCertPEM_DecodesBackToSameCert(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)
	ca, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	block, rest := pem.Decode(ca.CertPEM())
	if block == nil {
		t.Fatal("CertPEM() ne produit pas un bloc PEM valide")
	}
	if len(rest) != 0 {
		t.Errorf("il reste %d octets après le bloc PEM, attendu 0", len(rest))
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("type de bloc PEM = %q, attendu CERTIFICATE", block.Type)
	}

	decoded, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("le certificat décodé depuis CertPEM() ne parse pas : %v", err)
	}
	if !decoded.Equal(ca.Cert) {
		t.Error("le certificat décodé depuis CertPEM() devrait être identique à ca.Cert")
	}
}

func TestEnsureCA_MissingFilesGeneratesFresh(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)
	// Ni certPath ni keyPath n'existent encore (chemin d'erreur de loadCA
	// exercé en amont, avant le repli sur la génération).
	if _, err := EnsureCA(certPath, keyPath); err != nil {
		t.Fatalf("EnsureCA devrait générer une CA fraîche quand les fichiers sont absents : %v", err)
	}
}

func TestEnsureCA_CorruptedCertFallsBackToError(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)

	// Une CA valide existe d'abord...
	if _, err := EnsureCA(certPath, keyPath); err != nil {
		t.Fatalf("EnsureCA initial a échoué : %v", err)
	}
	// ...puis le certificat est corrompu sur disque : loadCA doit échouer et
	// EnsureCA doit alors régénérer une nouvelle CA plutôt que de planter.
	if err := os.WriteFile(certPath, []byte("not a valid pem"), 0o644); err != nil {
		t.Fatalf("écriture du certificat corrompu a échoué : %v", err)
	}

	ca, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("EnsureCA devrait régénérer après un certificat corrompu, pas échouer : %v", err)
	}
	if ca == nil || ca.Cert == nil {
		t.Fatal("EnsureCA devrait retourner une CA valide après régénération")
	}
}

func TestEnsureServerCert_RegeneratesExpiredCert(t *testing.T) {
	caCertPath, caKeyPath := certKeyPaths(t)
	ca, err := EnsureCA(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	srvCertPath, srvKeyPath := certKeyPaths(t)

	// Fabrique à la main un certificat serveur déjà expiré, signé par la CA,
	// pour vérifier qu'EnsureServerCert le détecte et en régénère un neuf
	// plutôt que de recharger silencieusement un certificat périmé.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("génération clé a échoué : %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: mustSerial(t),
		Subject:      pkix.Name{CommonName: "expired-test-server"},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     time.Now().Add(-24 * time.Hour), // déjà expiré
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		t.Fatalf("création du certificat expiré a échoué : %v", err)
	}
	if err := os.WriteFile(srvCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("écriture du certificat expiré a échoué : %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal clé a échoué : %v", err)
	}
	if err := os.WriteFile(srvKeyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("écriture de la clé a échoué : %v", err)
	}

	fresh, err := EnsureServerCert(ca, srvCertPath, srvKeyPath, []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("EnsureServerCert a échoué : %v", err)
	}
	if time.Now().After(fresh.Leaf.NotAfter) {
		t.Error("le certificat régénéré ne devrait pas être expiré")
	}
	if fresh.Leaf.Subject.CommonName != "Leo-One WSS Server" {
		t.Errorf("CN du certificat régénéré = %q, attendu \"Leo-One WSS Server\" (pas l'ancien certificat expiré)",
			fresh.Leaf.Subject.CommonName)
	}
}

func mustSerial(t *testing.T) *big.Int {
	t.Helper()
	s, err := randomSerial()
	if err != nil {
		t.Fatalf("randomSerial a échoué : %v", err)
	}
	return s
}

func TestEnsureServerCert_SignedByCA(t *testing.T) {
	caCertPath, caKeyPath := certKeyPaths(t)
	ca, err := EnsureCA(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	srvCertPath, srvKeyPath := certKeyPaths(t)
	tlsCert, err := EnsureServerCert(ca, srvCertPath, srvKeyPath, []string{"127.0.0.1", "rmm.example.com"})
	if err != nil {
		t.Fatalf("EnsureServerCert a échoué : %v", err)
	}

	if tlsCert.Leaf == nil {
		t.Fatal("tlsCert.Leaf ne devrait pas être nil")
	}

	// Vérifie que le certificat serveur est bien signé par la CA.
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := tlsCert.Leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("le certificat serveur ne vérifie pas contre la CA : %v", err)
	}

	// SANs : IP et DNS doivent être répartis correctement.
	foundIP := false
	for _, ip := range tlsCert.Leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			foundIP = true
		}
	}
	if !foundIP {
		t.Errorf("SAN IP 127.0.0.1 manquant, IPAddresses=%v", tlsCert.Leaf.IPAddresses)
	}
	foundDNS := false
	for _, dns := range tlsCert.Leaf.DNSNames {
		if dns == "rmm.example.com" {
			foundDNS = true
		}
	}
	if !foundDNS {
		t.Errorf("SAN DNS rmm.example.com manquant, DNSNames=%v", tlsCert.Leaf.DNSNames)
	}
}

func TestEnsureServerCert_ReloadsValidCert(t *testing.T) {
	caCertPath, caKeyPath := certKeyPaths(t)
	ca, err := EnsureCA(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	srvCertPath, srvKeyPath := certKeyPaths(t)
	first, err := EnsureServerCert(ca, srvCertPath, srvKeyPath, []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("premier EnsureServerCert a échoué : %v", err)
	}

	second, err := EnsureServerCert(ca, srvCertPath, srvKeyPath, []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("second EnsureServerCert (rechargement) a échoué : %v", err)
	}

	if !first.Leaf.Equal(second.Leaf) {
		t.Error("EnsureServerCert appelé deux fois avec un cert valide devrait recharger, pas régénérer")
	}
}

func TestFingerprint_IsStable64CharHex(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)
	ca, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	fp1 := Fingerprint(ca.Cert)
	fp2 := Fingerprint(ca.Cert)

	if fp1 != fp2 {
		t.Error("Fingerprint devrait être déterministe pour un même certificat")
	}
	if len(fp1) != 64 {
		t.Errorf("longueur de l'empreinte = %d, attendu 64 (SHA-256 hex)", len(fp1))
	}
	for _, c := range fp1 {
		isHexLower := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHexLower {
			t.Errorf("empreinte %q contient un caractère non hex-minuscule : %q", fp1, c)
			break
		}
	}
}

func TestIssueAgentCert_CNAndOUMatchIdentity(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)
	ca, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	const agentID = "6b2f50f8-cfdf-4bbf-894f-0c5ce81d24aa"
	const tenantID = "ffffd43f-a4f5-4bb2-832d-a672ab7f9eb8"

	certPEM, keyPEM, cert, err := IssueAgentCert(ca, agentID, tenantID)
	if err != nil {
		t.Fatalf("IssueAgentCert a échoué : %v", err)
	}
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		t.Fatal("certPEM/keyPEM ne devraient pas être vides")
	}

	if cert.Subject.CommonName != agentID {
		t.Errorf("CN = %q, attendu %q", cert.Subject.CommonName, agentID)
	}
	if len(cert.Subject.OrganizationalUnit) != 1 || cert.Subject.OrganizationalUnit[0] != tenantID {
		t.Errorf("OU = %v, attendu [%q]", cert.Subject.OrganizationalUnit, tenantID)
	}

	found := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageClientAuth {
			found = true
		}
	}
	if !found {
		t.Errorf("ExtKeyUsage devrait inclure ClientAuth, obtenu %v", cert.ExtKeyUsage)
	}

	// Vérifie que le certificat agent est bien signé par la CA (c'est ce que
	// AgentWSHandler.ConfigureMTLS vérifie côté serveur au handshake mTLS).
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Errorf("le certificat agent ne vérifie pas contre la CA : %v", err)
	}
}

func TestIssueAgentCert_DifferentAgentsGetDifferentSerials(t *testing.T) {
	certPath, keyPath := certKeyPaths(t)
	ca, err := EnsureCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("EnsureCA a échoué : %v", err)
	}

	_, _, cert1, err := IssueAgentCert(ca, "agent-1", "tenant-1")
	if err != nil {
		t.Fatalf("IssueAgentCert (1) a échoué : %v", err)
	}
	_, _, cert2, err := IssueAgentCert(ca, "agent-2", "tenant-1")
	if err != nil {
		t.Fatalf("IssueAgentCert (2) a échoué : %v", err)
	}

	if cert1.SerialNumber.Cmp(cert2.SerialNumber) == 0 {
		t.Error("deux certificats émis séparément ne devraient jamais partager le même numéro de série")
	}
	if Fingerprint(cert1) == Fingerprint(cert2) {
		t.Error("deux certificats différents ne devraient pas avoir la même empreinte")
	}
}
