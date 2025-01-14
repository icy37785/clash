package http

import (
	"net"
	"net/http"
	"strings"

	"github.com/icy37785/clash/adapter/inbound"
	N "github.com/icy37785/clash/common/net"
	C "github.com/icy37785/clash/constant"
	"github.com/icy37785/clash/transport/socks5"
)

func isUpgradeRequest(req *http.Request) bool {
	for _, header := range req.Header["Connection"] {
		for _, elm := range strings.Split(header, ",") {
			if strings.EqualFold(strings.TrimSpace(elm), "Upgrade") {
				return true
			}
		}
	}

	return false
}

func handleUpgrade(conn net.Conn, request *http.Request, in chan<- C.ConnContext) {
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	removeProxyHeaders(request.Header)
	removeExtraHTTPHostPort(request)

	address := request.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "80")
	}

	dstAddr := socks5.ParseAddr(address)
	if dstAddr == nil {
		return
	}

	left, right := net.Pipe()

	in <- inbound.NewHTTP(dstAddr, conn.RemoteAddr(), conn.LocalAddr(), right)

	bufferedLeft := N.NewBufferedConn(left)
	defer func(bufferedLeft *N.BufferedConn) {
		_ = bufferedLeft.Close()
	}(bufferedLeft)

	err := request.Write(bufferedLeft)
	if err != nil {
		return
	}

	resp, err := http.ReadResponse(bufferedLeft.Reader(), request)
	if err != nil {
		return
	}

	removeProxyHeaders(resp.Header)

	err = resp.Write(conn)
	if err != nil {
		return
	}

	if resp.StatusCode == http.StatusSwitchingProtocols {
		N.Relay(bufferedLeft, conn)
	}
}
