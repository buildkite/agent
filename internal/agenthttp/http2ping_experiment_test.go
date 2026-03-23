// Package agenthttp - HTTP/2 PING experiment
//
// This test verifies whether transport.HTTP2.SendPingTimeout is actually
// applied when golang.org/x/net/http2.ConfigureTransports is also called.
//
// The question: does x/net's per-connection configFromTransport pick up
// t1.HTTP2.SendPingTimeout, or does it get ignored?
//
// Source: x/net v0.52.0 http2/config.go, configFromTransport:
//
//	conf.SendPingTimeout = h2.ReadIdleTimeout   // x/net's own value
//	if h2.t1 != nil {
//	    fillNetHTTPConfig(&conf, h2.t1.HTTP2)   // then stdlib HTTP2Config overrides
//	}
//
// fillNetHTTPConfig sets conf.SendPingTimeout = h2.SendPingTimeout if non-zero.
// So stdlib's value takes precedence over x/net's ReadIdleTimeout.
package agenthttp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

// teeReadConn wraps a net.Conn and forwards everything Read returns into pw,
// so that a separate goroutine can parse the HTTP/2 frames flowing from client.
type teeReadConn struct {
	net.Conn
	pw *io.PipeWriter
}

func (c *teeReadConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		// Write to pipe; ignore errors (pipe may be closed when test ends).
		c.pw.Write(b[:n]) //nolint:errcheck
	}
	return n, err
}

func (c *teeReadConn) Close() error {
	c.pw.CloseWithError(io.EOF)
	return c.Conn.Close()
}

// countPingsFromPipe reads the HTTP/2 stream from pr and increments count
// each time a PING frame (type 0x6) is observed that is NOT a PING ACK.
// The HTTP/2 client connection preface (24 bytes magic) is skipped first.
func countPingsFromPipe(pr *io.PipeReader, count *atomic.Int64) {
	defer pr.Close()

	const clientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n" // 24 bytes
	preface := make([]byte, len(clientPreface))
	if _, err := io.ReadFull(pr, preface); err != nil {
		return
	}

	// Parse HTTP/2 frames: 9-byte header, then payload.
	// Frame header: [length:3][type:1][flags:1][stream_id:4]
	hdr := make([]byte, 9)
	for {
		if _, err := io.ReadFull(pr, hdr); err != nil {
			return
		}
		length := int(hdr[0])<<16 | int(hdr[1])<<8 | int(hdr[2])
		frameType := hdr[3]
		flags := hdr[4]

		payload := make([]byte, length)
		if _, err := io.ReadFull(pr, payload); err != nil {
			return
		}

		const typePing = 0x6
		const flagPingAck = 0x1
		if frameType == typePing && flags&flagPingAck == 0 {
			count.Add(1)
		}
	}
}

// selfSignedTLSConfig creates a minimal self-signed TLS certificate
// for use in tests.
func selfSignedTLSConfig() (*tls.Config, *x509.CertPool, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		DNSNames:     []string{"127.0.0.1", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}

	keyPEM, _ := marshalECKey(key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, err
	}

	serverCfg := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"h2"},
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return serverCfg, pool, nil
}

func marshalECKey(key *ecdsa.PrivateKey) ([]byte, error) {
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), nil
}

// runPingExperiment starts a TLS+HTTP/2 server, applies setupTransport to a
// fresh transport, makes a long-lived request, waits for duration, and returns
// the number of PING frames the server received from the client.
func runPingExperiment(t *testing.T, setupTransport func(*http.Transport, *http2.Transport), duration time.Duration) int64 {
	t.Helper()

	serverTLS, clientPool, err := selfSignedTLSConfig()
	if err != nil {
		t.Fatalf("selfSignedTLSConfig: %v", err)
	}

	// TCP listener wrapped so we can tee reads on each accepted conn.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	var pingCount atomic.Int64

	// HTTP/2 server that serves a long streaming response.
	h2srv := &http2.Server{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Keep the response open for the entire experiment duration.
		time.Sleep(duration + 2*time.Second)
	})

	// Accept loop: tee each accepted conn, count PINGs, serve HTTP/2.
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				pr, pw := io.Pipe()
				tee := &teeReadConn{
					Conn: tls.Server(c, serverTLS),
					pw:   pw,
				}
				// Do TLS handshake.
				if err := tee.Conn.(*tls.Conn).Handshake(); err != nil {
					tee.Close()
					pr.CloseWithError(err)
					return
				}
				go countPingsFromPipe(pr, &pingCount)
				h2srv.ServeConn(tee, &http2.ServeConnOpts{Handler: handler})
			}(raw)
		}
	}()

	addr := fmt.Sprintf("https://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)

	// Build transport.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: clientPool}

	tr2, err := http2.ConfigureTransports(transport)
	if err != nil {
		t.Fatalf("ConfigureTransports: %v", err)
	}

	setupTransport(transport, tr2)

	client := &http.Client{Transport: transport, Timeout: duration + 5*time.Second}

	// Make a request in background; we just need the connection open.
	reqErrCh := make(chan error, 1)
	go func() {
		resp, err := client.Get(addr + "/stream")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		reqErrCh <- err
	}()

	// Wait for the measurement window.
	time.Sleep(duration)

	// Close the listener to stop the experiment.
	ln.Close()

	return pingCount.Load()
}

