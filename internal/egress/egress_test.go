package egress

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostModeFailsClosedBeforeTunnel(t *testing.T) {
	c := New("host")
	if c.Ready() {
		t.Fatal("host egress must start unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.DialContext(ctx, "tcp", "127.0.0.1:1", time.Second, 0)
	if !errors.Is(err, ErrTunnelNotReady) {
		t.Fatalf("dial error = %v, want ErrTunnelNotReady", err)
	}
}

func TestContainerModeIsReady(t *testing.T) {
	if !New("container").Ready() {
		t.Fatal("container egress should rely on its namespace route immediately")
	}
}

func TestNewDeviceDialerRequiresDevice(t *testing.T) {
	_, _, err := NewDeviceDialer("")
	if err == nil {
		t.Fatal("missing device should be rejected")
	}
}
