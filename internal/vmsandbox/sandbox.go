package vmsandbox

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

// Config describes the host-side sandbox installation. Everything lives
// under Dir with a fixed layout so there's one flag to point at:
//
//	<Dir>/bin/firecracker      Firecracker binary
//	<Dir>/images/vmlinux       uncompressed guest kernel
//	<Dir>/images/rootfs.ext4   base guest root filesystem (copied per job)
//	<Dir>/state/<job-id>/      per-job working directory (disposable)
//	<Dir>/state/POISONED       present if a previous job could not be torn
//	                           down; blocks all further use until removed
type Config struct {
	Dir string

	// Guest machine budget. This bounds what the guest kernel sees, not
	// what Firecracker itself may consume on the host.
	VCPUs     int
	MemoryMiB int

	// Tap is the pre-created, agent-user-owned tap device the guest's eth0
	// is attached to, and the addressing on either end of it. Creating tap
	// devices and NAT needs CAP_NET_ADMIN, so it is done once by an operator
	// (see guest/setup-network.sh), not by the agent.
	Tap TapConfig

	// DNS servers written into the guest's resolv.conf.
	DNS []string

	// BootTimeout bounds the time from launching Firecracker to the guest
	// helper connecting and sending TypeReady.
	BootTimeout time.Duration

	// ShutdownTimeout bounds how long teardown waits for a cooperative
	// power-off before killing Firecracker.
	ShutdownTimeout time.Duration
}

// TapConfig is a tap device plus the /30-style addressing across it.
type TapConfig struct {
	Device   string
	HostAddr netip.Prefix // e.g. 172.16.0.1/30
	GuestIP  netip.Addr   // e.g. 172.16.0.2
}

// ParseTap parses "device:host-ip/prefix-len:guest-ip".
func ParseTap(s string) (TapConfig, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return TapConfig{}, fmt.Errorf("tap spec %q must be device:host-ip/prefix-len:guest-ip", s)
	}
	host, err := netip.ParsePrefix(parts[1])
	if err != nil {
		return TapConfig{}, fmt.Errorf("tap spec %q: host address: %w", s, err)
	}
	guest, err := netip.ParseAddr(parts[2])
	if err != nil {
		return TapConfig{}, fmt.Errorf("tap spec %q: guest address: %w", s, err)
	}
	if !host.Contains(guest) {
		return TapConfig{}, fmt.Errorf("tap spec %q: guest address %v is not within %v", s, guest, host)
	}
	if parts[0] == "" {
		return TapConfig{}, fmt.Errorf("tap spec %q: empty device name", s)
	}
	return TapConfig{Device: parts[0], HostAddr: host, GuestIP: guest}, nil
}

func (c Config) firecrackerPath() string { return filepath.Join(c.Dir, "bin", "firecracker") }
func (c Config) kernelPath() string      { return filepath.Join(c.Dir, "images", "vmlinux") }
func (c Config) rootfsPath() string      { return filepath.Join(c.Dir, "images", "rootfs.ext4") }
func (c Config) stateDir() string        { return filepath.Join(c.Dir, "state") }
func (c Config) poisonPath() string      { return filepath.Join(c.stateDir(), "POISONED") }

// Sandbox is the validated host-side installation. One per agent process;
// it hands out a Runner per job.
type Sandbox struct {
	conf   Config
	logger logger.Logger
}

