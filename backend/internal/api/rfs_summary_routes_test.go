package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetRFSSummarySettingsValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid mode",
			body: `{"mode":"sometimes","interval_minutes":60}`,
			want: "mode harus salah satu",
		},
		{
			name: "interval below minimum",
			body: `{"mode":"interval","interval_minutes":4}`,
			want: "interval_minutes harus 5..1440",
		},
		{
			name: "interval above maximum",
			body: `{"mode":"interval","interval_minutes":1441}`,
			want: "interval_minutes harus 5..1440",
		},
		{
			name: "invalid json",
			body: `{"mode":`,
			want: "body JSON tidak valid",
		},
		{
			name: "unknown field",
			body: `{"mode":"off","extra":true}`,
			want: "body JSON tidak valid",
		},
	}

	// Every case must be rejected before the handler reaches the database. A
	// nil store therefore makes accidental persistence on invalid input panic.
	s := &Server{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/settings/rfs-summary", strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			s.handleSetRFSSummarySettings(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("body = %q, want substring %q", w.Body.String(), tc.want)
			}
		})
	}
}
