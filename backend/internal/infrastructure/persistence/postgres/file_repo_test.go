// Tests d'intégration FileRepo — nécessitent une base Postgres de test
// réelle (voir internal/testutil.TestDB).
package postgres

import (
	"context"
	"testing"
	"time"

	fileDomain "github.com/yourorg/leo-one/internal/domain/file"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestFileRepo_Create_FindByID_List(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "File Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "file-owner@example.com")
	repo := NewFileRepo(pool)
	ctx := context.Background()

	f := &fileDomain.File{
		TenantID: tenantID, Name: "installer.msi", SizeBytes: 1024,
		ChecksumSHA256: "abc123", StoragePath: "/data/files/x", UploadedBy: &userID,
	}
	if err := repo.Create(ctx, f); err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}
	if f.ID == "" {
		t.Fatal("ID vide après Create")
	}

	found, err := repo.FindByID(ctx, tenantID, f.ID)
	if err != nil {
		t.Fatalf("FindByID a échoué : %v", err)
	}
	if found == nil || found.StoragePath != "/data/files/x" {
		t.Fatalf("FindByID inattendu : %+v", found)
	}

	list, err := repo.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("List a échoué : %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, attendu 1", len(list))
	}
}

func TestFileRepo_FindByID_IsolatedByTenant(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantA := testutil.SeedTenant(t, pool, "File Tenant A", 10)
	tenantB := testutil.SeedTenant(t, pool, "File Tenant B", 10)
	repo := NewFileRepo(pool)
	ctx := context.Background()

	f := &fileDomain.File{TenantID: tenantA, Name: "x.bin", SizeBytes: 1, ChecksumSHA256: "x", StoragePath: "/x"}
	if err := repo.Create(ctx, f); err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}

	found, err := repo.FindByID(ctx, tenantB, f.ID)
	if err != nil {
		t.Fatalf("FindByID a échoué : %v", err)
	}
	if found != nil {
		t.Errorf("un fichier d'un autre tenant ne devrait pas être trouvé, obtenu %+v", found)
	}
}

func TestFileRepo_Delete(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "File Delete Corp", 10)
	repo := NewFileRepo(pool)
	ctx := context.Background()

	f := &fileDomain.File{TenantID: tenantID, Name: "x.bin", SizeBytes: 1, ChecksumSHA256: "x", StoragePath: "/x"}
	if err := repo.Create(ctx, f); err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}

	if err := repo.Delete(ctx, tenantID, f.ID); err != nil {
		t.Fatalf("Delete a échoué : %v", err)
	}
	found, _ := repo.FindByID(ctx, tenantID, f.ID)
	if found != nil {
		t.Error("le fichier devrait être introuvable après Delete")
	}
}

func TestFileRepo_DownloadToken_ConsumeOnce(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "File Token Corp", 10)
	repo := NewFileRepo(pool)
	ctx := context.Background()

	f := &fileDomain.File{TenantID: tenantID, Name: "x.bin", SizeBytes: 1, ChecksumSHA256: "x", StoragePath: "/x"}
	if err := repo.Create(ctx, f); err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}

	token, err := repo.CreateDownloadToken(ctx, f.ID, time.Hour)
	if err != nil {
		t.Fatalf("CreateDownloadToken a échoué : %v", err)
	}
	if token == "" {
		t.Fatal("token vide")
	}

	consumed, err := repo.ConsumeDownloadToken(ctx, token)
	if err != nil {
		t.Fatalf("ConsumeDownloadToken (1) a échoué : %v", err)
	}
	if consumed == nil || consumed.ID != f.ID {
		t.Fatalf("ConsumeDownloadToken (1) inattendu : %+v", consumed)
	}

	// Deuxième consommation du même token : doit échouer (usage unique).
	consumed2, err := repo.ConsumeDownloadToken(ctx, token)
	if err != nil {
		t.Fatalf("ConsumeDownloadToken (2) a échoué : %v", err)
	}
	if consumed2 != nil {
		t.Errorf("un token déjà consommé ne devrait pas être réutilisable, obtenu %+v", consumed2)
	}
}

func TestFileRepo_DownloadToken_Expired(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "File Token Expired Corp", 10)
	repo := NewFileRepo(pool)
	ctx := context.Background()

	f := &fileDomain.File{TenantID: tenantID, Name: "x.bin", SizeBytes: 1, ChecksumSHA256: "x", StoragePath: "/x"}
	if err := repo.Create(ctx, f); err != nil {
		t.Fatalf("Create a échoué : %v", err)
	}

	token, err := repo.CreateDownloadToken(ctx, f.ID, -time.Minute) // déjà expiré
	if err != nil {
		t.Fatalf("CreateDownloadToken a échoué : %v", err)
	}

	consumed, err := repo.ConsumeDownloadToken(ctx, token)
	if err != nil {
		t.Fatalf("ConsumeDownloadToken a échoué : %v", err)
	}
	if consumed != nil {
		t.Errorf("un token expiré ne devrait pas être consommable, obtenu %+v", consumed)
	}
}

func TestFileRepo_DownloadToken_Unknown(t *testing.T) {
	pool := testutil.TestDB(t)
	repo := NewFileRepo(pool)

	consumed, err := repo.ConsumeDownloadToken(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("ConsumeDownloadToken a échoué : %v", err)
	}
	if consumed != nil {
		t.Errorf("un token inconnu ne devrait rien retourner, obtenu %+v", consumed)
	}
}
