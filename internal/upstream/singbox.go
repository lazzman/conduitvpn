package upstream

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"conduitvpn/internal/logx"
)

// Runner supervises the sing-box subprocess: spawn → wait for the local
// listener → restart on unexpected exit. udp marks hysteria2-style
// inbound-only processes (no TCP port to probe).
type Runner struct {
	cfgPath string
	port    int
	udp     bool

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	proc   *exec.Cmd
}

// StartRunner spawns sing-box and waits until its listener is ready.
func StartRunner(ctx context.Context, cfgPath string, port int) (*Runner, error) {
	r := &Runner{cfgPath: cfgPath, port: port}
	r.ctx, r.cancel = context.WithCancel(ctx)

	// validate the config before running
	if out, err := exec.Command("sing-box", "check", "-c", cfgPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sing-box check failed: %w: %s", err, truncate(string(out), 300))
	}

	if err := r.spawn(); err != nil {
		return nil, err
	}
	if err := r.waitReady(20 * time.Second); err != nil {
		r.Stop()
		return nil, err
	}
	logx.Info("sing-box upstream ready", "port", port)
	go r.watch()
	return r, nil
}

// StartRunnerUDP spawns sing-box whose inbound is UDP-only (e.g.
// hysteria2); readiness is confirmed by process liveness since there is
// no TCP port to probe.
func StartRunnerUDP(ctx context.Context, cfgPath string, port int) (*Runner, error) {
	r := &Runner{cfgPath: cfgPath, port: port, udp: true}
	r.ctx, r.cancel = context.WithCancel(ctx)
	if out, err := exec.Command("sing-box", "check", "-c", cfgPath).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sing-box check failed: %w: %s", err, truncate(string(out), 300))
	}
	if err := r.spawn(); err != nil {
		return nil, err
	}
	if err := r.waitReady(15 * time.Second); err != nil {
		r.Stop()
		return nil, err
	}
	logx.Info("sing-box udp inbound ready", "port", port)
	go r.watch()
	return r, nil
}

func (r *Runner) spawn() error {
	cmd := exec.Command("sing-box", "run", "-c", r.cfgPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn sing-box: %w", err)
	}
	r.mu.Lock()
	r.proc = cmd
	r.mu.Unlock()
	go scanLines(stdout, "sb")
	go scanLines(stderr, "sb")
	return nil
}

func scanLines(r interface{ Read([]byte) (int, error) }, tag string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 16*1024), 512*1024)
	for sc.Scan() {
		logx.Debug("sing-box", "line", sc.Text())
	}
}

func (r *Runner) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", r.port)
	for time.Now().Before(deadline) {
		if r.udp {
			r.mu.Lock()
			alive := r.proc != nil && r.proc.Process != nil && r.proc.ProcessState == nil
			r.mu.Unlock()
			if alive {
				return nil
			}
		} else {
			conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("sing-box listener %s not ready in %s", addr, timeout)
}

// watch restarts sing-box when it exits unexpectedly.
func (r *Runner) watch() {
	for {
		r.mu.Lock()
		proc := r.proc
		r.mu.Unlock()
		if proc == nil {
			return
		}
		err := proc.Wait()
		select {
		case <-r.ctx.Done():
			return
		default:
		}
		logx.Warn("sing-box exited, restarting", "err", err)
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		if err := r.spawn(); err != nil {
			logx.Error("sing-box respawn failed", "err", err)
			return
		}
	}
}

// Stop terminates sing-box.
func (r *Runner) Stop() {
	r.cancel()
	r.mu.Lock()
	proc := r.proc
	r.mu.Unlock()
	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
		_, _ = proc.Process.Wait()
	}
}
