// Package tunnel controls the external openvpn process: spawning,
// handshake detection from stdout, diagnostics capture, and graceful
// shutdown. Routing follows 方案 B: no --route-nopull, the pushed
// redirect-gateway becomes the container's default route.
package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"conduitvpn/internal/logx"
)

type EventType int

const (
	EventLog       EventType = iota // ordinary output line
	EventHandshake                  // "Initialization Sequence Completed"
	EventAuthFail                   // AUTH_FAILED
	EventTunFail                    // TUN/TAP unavailable
	EventFatal                      // unrecoverable openvpn error line
	EventExit                       // process reaped
)

func (t EventType) String() string {
	switch t {
	case EventHandshake:
		return "handshake"
	case EventAuthFail:
		return "auth_fail"
	case EventTunFail:
		return "tun_fail"
	case EventFatal:
		return "fatal"
	case EventExit:
		return "exit"
	default:
		return "log"
	}
}

type Event struct {
	Type EventType
	Line string
}

type Options struct {
	ConfigFile  string
	AuthFile    string
	Dev         string
	Verb        int
	RouteNoPull bool
}

type Tunnel struct {
	cmd         *exec.Cmd
	events      chan Event
	handshakeCh chan struct{}
	exitCh      chan struct{}
	tail        []string
	tailMax     int
	device      string
	mu          sync.Mutex
	killed      bool
}

func New() *Tunnel {
	return &Tunnel{
		events:      make(chan Event, 128),
		handshakeCh: make(chan struct{}, 1),
		exitCh:      make(chan struct{}),
		tailMax:     200,
	}
}

func (t *Tunnel) Events() <-chan Event { return t.events }

// Device returns the tunnel device reported by OpenVPN after it has opened it.
func (t *Tunnel) Device() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.device
}

func (t *Tunnel) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cmd != nil && t.cmd.Process != nil && t.cmd.ProcessState == nil
}

// Tail returns the last n captured output lines.
func (t *Tunnel) Tail(n int) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n <= 0 || n > len(t.tail) {
		n = len(t.tail)
	}
	out := make([]string, n)
	copy(out, t.tail[len(t.tail)-n:])
	return out
}

// Start spawns openvpn. The handshake and diagnostics arrive on Events().
func (t *Tunnel) Start(opts Options) error {
	t.mu.Lock()
	if t.cmd != nil && t.cmd.Process != nil && t.cmd.ProcessState == nil {
		t.mu.Unlock()
		return errors.New("tunnel already running")
	}
	t.cmd = nil
	t.killed = false
	t.mu.Unlock()

	cmd := exec.Command("openvpn", openVPNArgs(opts)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn openvpn: %w", err)
	}

	t.mu.Lock()
	t.cmd = cmd
	t.mu.Unlock()

	go t.scan(stdout, "out")
	go t.scan(stderr, "err")
	go t.wait()
	return nil
}

func openVPNArgs(opts Options) []string {
	dev := opts.Dev
	if dev == "" {
		dev = "tun"
	}
	verb := opts.Verb
	if verb == 0 {
		verb = 3
	}
	args := []string{
		"--config", opts.ConfigFile,
		"--dev", dev,
		"--dev-type", "tun",
		"--verb", fmt.Sprint(verb),
	}
	if opts.AuthFile != "" {
		args = append(args, "--auth-user-pass", opts.AuthFile)
	}
	if opts.RouteNoPull {
		args = append(args, "--route-nopull")
	}
	return args
}

// WaitHandshake blocks until the tunnel reports "Initialization Sequence
// Completed", or fails fast on auth/TUN/fatal errors, or times out.
func (t *Tunnel) WaitHandshake(timeout time.Duration) error {
	return t.WaitHandshakeContext(context.Background(), timeout)
}

// WaitHandshakeContext is WaitHandshake with cancellation support for
// short-lived tunnel users that must stop promptly during shutdown.
func (t *Tunnel) WaitHandshakeContext(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.handshakeCh:
			return nil
		case ev := <-t.events:
			switch ev.Type {
			case EventAuthFail:
				return fmt.Errorf("authentication failed: %s", ev.Line)
			case EventTunFail:
				return fmt.Errorf("tun device unavailable: %s", ev.Line)
			case EventFatal:
				return fmt.Errorf("openvpn fatal: %s", ev.Line)
			case EventExit:
				return fmt.Errorf("openvpn exited before handshake: %s", ev.Line)
			}
		case <-timer.C:
			return fmt.Errorf("handshake timeout after %s", timeout)
		}
	}
}

// Stop gracefully terminates openvpn (SIGTERM to the process group,
// escalate to SIGKILL after 5s) and waits for the process to be reaped.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	cmd := t.cmd
	t.killed = true
	t.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM) // fallback: direct signal
	}
	select {
	case <-t.exitCh:
		return nil
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-t.exitCh
		return nil
	}
}

// Version returns the first line of `openvpn --version`.
func Version() (string, error) {
	out, err := exec.Command("openvpn", "--version").Output()
	if err != nil {
		return "", err
	}
	first := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimSpace(first), nil
}

func (t *Tunnel) scan(r io.Reader, tag string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		line = "[" + tag + "] " + line
		t.capture(line)
		t.classify(line)
	}
}

func (t *Tunnel) classify(line string) {
	t.captureDevice(line)
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "initialization sequence completed"):
		t.emit(EventHandshake, line)
	case strings.Contains(lower, "auth_failed") || strings.Contains(lower, "authentication failure"):
		t.emit(EventAuthFail, line)
	case strings.Contains(lower, "cannot allocate tun") ||
		strings.Contains(lower, "cannot open tun/tap dev") ||
		strings.Contains(lower, "cannot ioctl") ||
		strings.Contains(lower, "operation not permitted"):
		t.emit(EventTunFail, line)
	case strings.Contains(lower, "fatal error") || strings.Contains(lower, "exiting due to fatal error"):
		t.emit(EventFatal, line)
	default:
		t.emit(EventLog, line)
	}
}

func (t *Tunnel) captureDevice(line string) {
	for _, field := range strings.Fields(line) {
		field = strings.Trim(field, "[]():,.")
		name := strings.TrimPrefix(field, "/dev/")
		if isTunnelDevice(name) {
			t.mu.Lock()
			t.device = name
			t.mu.Unlock()
			return
		}
	}
}

func isTunnelDevice(name string) bool {
	prefix := "tun"
	if strings.HasPrefix(name, "utun") {
		prefix = "utun"
	}
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	for _, ch := range name[len(prefix):] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func (t *Tunnel) capture(line string) {
	t.mu.Lock()
	t.tail = append(t.tail, line)
	if len(t.tail) > t.tailMax {
		t.tail = t.tail[len(t.tail)-t.tailMax:]
	}
	t.mu.Unlock()
}

func (t *Tunnel) emit(typ EventType, line string) {
	if typ == EventHandshake {
		select {
		case t.handshakeCh <- struct{}{}:
		default:
		}
	}
	if typ == EventLog {
		logx.Debug("openvpn", "line", line)
	}
	select {
	case t.events <- Event{typ, line}:
	default: // consumer slow; tail still has it
	}
}

// wait reaps the process exactly once and publishes EventExit.
func (t *Tunnel) wait() {
	err := t.cmd.Wait()
	t.mu.Lock()
	killed := t.killed
	t.mu.Unlock()
	if killed {
		t.emit(EventExit, "openvpn stopped")
	} else {
		t.emit(EventExit, fmt.Sprintf("openvpn exited: %v", err))
	}
	close(t.exitCh)
}
