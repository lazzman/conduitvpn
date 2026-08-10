package proxy

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"conduitvpn/internal/logx"
)

// handleSocks5 implements SOCKS5 with optional RFC 1929 username/password
// auth. Only CONNECT is supported.
func (s *Server) handleSocks5(client net.Conn, br *bufio.Reader) {
	head := make([]byte, 2)
	if _, err := io.ReadFull(br, head); err != nil {
		return
	}
	if head[0] != 0x05 {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}

	var chosen byte
	if s.authOn {
		chosen = 0x02 // username/password
		offered := false
		for _, m := range methods {
			if m == 0x02 {
				offered = true
				break
			}
		}
		if !offered {
			_, _ = client.Write([]byte{0x05, 0xFF})
			return
		}
	} else {
		chosen = 0x00
	}
	_, _ = client.Write([]byte{0x05, chosen})

	if chosen == 0x02 {
		if !s.socksAuth(client, br) {
			return
		}
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil {
		return
	}
	if req[0] != 0x05 || req[1] != 0x01 { // only CONNECT
		_, _ = client.Write(socksReply(0x07, nil)) // command not supported
		return
	}
	host, err := readSocksAddr(br, req[3])
	if err != nil {
		_, _ = client.Write(socksReply(0x08, nil)) // address type not supported
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(br, portBytes); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes)

	upstream, err := s.dial(net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		logx.Debug("socks5 dial failed", "target", net.JoinHostPort(host, fmt.Sprint(port)), "err", err)
		_, _ = client.Write(socksReply(0x05, nil)) // connection refused
		return
	}
	if _, err := client.Write(socksReply(0x00, nil)); err != nil {
		upstream.Close()
		return
	}
	relay(client, upstream)
}

// socksAuth performs RFC 1929 username/password negotiation.
func (s *Server) socksAuth(client net.Conn, br *bufio.Reader) bool {
	head := make([]byte, 2)
	if _, err := io.ReadFull(br, head); err != nil {
		return false
	}
	if head[0] != 0x01 {
		_, _ = client.Write([]byte{0x01, 0x01})
		return false
	}
	uname := make([]byte, int(head[1]))
	if _, err := io.ReadFull(br, uname); err != nil {
		return false
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(br, plen); err != nil {
		return false
	}
	passwd := make([]byte, int(plen[0]))
	if _, err := io.ReadFull(br, passwd); err != nil {
		return false
	}
	if constantTimeEq(string(uname), s.user) && constantTimeEq(string(passwd), s.pass) {
		_, _ = client.Write([]byte{0x01, 0x00})
		return true
	}
	_, _ = client.Write([]byte{0x01, 0x01})
	return false
}

// readSocksAddr reads the destination address per ATYP.
func readSocksAddr(br *bufio.Reader, atyp byte) (string, error) {
	switch atyp {
	case 0x01: // IPv4
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	case 0x03: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(br, l); err != nil {
			return "", err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		return string(b), nil
	case 0x04: // IPv6
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	default:
		return "", fmt.Errorf("unknown atyp %d", atyp)
	}
}

func socksReply(code byte, bound net.IP) []byte {
	if bound == nil {
		bound = net.IPv4zero
	}
	b := []byte{0x05, code, 0x00, 0x01}
	b = append(b, bound.To4()...)
	b = append(b, 0, 0)
	return b
}
