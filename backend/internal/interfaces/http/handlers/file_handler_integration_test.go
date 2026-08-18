// Tests d'intégration FileHandler — nécessitent une base Postgres de test
// réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func buildMultipartUpload(t *testing.T, fieldName, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("CreateFormFile a échoué : %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("écriture du corps multipart a échoué : %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("fermeture du writer multipart a échoué : %v", err)
	}
	return body, mw.FormDataContentType()
}

func TestFileHandler_Create_And_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "FileH Create Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "file-create@example.com")
	fileRepo := postgres.NewFileRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewFileHandler(fileRepo, agentRepo, agentHandler, t.TempDir(), time.Hour, "http://backend:8080", 0, nil)

	content := []byte("#!/bin/bash\necho hello\n")
	body, contentType := buildMultipartUpload(t, "file", "install.sh", content)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["name"] != "install.sh" {
		t.Errorf("name = %v, attendu install.sh", data["name"])
	}
	if data["size_bytes"].(float64) != float64(len(content)) {
		t.Errorf("size_bytes = %v, attendu %d", data["size_bytes"], len(content))
	}
	// sha256sum de "#!/bin/bash\necho hello\n"
	wantSHA := "f590776b449af73e55cb368f45ce28400a19d0c68cdb34485fdfc0602b6c2437"
	if data["checksum_sha256"] != wantSHA {
		t.Errorf("checksum_sha256 = %v, attendu %s", data["checksum_sha256"], wantSHA)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	listReq = listReq.WithContext(httpctx.WithTenantID(listReq.Context(), tenantID))
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("List : code = %d, attendu %d", listRec.Code, http.StatusOK)
	}
	files := decodeEnvelope(t, listRec.Body)["data"].([]any)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, attendu 1", len(files))
	}
}

func TestFileHandler_Create_DuplicateName_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "FileH Dup Corp", 10)
	fileRepo := postgres.NewFileRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewFileHandler(fileRepo, agentRepo, agentHandler, t.TempDir(), time.Hour, "http://backend:8080", 0, nil)

	upload := func() int {
		body, ct := buildMultipartUpload(t, "file", "dup.bin", []byte("x"))
		req := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
		req.Header.Set("Content-Type", ct)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		return rec.Code
	}

	if code := upload(); code != http.StatusCreated {
		t.Fatalf("premier upload : code = %d, attendu %d", code, http.StatusCreated)
	}
	if code := upload(); code != http.StatusConflict {
		t.Fatalf("second upload (même nom) : code = %d, attendu %d", code, http.StatusConflict)
	}
}

func TestFileHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "FileH Delete Corp", 10)
	fileRepo := postgres.NewFileRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewFileHandler(fileRepo, agentRepo, agentHandler, t.TempDir(), time.Hour, "http://backend:8080", 0, nil)

	body, ct := buildMultipartUpload(t, "file", "todelete.bin", []byte("x"))
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	createReq.Header.Set("Content-Type", ct)
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	fileID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	f, err := fileRepo.FindByID(context.Background(), tenantID, fileID)
	if err != nil || f == nil {
		t.Fatalf("setup FindByID a échoué : %v", err)
	}
	if _, err := os.Stat(f.StoragePath); err != nil {
		t.Fatalf("le fichier devrait exister sur disque après upload : %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/files/"+fileID, nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = withURLParam(req, "fileID", fileID)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNoContent)
	}
	if _, err := os.Stat(f.StoragePath); !os.IsNotExist(err) {
		t.Errorf("le fichier devrait avoir été supprimé du disque")
	}
}

func TestFileHandler_DeployFile_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "FileH Deploy Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "file-deploy@example.com")
	agentID := testutil.SeedAgent(t, pool, tenantID, "deploy-01")
	fileRepo := postgres.NewFileRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewFileHandler(fileRepo, agentRepo, agentHandler, t.TempDir(), time.Hour, "http://backend:8080", 0, nil)

	body, ct := buildMultipartUpload(t, "file", "installer.msi", []byte("binary content"))
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	createReq.Header.Set("Content-Type", ct)
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createReq = createReq.WithContext(httpctx.WithUserID(createReq.Context(), userID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	fileID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	deployBody, _ := json.Marshal(map[string]any{"file_id": fileID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/deploy-file", bytes.NewReader(deployBody))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	req = withURLParam(req, "agentID", agentID)
	rec := httptest.NewRecorder()

	h.DeployFile(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	commandID, _ := data["command_id"].(string)
	if commandID == "" {
		t.Fatalf("command_id manquant : %v", data)
	}

	var cmdType, payload string
	err := pool.QueryRow(context.Background(),
		`SELECT type::text, payload::text FROM commands WHERE id = $1`, commandID).Scan(&cmdType, &payload)
	if err != nil {
		t.Fatalf("lecture de la commande créée a échoué : %v", err)
	}
	if cmdType != "file_transfer" {
		t.Errorf("type = %s, attendu file_transfer", cmdType)
	}

	var p struct {
		DownloadURL string `json:"download_url"`
		Filename    string `json:"filename"`
		SHA256      string `json:"sha256"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("décodage du payload a échoué : %v", err)
	}
	if p.Filename != "installer.msi" {
		t.Errorf("filename = %s, attendu installer.msi", p.Filename)
	}
	if p.DownloadURL == "" {
		t.Error("download_url vide")
	}
}

func TestFileHandler_Download_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "FileH Download Corp", 10)
	fileRepo := postgres.NewFileRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewFileHandler(fileRepo, agentRepo, agentHandler, t.TempDir(), time.Hour, "http://backend:8080", 0, nil)

	content := []byte("payload de test pour le telechargement")
	body, ct := buildMultipartUpload(t, "file", "dl.bin", content)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/files", body)
	createReq.Header.Set("Content-Type", ct)
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	fileID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	token, err := fileRepo.CreateDownloadToken(context.Background(), fileID, time.Hour)
	if err != nil {
		t.Fatalf("CreateDownloadToken a échoué : %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+fileID+"/download?token="+token, nil)
	req = withURLParam(req, "fileID", fileID)
	rec := httptest.NewRecorder()

	h.Download(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != string(content) {
		t.Errorf("contenu téléchargé inattendu : %q", rec.Body.String())
	}

	// Le token est à usage unique : un second téléchargement doit échouer.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+fileID+"/download?token="+token, nil)
	req2 = withURLParam(req2, "fileID", fileID)
	rec2 := httptest.NewRecorder()
	h.Download(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("second téléchargement : code = %d, attendu %d (token déjà utilisé)", rec2.Code, http.StatusUnauthorized)
	}
}

func TestFileHandler_Download_InvalidToken_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	fileRepo := postgres.NewFileRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewFileHandler(fileRepo, agentRepo, agentHandler, t.TempDir(), time.Hour, "http://backend:8080", 0, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/00000000-0000-0000-0000-000000000000/download?token=bogus", nil)
	req = withURLParam(req, "fileID", "00000000-0000-0000-0000-000000000000")
	rec := httptest.NewRecorder()

	h.Download(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusUnauthorized)
	}
}
