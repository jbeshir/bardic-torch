package wayback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAvailable(t *testing.T) {
	tests := map[string]struct{ answer, want string }{
		"found": {`{"archived_snapshots":{"closest":{"available":true,"status":"200",` +
			`"url":"http://web.archive.org/web/2020/https://a.example/"}}}`, "https://web.archive.org/web/2020/https://a.example/"},
		"none":   {`{"archived_snapshots":{}}`, ""},
		"status": {`{"archived_snapshots":{"closest":{"available":true,"status":"404","url":"http://web.archive.org/x"}}}`, ""},
		"other host": {`{"archived_snapshots":{"closest":{"available":true,"status":"200",` +
			`"url":"https://evil.example/web/2020/x"}}}`, ""},
	}
	for name, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "https://a.example/", r.URL.Query().Get("url"))
			_, _ = w.Write([]byte(tt.answer))
		}))
		c := New("test")
		c.baseURL = srv.URL
		got, err := c.Available(context.Background(), "https://a.example/")
		srv.Close()
		require.NoError(t, err, name)
		assert.Equal(t, tt.want, got, name)
	}
}
