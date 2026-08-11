package proxy

import (
	"net"

	"conduitvpn/internal/egress"
)

// tunnelResolver queries a fixed public DNS server through the controller,
// keeping DNS subject to the same tunnel and fail-closed policy as TCP.
func tunnelResolver(controller *egress.Controller, server string) *net.Resolver {
	return controller.Resolver(server)
}
