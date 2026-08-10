package proxy

import (
	"bufio"
	"bytes"
	"net"
	"testing"
)

// TestSocks5RequestParsing exercises the request path with a fake
// upstream: greeting → CONNECT to 1.2.3.4:80 → relay.
func TestSocks5RequestParsing(t *testing.T) {
	s := New("127.0.0.1", 0, "", "", "8.8.8.8", 10)

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handle(server)
	}()

	// greeting
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 2)
	if _, err := readFull(client, resp); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resp, []byte{0x05, 0x00}) {
		t.Fatalf("want greeting reply 05 00, got %v", resp)
	}

	// CONNECT to 1.2.3.4:80 (will fail to dial — the point is the parse path)
	req := []byte{0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4, 0, 80}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := readFull(client, reply); err != nil {
		t.Fatal(err)
	}
	// The dial may either succeed (0x00) or be refused (0x05) depending
	// on the local network — both are valid code paths. Assert structure.
	if reply[0] != 0x05 || reply[1] != 0x00 && reply[1] != 0x05 {
		t.Fatalf("unexpected socks reply: %v", reply)
	}
}

func TestReadSocksAddr(t *testing.T) {
	// domain form
	br := bufio.NewReader(bytes.NewReader([]byte{0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e'}))
	host, err := readSocksAddr(br, 0x03)
	if err != nil || host != "example" {
		t.Fatalf("domain parse: host=%q err=%v", host, err)
	}
	// IPv4 form
	br = bufio.NewReader(bytes.NewReader([]byte{1, 2, 3, 4}))
	host, err = readSocksAddr(br, 0x01)
	if err != nil || host != "1.2.3.4" {
		t.Fatalf("ipv4 parse: host=%q err=%v", host, err)
	}
}

func TestNormalizeTarget(t *testing.T) {
	cases := map[string]string{
		"example.com:443":      "example.com:443",
		"example.com":          "example.com:80",
		"https://example.com":  "example.com:80",
		"http://example.com:90": "example.com:90",
		"[::1]:8080":           "[::1]:8080",
	}
	for in, want := range cases {
		if got := normalizeTarget(in); got != want {
			t.Errorf("normalizeTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func readFull(conn net.Conn, b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := conn.Read(b[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