// TestHTTP2PingExperiment verifies the PING behavior under different transport configs.
//
// Key scenarios:
//  1. production (current code): HTTP2.SendPingTimeout=1s + ConfigureTransports
//     → PINGs should fire every ~1s (from transport.HTTP2 via configFromTransport)
//  2. xnet-read-idle: only tr2.ReadIdleTimeout=1s set, no HTTP2 config
//     → PINGs should also fire every ~1s (from x/net's ReadIdleTimeout)
//  3. stdlib-http2-no-xnet: only HTTP2.SendPingTimeout=1s, NO ConfigureTransports
//     → PINGs should fire (stdlib bundled h2)
//  4. no-ping: ConfigureTransports, no ping config anywhere
//     → 0 PINGs expected
func TestHTTP2PingExperiment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow HTTP/2 ping experiment in short mode")
	}

	const (
		pingInterval = 1 * time.Second
		window       = 4 * time.Second // measure for 4s, expect ~3-4 pings
	)

	cases := []struct {
		name        string
		setup       func(*http.Transport, *http2.Transport)
		wantPingsGE int64 // at least this many pings expected
		wantPingsLE int64 // at most this many pings expected
	}{
		{
			name: "production (HTTP2.SendPingTimeout + ConfigureTransports)",
			setup: func(tr *http.Transport, tr2 *http2.Transport) {
				// This is what client.go does (with 1s instead of 10s for speed).
				tr.HTTP2 = &http.HTTP2Config{
					SendPingTimeout: pingInterval,
					PingTimeout:     500 * time.Millisecond,
				}
				// ConfigureTransports was already called with tr before setup.
			},
			wantPingsGE: 2,
			wantPingsLE: 6,
		},
		{
			name: "xnet-read-idle only (tr2.ReadIdleTimeout, no HTTP2 config)",
			setup: func(tr *http.Transport, tr2 *http2.Transport) {
				tr2.ReadIdleTimeout = pingInterval
			},
			wantPingsGE: 2,
			wantPingsLE: 6,
		},
		{
			name: "no-ping (ConfigureTransports, no ping config)",
			setup: func(tr *http.Transport, tr2 *http2.Transport) {
				// No ping configuration at all.
			},
			wantPingsGE: 0,
			wantPingsLE: 1, // allow 1 for noise
		},
		{
			name: "both-conflicting: HTTP2.SendPingTimeout=1s overrides tr2.ReadIdleTimeout=10s",
			setup: func(tr *http.Transport, tr2 *http2.Transport) {
				// x/net's ReadIdleTimeout would cause pings every 10s (none in 4s window).
				// But transport.HTTP2.SendPingTimeout=1s should override it.
				tr2.ReadIdleTimeout = 10 * time.Second
				tr.HTTP2 = &http.HTTP2Config{
					SendPingTimeout: pingInterval,
					PingTimeout:     500 * time.Millisecond,
				}
			},
			wantPingsGE: 2,
			wantPingsLE: 6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pings := runPingExperiment(t, tc.setup, window)
			t.Logf("PINGs received in %s: %d (want %d..%d)",
				window, pings, tc.wantPingsGE, tc.wantPingsLE)
			if pings < tc.wantPingsGE {
				t.Errorf("too few PINGs: got %d, want >= %d", pings, tc.wantPingsGE)
			}
			if pings > tc.wantPingsLE {
				t.Errorf("too many PINGs: got %d, want <= %d", pings, tc.wantPingsLE)
			}
		})
	}
}
