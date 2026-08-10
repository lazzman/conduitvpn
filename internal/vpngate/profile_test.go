package vpngate

import "testing"

const safeProfile = `client
dev tun
proto udp
remote 8.8.8.8 1194 udp
resolv-retry infinite
nobind
persist-key
persist-tun
auth-nocache
remote-cert-tls server
cipher AES-256-GCM
auth SHA256
verb 3
<ca>
-----BEGIN CERTIFICATE-----
MIIB
-----END CERTIFICATE-----
</ca>
`

func TestValidateOpenVPNProfileAcceptsSafeProfile(t *testing.T) {
	got, err := ValidateOpenVPNProfile(safeProfile, "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("validated profile is empty")
	}
}

func TestValidateOpenVPNProfileRejectsUnsafeDirectives(t *testing.T) {
	for _, profile := range []string{
		safeProfile + "script-security 2\nup /bin/sh\n",
		safeProfile + "plugin /tmp/evil.so\n",
		safeProfile + "management 127.0.0.1 7505\n",
		safeProfile + "ca /etc/passwd\n",
	} {
		if _, err := ValidateOpenVPNProfile(profile, "8.8.8.8"); err == nil {
			t.Fatalf("unsafe profile accepted: %q", profile)
		}
	}
}

func TestValidateOpenVPNProfileRejectsUnexpectedRemote(t *testing.T) {
	if _, err := ValidateOpenVPNProfile(safeProfile, "1.1.1.1"); err == nil {
		t.Fatal("mismatched remote should be rejected")
	}
	private := "client\ndev tun\nremote 10.0.0.1 1194 udp\n"
	if _, err := ValidateOpenVPNProfile(private, "10.0.0.1"); err == nil {
		t.Fatal("private remote should be rejected")
	}
}
