// Package vmsandbox runs each job's bootstrap inside a fresh, disposable
// Firecracker microVM while the registered agent stays on the host.
//
// The host side (Runner) launches Firecracker, copies a base root filesystem
// per job, and talks to the guest helper over a single virtio-vsock
// connection. The guest side (Guest) runs as the guest's only job-related
// process: it receives the bootstrap environment, runs `buildkite-agent
// bootstrap`, streams its output back, reports the exit status, and powers
// the guest off.
//
// The protocol is deliberately tiny: newline-delimited JSON messages in each
// direction over one stream. There is no request/response matching, no
// host-command endpoint, and nothing the guest can ask the host to do other
// than accept output and an exit status.
package vmsandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// GuestPort is the vsock port the guest helper connects to on the host.
// Firecracker exposes guest-initiated connections to this port as the Unix
// socket "<vsock uds path>_<port>" on the host.
const GuestPort = 5000

// Message types, host -> guest.
const (
	// TypeStart carries the bootstrap environment and job context file
	// contents. Sent once, immediately after the guest connects.
	TypeStart = "start"
	// TypeInterrupt asks the guest to send the cancel signal to bootstrap.
	TypeInterrupt = "int"
	// TypePowerOff asks the guest to power off. The host also treats
	// Firecracker exiting as the authoritative signal that the guest is gone.
	TypePowerOff = "off"
)

// Message types, guest -> host.
const (
	// TypeReady is the first message the guest sends. Receiving it is what
	// "guest readiness" means to the runner.
	TypeReady = "ready"
	// TypeOutput carries a chunk of bootstrap stdout/stderr.
	TypeOutput = "o"
	// TypeLog carries a diagnostic line from the helper itself. The host
	// writes these to the job log too, so they're visible next to bootstrap
	// output, but they're distinguishable from it.
	TypeLog = "log"
	// TypeExit reports bootstrap's wait status. It is the last message the
	// guest sends about the job.
	TypeExit = "x"
)

// Message is the single on-the-wire type. Which fields are meaningful
// depends on Type; unused fields are omitted.
type Message struct {
	Type string `json:"t"`

	// TypeStart
	Env         []string `json:"env,omitempty"`
	EnvFile     string   `json:"env_file,omitempty"`
	EnvJSONFile string   `json:"env_json_file,omitempty"`

	// TypeInterrupt: whether the job was cancelled because of a Buildkite
	// job-level timeout, so the guest can create the timeout marker file that
	// bootstrap turns into BUILDKITE_JOB_TIMED_OUT.
	TimedOut bool `json:"timed_out,omitempty"`

	// TypeOutput
	Data []byte `json:"d,omitempty"`

	// TypeLog
	Text string `json:"m,omitempty"`

	// TypeExit
	ExitStatus int  `json:"s,omitempty"`
	Signaled   bool `json:"sig,omitempty"`
	Signal     int  `json:"signum,omitempty"`
}

// Conn wraps a stream with the newline-delimited JSON framing. Writes are
// serialised so output chunks and control messages can be sent from
// different goroutines.
type Conn struct {
	r  *bufio.Reader
	w  io.Writer
	mu sync.Mutex
}

// NewConn wraps rw. The reader side is buffered, so callers must not read
// from rw directly after this.
func NewConn(rw io.ReadWriter) *Conn {
	return &Conn{
		// Env values (and BUILDKITE_MESSAGE) can be large; give the reader
		// room so a start message doesn't trip bufio's line limit.
		r: bufio.NewReaderSize(rw, 1<<20),
		w: rw,
	}
}

// Send encodes and writes one message.
func (c *Conn) Send(m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding %s message: %w", m.Type, err)
	}
	b = append(b, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(b)
	return err
}

// Recv reads and decodes the next message. It returns io.EOF (possibly
// wrapped) when the peer has gone away.
func (c *Conn) Recv() (Message, error) {
	var m Message
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		if len(line) == 0 {
			return m, err
		}
		return m, fmt.Errorf("truncated message: %w", err)
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("decoding message: %w", err)
	}
	return m, nil
}

// outputWriter turns Write calls into TypeOutput messages. bootstrap's
// stdout and stderr are both pointed at one of these on the guest.
type outputWriter struct {
	conn *Conn
}

func (w outputWriter) Write(p []byte) (int, error) {
	if err := w.conn.Send(Message{Type: TypeOutput, Data: p}); err != nil {
		return 0, err
	}
	return len(p), nil
}
