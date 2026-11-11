package turnstile

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_VerifyTurnstile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer string
		want   bool
	}{
		{"accepted", `{"success":true,"hostname":"Bardictorch.org","action":"contact"}`, true},
		{"refused", `{"success":false,"error-codes":["invalid-input-response"]}`, false},
		{"other hostname", `{"success":true,"hostname":"evil.example","action":"contact"}`, false},
		{"other action", `{"success":true,"hostname":"bardictorch.org","action":"login"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "tok", r.FormValue("response"))
				_, _ = w.Write([]byte(tc.answer))
			}))
			defer srv.Close()

			c := &Client{Secret: "s", Hostname: "bardictorch.org", Endpoint: srv.URL, HTTP: srv.Client()}
			got, err := c.VerifyTurnstile(t.Context(), "tok")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
