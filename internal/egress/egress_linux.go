//go:build linux

package egress

import (
	"fmt"
	"os/exec"
	"syscall"
)

const (
	routeMark          = "0xc0de"
	routeTable         = "51820"
	routePriority      = "10001"
	probeRouteMark     = "0xc0df"
	probeRouteTable    = "51821"
	probeRoutePriority = "10002"
)

func setupHostRoute(device string) error {
	// These identifiers are reserved for conduitvpn. Clear stale rules from a
	// previous ungraceful exit before adding the current tunnel route.
	clearHostRoute()
	commands := hostRouteCommands(device)
	if err := runIP(commands[0]...); err != nil {
		return fmt.Errorf("install tunnel route: %w", err)
	}
	if err := runIP(commands[1]...); err != nil {
		clearHostRoute()
		return fmt.Errorf("install tunnel policy rule: %w", err)
	}
	return nil
}

func clearHostRoute() {
	for _, args := range hostRouteCleanupCommands() {
		_ = runIP(args...)
	}
}

func hostRouteCommands(device string) [][]string {
	return [][]string{
		{"route", "replace", "default", "dev", device, "table", routeTable},
		{"rule", "add", "fwmark", routeMark, "table", routeTable, "priority", routePriority},
	}
}

func hostRouteCleanupCommands() [][]string {
	return [][]string{
		{"rule", "del", "fwmark", routeMark, "table", routeTable, "priority", routePriority},
		{"route", "flush", "table", routeTable},
	}
}

func runIP(args ...string) error {
	return exec.Command("ip", args...).Run()
}

func bindSocket(_ string, raw syscall.RawConn) error {
	return markSocket(raw, 0xc0de)
}

func bindDeviceSocket(_ string, raw syscall.RawConn) error {
	return markSocket(raw, 0xc0df)
}

func markSocket(raw syscall.RawConn, mark int) error {
	var setErr error
	err := raw.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
	})
	if err != nil {
		return err
	}
	if setErr != nil {
		return fmt.Errorf("mark tunnel socket: %w", setErr)
	}
	return nil
}

func setupDeviceRoute(device string) (func(), error) {
	clearDeviceRoute()
	commands := [][]string{
		{"route", "replace", "default", "dev", device, "table", probeRouteTable},
		{"rule", "add", "fwmark", probeRouteMark, "table", probeRouteTable, "priority", probeRoutePriority},
	}
	if err := runIP(commands[0]...); err != nil {
		return nil, fmt.Errorf("install verification route: %w", err)
	}
	if err := runIP(commands[1]...); err != nil {
		clearDeviceRoute()
		return nil, fmt.Errorf("install verification policy rule: %w", err)
	}
	return clearDeviceRoute, nil
}

func clearDeviceRoute() {
	for _, args := range [][]string{
		{"rule", "del", "fwmark", probeRouteMark, "table", probeRouteTable, "priority", probeRoutePriority},
		{"route", "flush", "table", probeRouteTable},
	} {
		_ = runIP(args...)
	}
}

func switchHostRoute(device string) error {
	if err := runIP(switchHostRouteArgs(device)...); err != nil {
		return fmt.Errorf("switch tunnel route: %w", err)
	}
	return nil
}

func switchHostRouteArgs(device string) []string {
	return []string{"route", "replace", "default", "dev", device, "table", routeTable}
}

func replaceDefaultDev(device string) error {
	if err := runIP(replaceDefaultDevArgs(device)...); err != nil {
		return fmt.Errorf("replace default route: %w", err)
	}
	return nil
}

func replaceDefaultDevArgs(device string) []string {
	return []string{"route", "replace", "default", "dev", device}
}
