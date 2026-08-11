package tunnel

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClassifyHandshake(t *testing.T) {
	tun := New()
	go tun.classify("Initialization Sequence Completed")
	select {
	case <-tun.handshakeCh:
	case <-time.After(time.Second):
		t.Fatal("handshake not detected")
	}
}

func TestClassifyAuthFail(t *testing.T) {
	tun := New()
	go tun.classify("AUTH_FAILED,server: auth failure")
	select {
	case ev := <-tun.events:
		if ev.Type != EventAuthFail {
			t.Fatalf("want EventAuthFail, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("auth fail event not emitted")
	}
}

func TestClassifyTunFail(t *testing.T) {
	tun := New()
	go tun.classify("ERROR: Cannot allocate TUN/TAP dev dynamically")
	select {
	case ev := <-tun.events:
		if ev.Type != EventTunFail {
			t.Fatalf("want EventTunFail, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("tun fail event not emitted")
	}
}

func TestWaitHandshakeTimeout(t *testing.T) {
	tun := New()
	start := time.Now()
	err := tun.WaitHandshake(200 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("returned too early")
	}
}

func TestWaitHandshakeContextCancellation(t *testing.T) {
	tun := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tun.WaitHandshakeContext(ctx, time.Second); err == nil {
		t.Fatal("cancelled context should stop handshake wait")
	}
}

func TestOpenVPNArgsRouteNoPull(t *testing.T) {
	args := openVPNArgs(Options{ConfigFile: "node.ovpn", AuthFile: "auth", RouteNoPull: true})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--dev tun") || !strings.Contains(joined, "--route-nopull") {
		t.Fatalf("args = %v", args)
	}
}

func TestCaptureDevice(t *testing.T) {
	tun := New()
	tun.captureDevice("[out] TUN/TAP device /dev/tun0 opened")
	if got := tun.Device(); got != "tun0" {
		t.Fatalf("linux device = %q, want tun0", got)
	}
	tun.captureDevice("[out] Opened utun device utun7")
	if got := tun.Device(); got != "utun7" {
		t.Fatalf("darwin device = %q, want utun7", got)
	}
	if isTunnelDevice("tunnel") || isTunnelDevice("tun") || isTunnelDevice("utun") {
		t.Fatal("non-device names must not be accepted")
	}
}

func TestStopIdle(t *testing.T) {
	tun := New()
	if err := tun.Stop(); err != nil {
		t.Fatalf("stop on idle tunnel should be a no-op: %v", err)
	}
}
