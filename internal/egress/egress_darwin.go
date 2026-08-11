//go:build darwin

package egress

import (
	"fmt"
	"net"
	"syscall"
)

// IP_BOUND_IF is the Darwin IPv4 socket option. Host mode intentionally uses
// IPv4 only because the OpenVPN tunnel implementation is IPv4-only.
const ipBoundIf = 25

func setupHostRoute(_ string) error { return nil }

func clearHostRoute() {}

func bindSocket(device string, raw syscall.RawConn) error {
	iface, err := net.InterfaceByName(device)
	if err != nil {
		return err
	}
	var setErr error
	err = raw.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, ipBoundIf, iface.Index)
	})
	if err != nil {
		return err
	}
	if setErr != nil {
		return fmt.Errorf("bind socket to %s: %w", device, setErr)
	}
	return nil
}

func bindDeviceSocket(device string, raw syscall.RawConn) error { return bindSocket(device, raw) }

func setupDeviceRoute(_ string) (func(), error) { return func() {}, nil }
