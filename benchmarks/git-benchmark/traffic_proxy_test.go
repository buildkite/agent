package main

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestCountingProxyCapturesConnectionsAndBytes(t *testing.T) {
	t.Parallel()

	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = upstream.Close() })

	go func() {
		conn, err := upstream.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		buf := make([]byte, len("ping"))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		_, _ = conn.Write([]byte("hello"))
		closeWrite(conn)
	}()

	proxy, err := newCountingProxy(upstream.Addr().String())
	if err != nil {
		t.Fatalf("newCountingProxy() error = %v", err)
	}
	t.Cleanup(func() { _ = proxy.Close() })

	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxy.Port())))
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("conn.Write() error = %v", err)
	}
	closeWrite(conn)

	buf := make([]byte, len("hello"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("io.ReadFull() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	snapshot := proxy.Snapshot()
	for time.Now().Before(deadline) && (snapshot.Connections != 1 || snapshot.BytesToUpstream != int64(len("ping")) || snapshot.BytesFromUpstream != int64(len("hello"))) {
		time.Sleep(10 * time.Millisecond)
		snapshot = proxy.Snapshot()
	}

	if snapshot.Connections != 1 {
		t.Fatalf("snapshot.Connections = %d, want 1", snapshot.Connections)
	}
	if snapshot.BytesToUpstream != int64(len("ping")) {
		t.Fatalf("snapshot.BytesToUpstream = %d, want %d", snapshot.BytesToUpstream, len("ping"))
	}
	if snapshot.BytesFromUpstream != int64(len("hello")) {
		t.Fatalf("snapshot.BytesFromUpstream = %d, want %d", snapshot.BytesFromUpstream, len("hello"))
	}
	if snapshot.TotalBytes() != int64(len("ping")+len("hello")) {
		t.Fatalf("snapshot.TotalBytes() = %d, want %d", snapshot.TotalBytes(), len("ping")+len("hello"))
	}
}
