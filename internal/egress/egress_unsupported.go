//go:build !linux && !darwin

package egress

import (
	"fmt"
	"syscall"
)

func setupHostRoute(_ string) error {
	return fmt.Errorf("NETWORK_MODE=host is only supported on Linux and macOS")
}

func clearHostRoute() {}

func bindSocket(_ string, _ syscall.RawConn) error {
	return fmt.Errorf("NETWORK_MODE=host is only supported on Linux and macOS")
}
