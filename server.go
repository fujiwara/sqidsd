package sqidsd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sqids/sqids-go"
)

const (
	initialLineBufSize = 64 << 10 // 64KiB
	maxLineSize        = 1 << 20  // 1MiB
)

// DefaultShutdownTimeout is the default grace period to drain connections
// on shutdown.
const DefaultShutdownTimeout = 5 * time.Second

// Options configures a Server.
type Options struct {
	// Addresses are the addresses to listen on. A unix domain socket path
	// or a TCP address is detected automatically by DetectNetwork.
	Addresses []string
	// Alphabet is a custom Sqids alphabet. Empty means the Sqids default.
	Alphabet string
	// MinLength is the minimum length of generated Sqids IDs.
	MinLength uint8
	// ShutdownTimeout is the grace period to drain connections on shutdown.
	ShutdownTimeout time.Duration
}

// Server is a line-oriented JSON-RPC 2.0 server that encodes/decodes Sqids IDs.
type Server struct {
	sqids *sqids.Sqids
	opts  *Options

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	addrs []net.Addr
}

// New creates a Server. At least one address in opts.Addresses is required.
func New(opts *Options) (*Server, error) {
	if len(opts.Addresses) == 0 {
		return nil, errors.New("at least one listen address is required")
	}
	sq, err := sqids.New(sqids.Options{
		Alphabet:  opts.Alphabet,
		MinLength: opts.MinLength,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize sqids: %w", err)
	}
	return &Server{
		sqids: sq,
		opts:  opts,
		conns: make(map[net.Conn]struct{}),
	}, nil
}

// Addrs returns the addresses the server is listening on.
// It returns an empty slice until Run has set up the listeners.
func (s *Server) Addrs() []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	addrs := make([]net.Addr, len(s.addrs))
	copy(addrs, s.addrs)
	return addrs
}

// Run listens on the configured addresses and serves until ctx is canceled.
// It returns nil on a graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	var listeners []net.Listener
	closeListeners := func() {
		for _, l := range listeners {
			l.Close()
		}
	}
	for _, addr := range s.opts.Addresses {
		var l net.Listener
		var err error
		switch DetectNetwork(addr) {
		case "unix":
			l, err = listenUnix(addr)
		default:
			l, err = net.Listen("tcp", addr)
		}
		if err != nil {
			closeListeners()
			return err
		}
		listeners = append(listeners, l)
	}
	s.mu.Lock()
	for _, l := range listeners {
		slog.Info("listening", "addr", l.Addr())
		s.addrs = append(s.addrs, l.Addr())
	}
	s.mu.Unlock()

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-stop:
		}
		closeListeners()
	}()
	defer close(stop)

	var connWG, acceptWG sync.WaitGroup
	errCh := make(chan error, len(listeners))
	for _, l := range listeners {
		acceptWG.Add(1)
		go func(l net.Listener) {
			defer acceptWG.Done()
			for {
				conn, err := l.Accept()
				if err != nil {
					if !errors.Is(err, net.ErrClosed) {
						errCh <- fmt.Errorf("failed to accept on %s: %w", l.Addr(), err)
						closeListeners()
					}
					return
				}
				s.addConn(conn)
				connWG.Go(func() {
					defer s.removeConn(conn)
					s.handleConn(conn)
				})
			}
		}(l)
	}
	acceptWG.Wait()

	select {
	case err := <-errCh:
		s.closeConns(false)
		connWG.Wait()
		return err
	default:
	}

	// Graceful shutdown: stop reading new requests but let in-flight
	// (already buffered) requests be answered.
	slog.Info("shutting down")
	s.closeConns(true)
	done := make(chan struct{})
	go func() {
		connWG.Wait()
		close(done)
	}()
	timeout := s.opts.ShutdownTimeout
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("shutdown timeout exceeded, closing remaining connections")
		s.closeConns(false)
		<-done
	}
	return nil
}

// DetectNetwork returns "unix" or "tcp" for the given listen address.
// An address containing a path separator or not in the host:port form is
// treated as a unix domain socket path.
func DetectNetwork(addr string) string {
	if strings.Contains(addr, "/") {
		return "unix"
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return "tcp"
	}
	return "unix"
}

// listenUnix listens on a unix domain socket, removing a stale socket file
// left by a dead process if necessary.
func listenUnix(path string) (net.Listener, error) {
	if st, err := os.Stat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s already exists and is not a socket", path)
		}
		if conn, err := net.Dial("unix", path); err == nil {
			conn.Close()
			return nil, fmt.Errorf("%s is already in use by another process", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("failed to remove stale socket file %s: %w", path, err)
		}
		slog.Info("removed stale socket file", "path", path)
	}
	return net.Listen("unix", path)
}

func (s *Server) addConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *Server) removeConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// closeConns closes all connections. When graceful is true, it only closes
// the read side so that responses to buffered requests can still be written.
func (s *Server) closeConns(graceful bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		if cr, ok := conn.(interface{ CloseRead() error }); graceful && ok {
			cr.CloseRead()
		} else {
			conn.Close()
		}
	}
}

// handleConn reads request lines from the connection and writes one response
// line per request, in request order.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, initialLineBufSize), maxLineSize)
	w := bufio.NewWriter(conn)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		resp := s.handleLine(line)
		if resp == nil {
			continue
		}
		if err := writeResponse(w, resp); err != nil {
			slog.Warn("failed to write response", "error", err)
			return
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		// e.g. bufio.ErrTooLong: respond with a parse error and close.
		writeResponse(w, newErrorResponse(nil, CodeParseError, "Parse error", err.Error()))
		slog.Warn("failed to read request", "error", err)
	}
}

func writeResponse(w *bufio.Writer, resp *Response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := w.Write(b); err != nil {
		return err
	}
	return w.Flush()
}
