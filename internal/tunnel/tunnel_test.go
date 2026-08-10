package tunnel

import (
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

func TestStopIdle(t *testing.T) {
	tun := New()
	if err := tun.Stop(); err != nil {
		t.Fatalf("stop on idle tunnel should be a no-op: %v", err)
	}
}
