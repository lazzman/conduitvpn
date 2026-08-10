// Package proxy implements the single-port dual-protocol listener
// (HTTP + SOCKS5, sniffed from the first byte) and relays connections
// through the tunnel's default route.
package proxy

import (
	"bufio"
	"net"
	"strconv"
	"sync"
	"time"

	"conduitvpn/internal/logx"
)

// Server is a dual-protocol proxy listener.
type Server struct {
	host     string
	port     int
	user     string
	pass     string
	authOn   bool
	dns      string
	maxConns int

	mu       sync.Mutex
	listener net.Listener
	limit    chan struct{}
	closed   bool
}

func New(host string, port int, user, pass, dnsServer string, maxConns int) *Server {
	if maxConns < 1 {
		maxConns = 512
	}
	return &Server{
		host:     host,
		port:     port,
		user:     user,
		pass:     pass,
		authOn:   user != "" || pass != "",
		dns:      dnsServer,
		maxConns: maxConns,
		limit:    make(chan struct{}, maxConns),
	}
}

func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Start begins accepting connections in the background.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	go s.acceptLoop(ln)
	return nil
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.closed {
				return
			}
			logx.Warn("proxy accept failed", "err", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetKeepAlive(true)
			_ = tcp.SetKeepAlivePeriod(60 * time.Second)
		}
		s.limit <- struct{}{} // soft cap to bound FD usage
		go func() {
			defer func() { <-s.limit }()
			s.handle(conn)
		}()
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 32*1024)
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	switch {
	case first[0] == 0x05: // SOCKS5 greeting
		s.handleSocks5(conn, br)
	default:
		s.handleHTTP(conn, br)
	}
}
