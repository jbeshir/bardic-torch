// Package refusingmysql runs a TCP server that turns away every MySQL connection as a MySQL server at
// max_connections does ("Error 1040: Too many connections"), so tests can check how the application answers a
// server that is up but will not take another connection.
package refusingmysql

import (
	"database/sql"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// ErConCount and Message are ER_CON_COUNT_ERROR and its message, the error a server at max_connections sends.
const (
	ErConCount = 1040
	Message    = "Too many connections"
)

// tooManyConnections is the MySQL protocol ERR packet the server sends in place of its handshake: the 3-byte
// little-endian payload length and sequence number 0, then the payload, 0xff, ErConCount (little-endian) and Message.
var tooManyConnections = append([]byte{byte(3 + len(Message)), 0, 0, 0, 0xff, ErConCount & 0xff, ErConCount >> 8},
	Message...)

// Listen starts a server on a free 127.0.0.1 port that answers every connection with tooManyConnections, and closes
// it; it returns the server's address. The server stops when the test finishes.
func Listen(t testing.TB) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err != nil {
				t.Errorf("refusingmysql: accepting: %v", err)
				return
			}
			// The client reads the packet and closes the connection; an error here is the client's going first.
			_, _ = conn.Write(tooManyConnections)
			_ = conn.Close()
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})

	return listener.Addr().String()
}

// DB returns a pool to a server Listen started, closed when the test finishes. Its connections time out after two
// seconds, so a test fails rather than hangs if the server stops answering.
func DB(t testing.TB) *sql.DB {
	t.Helper()

	cfg := mysqldriver.NewConfig()
	cfg.Net, cfg.Addr, cfg.DBName, cfg.Timeout = "tcp", Listen(t), "bardic_torch", 2*time.Second
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })

	return db
}
