//go:build linux

package egress

import (
	"reflect"
	"testing"
)

func TestHostRouteCommands(t *testing.T) {
	want := [][]string{
		{"route", "replace", "default", "dev", "tun0", "table", routeTable},
		{"rule", "add", "fwmark", routeMark, "table", routeTable, "priority", routePriority},
	}
	if got := hostRouteCommands("tun0"); !reflect.DeepEqual(got, want) {
		t.Fatalf("host route commands = %#v, want %#v", got, want)
	}
	cleanup := hostRouteCleanupCommands()
	if len(cleanup) != 2 || cleanup[0][0] != "rule" || cleanup[1][0] != "route" {
		t.Fatalf("unexpected cleanup commands: %#v", cleanup)
	}
}
