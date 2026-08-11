//go:build linux

package egress

import (
	"fmt"
	"os/exec"
	"syscall"
)

const (
	routeMark     = "0xc0de"
	routeTable    = "51820"
	routePriority = "10001"
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
	var setErr error
	err := raw.Control(func(fd uintptr) {
		// SO_MARK is evaluated by the policy rule above; only dials issued
		// through Controller receive it, so host traffic is unaffected.
		setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, 0xc0de)
	})
	if err != nil {
		return err
	}
	if setErr != nil {
		return fmt.Errorf("mark tunnel socket: %w", setErr)
	}
	return nil
}
