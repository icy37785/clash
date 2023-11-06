package socks

import (
	"io"
	"net"

	"github.com/icy37785/clash/adapter/inbound"
	N "github.com/icy37785/clash/common/net"
	C "github.com/icy37785/clash/constant"
	authStore "github.com/icy37785/clash/listener/auth"
	"github.com/icy37785/clash/transport/socks4"
	"github.com/icy37785/clash/transport/socks5"
)

type Listener struct {
	listener net.Listener
	addr     string
	closed   bool
}

// RawAddress implements C.Listener
func (l *Listener) RawAddress() string {
	return l.addr
}

// Address implements C.Listener
func (l *Listener) Address() string {
	return l.listener.Addr().String()
}

// Close implements C.Listener
func (l *Listener) Close() error {
	l.closed = true
	return l.listener.Close()
}

func New(addr string, in chan<- C.ConnContext) (C.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	sl := &Listener{
		listener: l,
		addr:     addr,
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				if sl.closed {
					break
				}
				continue
			}
			go handleSocks(c, in)
		}
	}()

	return sl, nil
}

func handleSocks(conn net.Conn, in chan<- C.ConnContext) {
	_ = conn.(*net.TCPConn).SetKeepAlive(true)
	bufConn := N.NewBufferedConn(conn)
	head, err := bufConn.Peek(1)
	if err != nil {
		_ = conn.Close()
		return
	}

	switch head[0] {
	case socks4.Version:
		HandleSocks4(bufConn, in)
	case socks5.Version:
		HandleSocks5(bufConn, in)
	default:
		_ = conn.Close()
	}
}

func HandleSocks4(conn net.Conn, in chan<- C.ConnContext) {
	addr, _, err := socks4.ServerHandshake(conn, authStore.Authenticator())
	if err != nil {
		_ = conn.Close()
		return
	}
	in <- inbound.NewSocket(socks5.ParseAddr(addr), conn, C.SOCKS4)
}

func HandleSocks5(conn net.Conn, in chan<- C.ConnContext) {
	target, command, err := socks5.ServerHandshake(conn, authStore.Authenticator())
	if err != nil {
		_ = conn.Close()
		return
	}
	if command == socks5.CmdUDPAssociate {
		defer func(conn net.Conn) {
			_ = conn.Close()
		}(conn)
		io.Copy(io.Discard, conn)
		return
	}
	in <- inbound.NewSocket(target, conn, C.SOCKS5)
}
