package mysql

import (
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDBName = "bardic_torch"

// testURL builds a database URL, keeping credentials out of string literals.
func testURL(user *url.Userinfo, host, path string) string {
	u := url.URL{Scheme: "mysql", User: user, Host: host, Path: path}
	return u.String()
}

func TestConfigFromURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		url        string
		wantUser   *url.Userinfo
		wantAddr   string
		wantDBName string
		wantErr    bool
	}{
		{
			name:       "dokku_form",
			url:        testURL(url.UserPassword("mysql", "0123abcd"), "dokku-mysql-bardic-torch-db:3306", "/bardic_torch_db"),
			wantUser:   url.UserPassword("mysql", "0123abcd"),
			wantAddr:   "dokku-mysql-bardic-torch-db:3306",
			wantDBName: "bardic_torch_db",
		},
		{
			name:       "special_chars_are_decoded",
			url:        testURL(url.UserPassword("bard user", "p@ss:w/rd?#%"), "db.example:3307", "/"+testDBName),
			wantUser:   url.UserPassword("bard user", "p@ss:w/rd?#%"),
			wantAddr:   "db.example:3307",
			wantDBName: testDBName,
		},
		{
			name:       "default_port",
			url:        "mysql://user@localhost/" + testDBName,
			wantUser:   url.User("user"),
			wantAddr:   "localhost:3306",
			wantDBName: testDBName,
		},
		{
			name:       "ipv6_host",
			url:        "mysql://user@[::1]/" + testDBName,
			wantUser:   url.User("user"),
			wantAddr:   "[::1]:3306",
			wantDBName: testDBName,
		},
		{name: "missing_db", url: "mysql://user@localhost:3306", wantErr: true},
		{name: "empty_db", url: "mysql://user@localhost:3306/", wantErr: true},
		{name: "nested_path", url: "mysql://user@localhost:3306/a/b", wantErr: true},
		{name: "wrong_scheme", url: "postgres://user@localhost:5432/" + testDBName, wantErr: true},
		{name: "driver_dsn", url: "user@tcp(localhost:3306)/" + testDBName, wantErr: true},
		{name: "missing_host", url: "mysql:///" + testDBName, wantErr: true},
		{name: "query_params", url: "mysql://user@localhost/" + testDBName + "?tls=true", wantErr: true},
		{name: "unparseable", url: "mysql://user@localhost:port/" + testDBName, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := ConfigFromURL(tc.url)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			wantPassword, _ := tc.wantUser.Password()
			assert.Equal(t, tc.wantUser.Username(), cfg.User)
			assert.Equal(t, wantPassword, cfg.Passwd)
			assert.Equal(t, "tcp", cfg.Net)
			assert.Equal(t, tc.wantAddr, cfg.Addr)
			assert.Equal(t, tc.wantDBName, cfg.DBName)
			assert.Equal(t, "utf8mb4_0900_ai_ci", cfg.Collation)
			assert.True(t, cfg.ParseTime)
			assert.Equal(t, time.UTC, cfg.Loc)
			assert.True(t, cfg.ClientFoundRows)
			assert.Equal(t, map[string]string{"time_zone": "'+00:00'", "max_execution_time": "2000"}, cfg.Params)
		})
	}
}

func TestConfigFromURL_ErrorsNeverQuoteThePassword(t *testing.T) {
	t.Parallel()

	const password = "S3cretPlaceholder"
	for _, raw := range []string{
		"mysql://admin:" + password + "@db host/" + testDBName,
		"mysql://admin:" + password + "@localhost:port/" + testDBName,
		"mysql://admin:" + password + "%zz@localhost/" + testDBName,
		"mysql://admin:ab/" + password + "@localhost/" + testDBName,
		"mysql://admin:" + password + "/x@localhost/" + testDBName,
	} {
		_, err := ConfigFromURL(raw)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), password[:6], "the error quotes no part of the URL")
	}
}

func TestConnect_UnreachableDatabase(t *testing.T) {
	t.Parallel()

	// A local listener that hangs up on every connection, so the driver's handshake fails. It stays open for the
	// whole test, so no other process can take its port and answer instead.
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	cfg, err := ConfigFromURL("mysql://user@" + listener.Addr().String() + "/" + testDBName)
	require.NoError(t, err)

	db, err := Connect(t.Context(), cfg)
	assert.Nil(t, db)
	assert.ErrorContains(t, err, "checking MySQL DB connection")
}
