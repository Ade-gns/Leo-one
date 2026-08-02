package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStubHandler_AllMethodsReturn501(t *testing.T) {
	h := &StubHandler{}

	methods := map[string]func(http.ResponseWriter, *http.Request){
		"List":            h.List,
		"Get":             h.Get,
		"Create":          h.Create,
		"Update":          h.Update,
		"Delete":          h.Delete,
		"Acknowledge":     h.Acknowledge,
		"CreateRule":      h.CreateRule,
		"UpdateRule":      h.UpdateRule,
		"DeleteRule":      h.DeleteRule,
		"ListRules":       h.ListRules,
		"AddComment":      h.AddComment,
		"Hardware":        h.Hardware,
		"Software":        h.Software,
		"MFAEnable":       h.MFAEnable,
		"MFAConfirm":      h.MFAConfirm,
		"MFADisable":      h.MFADisable,
		"ListPermissions": h.ListPermissions,
		"Summary":         h.Summary,
	}

	for name, fn := range methods {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
			rec := httptest.NewRecorder()

			fn(rec, req)

			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotImplemented)
			}
			env := decodeEnvelope(t, rec.Body)
			errObj, ok := env["error"].(map[string]any)
			if !ok {
				t.Fatalf("error absent ou invalide: %v", env)
			}
			if errObj["code"] != "NOT_IMPLEMENTED" {
				t.Errorf("code d'erreur = %v, attendu NOT_IMPLEMENTED", errObj["code"])
			}
		})
	}
}