// New validates the installation and reaps anything left over from a
// previous agent process before returning. It fails, rather than degrading,
// if anything needed to run a job in a VM is missing, or if a previous job's
// sandbox could not be cleaned up.
func New(l logger.Logger, conf Config) (*Sandbox, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("vm sandbox requires Linux with KVM, not %s", runtime.GOOS)
	}
	if conf.Dir == "" {
		return nil, errors.New("vm sandbox directory not set")
	}
	if conf.VCPUs < 1 {
		return nil, fmt.Errorf("vm sandbox vcpus must be at least 1, got %d", conf.VCPUs)
	}
	if conf.MemoryMiB < 128 {
		return nil, fmt.Errorf("vm sandbox memory must be at least 128 MiB, got %d", conf.MemoryMiB)
	}
	if conf.Tap.Device == "" {
		return nil, errors.New("vm sandbox tap device not set")
	}
	if conf.BootTimeout <= 0 {
		// Nested virtualisation (the local dev setup) boots in 40-55 s; bare
		// KVM is much faster. Leave headroom either way.
		conf.BootTimeout = 120 * time.Second
	}
	if conf.ShutdownTimeout <= 0 {
		conf.ShutdownTimeout = 10 * time.Second
	}
	if len(conf.DNS) == 0 {
		conf.DNS = []string{"1.1.1.1", "8.8.8.8"}
	}

	for _, p := range []string{conf.firecrackerPath(), conf.kernelPath(), conf.rootfsPath()} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("vm sandbox installation incomplete: %w", err)
		}
	}
	// Firecracker opens /dev/kvm read-write; check that we could too, so the
	// failure is reported before a job is accepted rather than per job.
	kvm, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("vm sandbox needs read-write access to /dev/kvm: %w", err)
	}
	_ = kvm.Close()
	if _, err := os.Stat(filepath.Join("/sys/class/net", conf.Tap.Device)); err != nil {
		return nil, fmt.Errorf("vm sandbox tap device %q does not exist (run guest/setup-network.sh): %w", conf.Tap.Device, err)
	}

	if err := os.MkdirAll(conf.stateDir(), 0o755); err != nil {
		return nil, fmt.Errorf("creating vm sandbox state dir: %w", err)
	}

	s := &Sandbox{conf: conf, logger: l}
	if err := s.checkPoison(); err != nil {
		return nil, err
	}
	if err := s.Sweep(); err != nil {
		return nil, fmt.Errorf("reaping stale vm sandboxes: %w", err)
	}
	return s, nil
}

// checkPoison refuses to proceed while a POISONED marker exists.
func (s *Sandbox) checkPoison() error {
	reason, err := os.ReadFile(s.conf.poisonPath())
	switch {
	case err == nil:
		return fmt.Errorf("vm sandbox is poisoned: a previous job's sandbox could not be torn down (%s). Inspect %s, clean up by hand, then remove the marker file %s",
			strings.TrimSpace(string(reason)), s.conf.stateDir(), s.conf.poisonPath())
	case os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("reading vm sandbox poison marker: %w", err)
	}
}

// poison records that teardown failed. Nothing else will run in this
// sandbox directory until an operator removes the marker.
func (s *Sandbox) poison(reason string) {
	s.logger.Errorf("[vmsandbox] Teardown failed; poisoning sandbox to prevent slot reuse: %s", reason)
	if err := os.WriteFile(s.conf.poisonPath(), []byte(reason+"\n"), 0o644); err != nil {
		s.logger.Errorf("[vmsandbox] Could not write poison marker %s: %v", s.conf.poisonPath(), err)
	}
}

// Sweep kills any Firecracker left running by a previous agent process and
// removes every per-job state directory. The ownership rule is simple: the
// state directory is the record of what this sandbox owns, and a pidfile in
// it names a Firecracker that must not outlive the job directory.
//
// A Firecracker that can't be killed, or a directory that can't be removed,
// is an error: the caller must not start accepting jobs.
func (s *Sandbox) Sweep() error {
	entries, err := os.ReadDir(s.conf.stateDir())
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := filepath.Join(s.conf.stateDir(), ent.Name())
		if err := s.reapDir(dir); err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		s.logger.Warnf("[vmsandbox] Removed stale sandbox %s", dir)
	}
	return nil
}

// reapDir kills the Firecracker named by dir's pidfile (if it's still ours)
// and removes dir.
func (s *Sandbox) reapDir(dir string) error {
	pidBytes, err := os.ReadFile(filepath.Join(dir, "firecracker.pid"))
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil {
			return fmt.Errorf("bad pidfile: %w", err)
		}
		if isOurFirecracker(pid, s.conf.firecrackerPath(), dir) {
			if err := killAndWait(pid, s.conf.ShutdownTimeout); err != nil {
				return fmt.Errorf("killing stale firecracker pid %d: %w", pid, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(dir)
}

// isOurFirecracker reports whether pid is alive and is a Firecracker
// process that was launched with an API socket inside dir. That guards
// against a recycled PID belonging to something unrelated.
func isOurFirecracker(pid int, firecrackerPath, dir string) bool {
	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	args := strings.Split(string(cmdline), "\x00")
	if len(args) == 0 || args[0] != firecrackerPath {
		return false
	}
	for _, a := range args {
		if strings.HasPrefix(a, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
