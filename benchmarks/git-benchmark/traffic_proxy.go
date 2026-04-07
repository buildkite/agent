package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

type trafficSnapshot struct {
	Connections       int64 `json:"connections"`
	BytesToUpstream   int64 `json:"bytes_to_upstream"`
	BytesFromUpstream int64 `json:"bytes_from_upstream"`
}

func (s trafficSnapshot) Delta(previous trafficSnapshot) trafficSnapshot {
	return trafficSnapshot{
		Connections:       s.Connections - previous.Connections,
		BytesToUpstream:   s.BytesToUpstream - previous.BytesToUpstream,
		BytesFromUpstream: s.BytesFromUpstream - previous.BytesFromUpstream,
	}
}

func (s trafficSnapshot) TotalBytes() int64 {
	return s.BytesToUpstream + s.BytesFromUpstream
}

type countingProxy struct {
	listener     net.Listener
	upstreamAddr string
	wg           sync.WaitGroup

	connections       atomic.Int64
	bytesToUpstream   atomic.Int64
	bytesFromUpstream atomic.Int64
}

func newCountingProxy(upstreamAddr string) (*countingProxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for counting proxy: %w", err)
	}

	p := &countingProxy{listener: listener, upstreamAddr: upstreamAddr}
	p.wg.Add(1)
	go p.serve()
	return p, nil
}

func (p *countingProxy) serve() {
	defer p.wg.Done()

	for {
		conn, err := p.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handle(conn)
		}()
	}
}

func (p *countingProxy) handle(client net.Conn) {
	defer func() { _ = client.Close() }()

	upstream, err := net.Dial("tcp", p.upstreamAddr)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()

	p.connections.Add(1)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		n, _ := io.Copy(upstream, client)
		p.bytesToUpstream.Add(n)
		closeWrite(upstream)
	}()

	go func() {
		defer wg.Done()
		n, _ := io.Copy(client, upstream)
		p.bytesFromUpstream.Add(n)
		closeWrite(client)
	}()

	wg.Wait()
}

func (p *countingProxy) Port() int {
	addr, _ := p.listener.Addr().(*net.TCPAddr)
	if addr == nil {
		return 0
	}
	return addr.Port
}

func (p *countingProxy) Snapshot() trafficSnapshot {
	return trafficSnapshot{
		Connections:       p.connections.Load(),
		BytesToUpstream:   p.bytesToUpstream.Load(),
		BytesFromUpstream: p.bytesFromUpstream.Load(),
	}
}

func (p *countingProxy) Close() error {
	err := p.listener.Close()
	p.wg.Wait()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func closeWrite(conn net.Conn) {
	type closeWriter interface {
		CloseWrite() error
	}

	if closer, ok := conn.(closeWriter); ok {
		_ = closer.CloseWrite()
	}
}
