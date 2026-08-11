// Package egress controls which network path application-initiated
// connections use. Container mode relies on the namespace default route;
// host mode fails closed until a verified tunnel device is available.
package egress

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
)

var ErrTunnelNotReady = errors.New("tunnel egress is not ready")

type Controller struct {
	mode string

	mu     sync.RWMutex
	device string
	ready  bool
}

func New(mode string) *Controller {
	return &Controller{mode: mode, ready: mode == "container"}
}

func (c *Controller) HostMode() bool { return c.mode == "host" }

func (c *Controller) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Configure enables host egress only after the OpenVPN-created device is
// visible. Container mode deliberately remains a no-op.
func (c *Controller) Configure(device string) error {
	if !c.HostMode() {
		return nil
	}
	if device == "" {
		return errors.New("OpenVPN did not report a tunnel device")
	}
	if _, err := net.InterfaceByName(device); err != nil {
		return err
	}
	if err := setupHostRoute(device); err != nil {
		return err
	}
	c.mu.Lock()
	c.device = device
	c.ready = true
	c.mu.Unlock()
	return nil
}

// Clear removes host-mode policy state before the tunnel is stopped and
// immediately makes future application dials fail closed.
func (c *Controller) Clear() {
	if !c.HostMode() {
		return
	}
	c.mu.Lock()
	c.ready = false
	c.device = ""
	c.mu.Unlock()
	clearHostRoute()
}

func (c *Controller) network(network string) string {
	if !c.HostMode() {
		return network
	}
	switch network {
	case "tcp":
		return "tcp4"
	case "udp":
		return "udp4"
	default:
		return network
	}
}

func (c *Controller) control(_, _ string, raw syscall.RawConn) error {
	if !c.HostMode() {
		return nil
	}
	c.mu.RLock()
	device, ready := c.device, c.ready
	c.mu.RUnlock()
	if !ready {
		return ErrTunnelNotReady
	}
	return bindSocket(device, raw)
}

// DialContext creates an application dial constrained to the active tunnel in
// host mode. Callers still own their context timeout.
func (c *Controller) DialContext(ctx context.Context, network, address string, timeout, keepAlive time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout, KeepAlive: keepAlive}
	if c.HostMode() {
		d.Control = c.control
	}
	return d.DialContext(ctx, c.network(network), address)
}

// NewDeviceDialer returns a dial function constrained to one temporary tunnel
// plus its cleanup function. It never changes the serving controller. Linux
// uses an isolated mark and policy table so deployments only need NET_ADMIN;
// macOS binds directly to the interface.
func NewDeviceDialer(device string) (func(context.Context, string, string, time.Duration, time.Duration) (net.Conn, error), func(), error) {
	if device == "" {
		return nil, nil, errors.New("tunnel device is required")
	}
	cleanup, err := setupDeviceRoute(device)
	if err != nil {
		return nil, nil, err
	}
	dial := func(ctx context.Context, network, address string, timeout, keepAlive time.Duration) (net.Conn, error) {
		d := net.Dialer{
			Timeout:   timeout,
			KeepAlive: keepAlive,
			Control: func(_, _ string, raw syscall.RawConn) error {
				return bindDeviceSocket(device, raw)
			},
		}
		return d.DialContext(ctx, network, address)
	}
	return dial, cleanup, nil
}

// Resolver returns a DNS resolver whose UDP requests follow the same egress
// policy as proxied TCP traffic.
func (c *Controller) Resolver(server string) *net.Resolver {
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return c.DialContext(ctx, network, server, 5*time.Second, 0)
		},
	}
}
