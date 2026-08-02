package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAlertHandler_AllMethodsReturn501(t *testing.T) {
	h := NewAlertHandler()

	methods := map[string]func(http.ResponseWriter, *http.Request){
		"List":        h.List,
		"Get":         h.Get,
		"Acknowledge": h.Acknowledge,
		"ListRules":   h.ListRules,
		"CreateRule":  h.CreateRule,
		"UpdateRule":  h.UpdateRule,
		"DeleteRule":  h.DeleteRule,
	}

	for name, fn := range methods {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
			rec := httptest.NewRecorder()

			fn(rec, req)

			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotImplemented)
			}
		})
	}
}
