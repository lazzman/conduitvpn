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
func Apply(ports []string) error {
	gw, dev, err := defaultGateway()
	if err != nil {
		return err
	}
	portSpec := strings.Join(ports, ",")

	// iptables: mark inbound service connections, then mark their replies.
	// Note: -t mangle must precede -C/-A (iptables consumes the next token
	// as the chain name).
	for _, rule := range [][]string{
		{"PREROUTING", "-i", dev, "-p", "tcp", "-m", "multiport", "--dports", portSpec, "-j", "CONNMARK", "--set-mark", mark},
		{"OUTPUT", "-m", "connmark", "--mark", mark, "-j", "MARK", "--set-mark", mark},
	} {
		if err := exec.Command("iptables", append([]string{"-t", "mangle", "-C"}, rule...)...).Run(); err != nil {
			if cerr := exec.Command("iptables", append([]string{"-t", "mangle", "-A"}, rule...)...).Run(); cerr != nil {
				logx.Warn("netfix iptables add failed", "rule", strings.Join(rule, " "), "err", cerr)
			}
		}
	}

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
