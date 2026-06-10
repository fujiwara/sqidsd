package sqidsd

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startServer(t *testing.T, opts *Options) (*Server, context.CancelFunc, chan error) {
	t.Helper()
	s := newTestServer(t, opts)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(ctx) }()
	want := len(s.opts.Addresses)
	for range 100 {
		if len(s.Addrs()) >= want {
			return s, cancel, errCh
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start listening")
	return nil, nil, nil
}

func dialServer(t *testing.T, s *Server, network string) net.Conn {
	t.Helper()
	for _, a := range s.Addrs() {
		if a.Network() == network {
			conn, err := net.Dial(network, a.String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			return conn
		}
	}
	t.Fatalf("no %s listener found", network)
	return nil
}

// request writes the request lines at once and reads wantResponses response
// lines. wantResponses may be less than len(lines) when notifications are
// included.
func request(t *testing.T, conn net.Conn, wantResponses int, lines ...string) []string {
	t.Helper()
	if _, err := conn.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	resps := make([]string, 0, wantResponses)
	for range wantResponses {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		resps = append(resps, strings.TrimSpace(line))
	}
	return resps
}

func TestDetectNetwork(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:8089", "tcp"},
		{":8089", "tcp"},
		{"localhost:8089", "tcp"},
		{"[::1]:8089", "tcp"},
		{"/tmp/sqidsd.sock", "unix"},
		{"./sqidsd.sock", "unix"},
		{"sqidsd.sock", "unix"},
		{"/tmp/with:colon.sock", "unix"},
	}
	for _, tt := range tests {
		if got := DetectNetwork(tt.addr); got != tt.want {
			t.Errorf("DetectNetwork(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

func TestServeTCP(t *testing.T) {
	s, _, _ := startServer(t, &Options{Addresses: []string{"127.0.0.1:0"}})
	conn := dialServer(t, s, "tcp")
	resps := request(t, conn, 1, `{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}`)
	if want := `{"jsonrpc":"2.0","result":"86Rf07","id":1}`; resps[0] != want {
		t.Errorf("got %s, want %s", resps[0], want)
	}
}

func TestServeUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqidsd.sock")
	s, _, _ := startServer(t, &Options{Addresses: []string{path}})
	conn := dialServer(t, s, "unix")
	resps := request(t, conn, 1, `{"jsonrpc":"2.0","id":1,"method":"decode","params":["86Rf07"]}`)
	if want := `{"jsonrpc":"2.0","result":[1,2,3],"id":1}`; resps[0] != want {
		t.Errorf("got %s, want %s", resps[0], want)
	}
}

func TestServeBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqidsd.sock")
	s, _, _ := startServer(t, &Options{Addresses: []string{path, "127.0.0.1:0"}})
	for _, network := range []string{"unix", "tcp"} {
		conn := dialServer(t, s, network)
		resps := request(t, conn, 1, `{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}`)
		if want := `{"jsonrpc":"2.0","result":"86Rf07","id":1}`; resps[0] != want {
			t.Errorf("%s: got %s, want %s", network, resps[0], want)
		}
	}
}

func TestPipelining(t *testing.T) {
	s, _, _ := startServer(t, &Options{Addresses: []string{"127.0.0.1:0"}})
	conn := dialServer(t, s, "tcp")
	resps := request(t, conn, 3,
		`{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}`,
		`{"jsonrpc":"2.0","method":"encode","params":[4,5,6]}`, // notification: no response
		`{"jsonrpc":"2.0","id":2,"method":"decode","params":["86Rf07"]}`,
		`{"jsonrpc":"2.0","id":3,"method":"encode","params":[0]}`,
	)
	// 4 requests, 3 responses (the notification gets none), in request order.
	wants := []string{
		`{"jsonrpc":"2.0","result":"86Rf07","id":1}`,
		`{"jsonrpc":"2.0","result":[1,2,3],"id":2}`,
		`{"jsonrpc":"2.0","result":"bM","id":3}`,
	}
	for i, want := range wants {
		if resps[i] != want {
			t.Errorf("response %d: got %s, want %s", i, resps[i], want)
		}
	}
}

func TestGracefulShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqidsd.sock")
	s, cancel, errCh := startServer(t, &Options{Addresses: []string{path}, ShutdownTimeout: time.Second})
	conn := dialServer(t, s, "unix")
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned an error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket file %s still exists after shutdown", path)
	}
	// The open connection has been closed by the server.
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := bufio.NewReader(conn).ReadString('\n'); err == nil {
		t.Error("expected the connection to be closed")
	}
}

func TestStaleSocketFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqidsd.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket file was not left: %v", err)
	}
	s, _, _ := startServer(t, &Options{Addresses: []string{path}})
	conn := dialServer(t, s, "unix")
	resps := request(t, conn, 1, `{"jsonrpc":"2.0","id":1,"method":"encode","params":[1,2,3]}`)
	if want := `{"jsonrpc":"2.0","result":"86Rf07","id":1}`; resps[0] != want {
		t.Errorf("got %s, want %s", resps[0], want)
	}
}

func TestSocketAlreadyInUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqidsd.sock")
	startServer(t, &Options{Addresses: []string{path}})
	s2, err := New(&Options{Addresses: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Run(context.Background()); err == nil {
		t.Error("Run must fail when the socket is in use by another listener")
	}
}

func TestSocketPathIsNotASocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(&Options{Addresses: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background()); err == nil {
		t.Error("Run must fail when the socket path is a regular file")
	}
}

func TestLineTooLong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqidsd.sock")
	s, _, _ := startServer(t, &Options{Addresses: []string{path}})
	conn := dialServer(t, s, "unix")
	if _, err := conn.Write([]byte(strings.Repeat("a", maxLineSize+10) + "\n")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, `"code":-32700`) {
		t.Errorf("expected a parse error response, got %s", line)
	}
	if _, err := br.ReadString('\n'); err == nil {
		t.Error("expected the connection to be closed")
	}
}
