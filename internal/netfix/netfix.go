// Package netfix repairs reply routing under 方案 B.
//
// With redirect-gateway the container's /1 routes send *all* traffic
// through tun0 — including the HTTP responses of inbound services
// (web UI, proxy listener). External clients then never see a reply.
//
// The fix marks inbound connections to the service ports and routes the
// replies of those connections via the docker bridge (eth0), while
// freshly-initiated outbound traffic (the proxy) keeps egressing through
// the VPN via the main table.
package netfix

import (
	"fmt"
	"os/exec"
	"strings"

	"conduitvpn/internal/logx"
)

const mark = "0x1"
const table = "101"

// Apply is idempotent: it only adds rules that are missing. Failures are
// logged as warnings — the app still runs, only inbound replies degrade.
// tcpPorts/udpPorts are the service ports whose reply traffic must go via
// the docker bridge instead of the tunnel.
func Apply(tcpPorts, udpPorts []string) error {
	gw, dev, err := defaultGateway()
	if err != nil {
		return err
	}

	enable := func(rule []string) {
		if err := exec.Command("iptables", append([]string{"-t", "mangle", "-C"}, rule...)...).Run(); err != nil {
			if cerr := exec.Command("iptables", append([]string{"-t", "mangle", "-A"}, rule...)...).Run(); cerr != nil {
				logx.Warn("netfix iptables add failed", "rule", strings.Join(rule, " "), "err", cerr)
			}
		}
	}

	// mark inbound service connections (TCP + optional UDP), then mark
	// their replies.
	if len(tcpPorts) > 0 {
		enable([]string{"PREROUTING", "-i", dev, "-p", "tcp", "-m", "multiport", "--dports", strings.Join(tcpPorts, ","), "-j", "CONNMARK", "--set-mark", mark})
	}
	if len(udpPorts) > 0 {
		enable([]string{"PREROUTING", "-i", dev, "-p", "udp", "-m", "multiport", "--dports", strings.Join(udpPorts, ","), "-j", "CONNMARK", "--set-mark", mark})
	}
	enable([]string{"OUTPUT", "-m", "connmark", "--mark", mark, "-j", "MARK", "--set-mark", mark})

	// ip: route marked replies via the docker gateway.
	if err := exec.Command("ip", "rule", "add", "fwmark", mark, "table", table, "pref", "100").Run(); err != nil {
		logx.Warn("netfix ip rule (may already exist)", "err", err)
	}
	if err := exec.Command("ip", "route", "add", "default", "via", gw, "dev", dev, "table", table).Run(); err != nil {
		logx.Warn("netfix ip route (may already exist)", "err", err)
	}
	return nil
}

// defaultGateway extracts the container's default gateway + interface,
// e.g. "default via 172.17.0.1 dev eth0".
func defaultGateway() (string, string, error) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", "", fmt.Errorf("ip route show default: %w", err)
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			gw := fields[i+1]
			for j := i + 2; j+1 < len(fields); j++ {
				if fields[j] == "dev" {
					return gw, fields[j+1], nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("no default gateway found in %q", strings.TrimSpace(string(out)))
}
