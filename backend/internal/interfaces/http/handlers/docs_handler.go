package handlers

import (
	"html/template"
	"net/http"
	"os"
	"time"
)

// DocsHandler sert la spécification OpenAPI et une page Swagger UI
// interactive — dev-only par défaut (voir Dependencies.EnableAPIDocs dans
// router.go, jamais actif en production sauf activation explicite).
type DocsHandler struct {
	specPath string // chemin vers docs/openapi.yaml
}

// NewDocsHandler crée un DocsHandler pointant vers le fichier OpenAPI donné.
func NewDocsHandler(specPath string) *DocsHandler {
	return &DocsHandler{specPath: specPath}
}

// Spec sert le fichier openapi.yaml tel quel.
//
//	GET /docs/openapi.yaml
func (h *DocsHandler) Spec(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(h.specPath)
	if err != nil {
		http.Error(w, "spécification OpenAPI introuvable sur le serveur", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	http.ServeContent(w, r, "openapi.yaml", modTime(h.specPath), f)
}

// modTime retourne la date de modification du fichier, ou la valeur zéro si
// indisponible — http.ServeContent l'accepte pour la négociation If-Modified-Since,
// mais s'en passe très bien (zéro = toujours re-servi).
func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// uiTemplate charge Swagger UI depuis un CDN (jsdelivr) — page dev-only,
// jamais servie en production par défaut : pas de contrainte "self-hosted"
// à respecter ici, contrairement au reste de la stack applicative.
var uiTemplate = template.Must(template.New("docs").Parse(`<!DOCTYPE html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <title>Leo-One API — Documentation</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: {{.SpecURL}},
        dom_id: "#swagger-ui",
        presets: [SwaggerUIBundle.presets.apis],
      });
    };
  </script>
</body>
</html>`))

// UI sert une page Swagger UI interactive, pointée sur Spec.
//
//	GET /docs
func (h *DocsHandler) UI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = uiTemplate.Execute(w, map[string]string{"SpecURL": "/docs/openapi.yaml"})
}
