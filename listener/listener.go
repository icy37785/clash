package listener

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/icy37785/clash/adapter/inbound"
	"github.com/icy37785/clash/config"
	C "github.com/icy37785/clash/constant"
	"github.com/icy37785/clash/listener/http"
	"github.com/icy37785/clash/listener/mixed"
	"github.com/icy37785/clash/listener/redir"
	"github.com/icy37785/clash/listener/socks"
	"github.com/icy37785/clash/listener/tproxy"
	"github.com/icy37785/clash/listener/tunnel"
	"github.com/icy37785/clash/log"

	"github.com/samber/lo"
)

var (
	allowLan    = false
	bindAddress = "*"

	tcpListeners = map[C.Inbound]C.Listener{}
	udpListeners = map[C.Inbound]C.Listener{}

	tunnelTCPListeners = map[string]*tunnel.Listener{}
	tunnelUDPListeners = map[string]*tunnel.PacketConn{}

	// lock for recreate function
	recreateMux sync.Mutex
	tunnelMux   sync.Mutex
)

type Ports struct {
	Port       int `json:"port"`
	SocksPort  int `json:"socks-port"`
	RedirPort  int `json:"redir-port"`
	TProxyPort int `json:"tproxy-port"`
	MixedPort  int `json:"mixed-port"`
}

var tcpListenerCreators = map[C.InboundType]tcpListenerCreator{
	C.InboundTypeHTTP:   http.New,
	C.InboundTypeSocks:  socks.New,
	C.InboundTypeRedir:  redir.New,
	C.InboundTypeTproxy: tproxy.New,
	C.InboundTypeMixed:  mixed.New,
}

var udpListenerCreators = map[C.InboundType]udpListenerCreator{
	C.InboundTypeSocks:  socks.NewUDP,
	C.InboundTypeRedir:  tproxy.NewUDP,
	C.InboundTypeTproxy: tproxy.NewUDP,
	C.InboundTypeMixed:  socks.NewUDP,
}

type (
	tcpListenerCreator func(addr string, tcpIn chan<- C.ConnContext) (C.Listener, error)
	udpListenerCreator func(addr string, udpIn chan<- *inbound.PacketAdapter) (C.Listener, error)
)

func AllowLan() bool {
	return allowLan
}

func BindAddress() string {
	return bindAddress
}

func SetAllowLan(al bool) {
	allowLan = al
}

func SetBindAddress(host string) {
	bindAddress = host
}

func createListener(inbound C.Inbound, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	addr := inbound.BindAddress
	if portIsZero(addr) {
		return
	}
	tcpCreator := tcpListenerCreators[inbound.Type]
	udpCreator := udpListenerCreators[inbound.Type]
	if tcpCreator == nil && udpCreator == nil {
		log.Errorln("inbound type %s is not supported", inbound.Type)
		return
	}
	if tcpCreator != nil {
		tcpListener, err := tcpCreator(addr, tcpIn)
		if err != nil {
			log.Errorln("create addr %s tcp listener error: %v", addr, err)
			return
		}
		tcpListeners[inbound] = tcpListener
	}
	if udpCreator != nil {
		udpListener, err := udpCreator(addr, udpIn)
		if err != nil {
			log.Errorln("create addr %s udp listener error: %v", addr, err)
			return
		}
		udpListeners[inbound] = udpListener
	}
	log.Infoln("inbound %s created successfully", inbound.ToAlias())
}

func closeListener(inbound C.Inbound) {
	listener := tcpListeners[inbound]
	if listener != nil {
		if err := listener.Close(); err != nil {
			log.Errorln("close tcp address `%s` error: %s", inbound.ToAlias(), err.Error())
		}
		delete(tcpListeners, inbound)
	}
	listener = udpListeners[inbound]
	if listener != nil {
		if err := listener.Close(); err != nil {
			log.Errorln("close udp address `%s` error: %s", inbound.ToAlias(), err.Error())
		}
		delete(udpListeners, inbound)
	}
}

func getNeedCloseAndCreateInbound(originInbounds []C.Inbound, newInbounds []C.Inbound) ([]C.Inbound, []C.Inbound) {
	needCloseMap := map[C.Inbound]bool{}
	var needClose []C.Inbound
	var needCreate []C.Inbound

	for _, _inbound := range originInbounds {
		needCloseMap[_inbound] = true
	}
	for _, _inbound := range newInbounds {
		if needCloseMap[_inbound] {
			delete(needCloseMap, _inbound)
		} else {
			needCreate = append(needCreate, _inbound)
		}
	}
	for _inbound := range needCloseMap {
		needClose = append(needClose, _inbound)
	}
	return needClose, needCreate
}

// ReCreateListeners only recreate inbound config listener
func ReCreateListeners(inbounds []C.Inbound, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	var newInbounds []C.Inbound
	newInbounds = append(newInbounds, inbounds...)
	for _, _inbound := range getInbounds() {
		if _inbound.IsFromPortCfg {
			newInbounds = append(newInbounds, _inbound)
		}
	}
	reCreateListeners(newInbounds, tcpIn, udpIn)
}

// ReCreatePortsListeners only recreate ports config listener
func ReCreatePortsListeners(ports Ports, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	var newInbounds []C.Inbound
	newInbounds = append(newInbounds, GetInbounds()...)
	newInbounds = addPortInbound(newInbounds, C.InboundTypeHTTP, ports.Port)
	newInbounds = addPortInbound(newInbounds, C.InboundTypeSocks, ports.SocksPort)
	newInbounds = addPortInbound(newInbounds, C.InboundTypeRedir, ports.RedirPort)
	newInbounds = addPortInbound(newInbounds, C.InboundTypeTproxy, ports.TProxyPort)
	newInbounds = addPortInbound(newInbounds, C.InboundTypeMixed, ports.MixedPort)
	reCreateListeners(newInbounds, tcpIn, udpIn)
}

func addPortInbound(inbounds []C.Inbound, inboundType C.InboundType, port int) []C.Inbound {
	if port != 0 {
		inbounds = append(inbounds, C.Inbound{
			Type:          inboundType,
			BindAddress:   genAddr(bindAddress, port, allowLan),
			IsFromPortCfg: true,
		})
	}
	return inbounds
}

func reCreateListeners(inbounds []C.Inbound, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	recreateMux.Lock()
	defer recreateMux.Unlock()
	needClose, needCreate := getNeedCloseAndCreateInbound(getInbounds(), inbounds)
	for _, _inbound := range needClose {
		closeListener(_inbound)
	}
	for _, _inbound := range needCreate {
		createListener(_inbound, tcpIn, udpIn)
	}
}

func PatchTunnel(tunnels []config.Tunnel, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	tunnelMux.Lock()
	defer tunnelMux.Unlock()

	type addrProxy struct {
		network string
		addr    string
		target  string
		proxy   string
	}

	tcpOld := lo.Map(
		lo.Keys(tunnelTCPListeners),
		func(key string, _ int) addrProxy {
			parts := strings.Split(key, "/")
			return addrProxy{
				network: "tcp",
				addr:    parts[0],
				target:  parts[1],
				proxy:   parts[2],
			}
		},
	)
	udpOld := lo.Map(
		lo.Keys(tunnelUDPListeners),
		func(key string, _ int) addrProxy {
			parts := strings.Split(key, "/")
			return addrProxy{
				network: "udp",
				addr:    parts[0],
				target:  parts[1],
				proxy:   parts[2],
			}
		},
	)
	oldElm := lo.Union(tcpOld, udpOld)

	newElm := lo.FlatMap(
		tunnels,
		func(tunnel config.Tunnel, _ int) []addrProxy {
			return lo.Map(
				tunnel.Network,
				func(network string, _ int) addrProxy {
					return addrProxy{
						network: network,
						addr:    tunnel.Address,
						target:  tunnel.Target,
						proxy:   tunnel.Proxy,
					}
				},
			)
		},
	)

	needClose, needCreate := lo.Difference(oldElm, newElm)

	for _, elm := range needClose {
		key := fmt.Sprintf("%s/%s/%s", elm.addr, elm.target, elm.proxy)
		if elm.network == "tcp" {
			_ = tunnelTCPListeners[key].Close()
			delete(tunnelTCPListeners, key)
		} else {
			_ = tunnelUDPListeners[key].Close()
			delete(tunnelUDPListeners, key)
		}
	}

	for _, elm := range needCreate {
		key := fmt.Sprintf("%s/%s/%s", elm.addr, elm.target, elm.proxy)
		if elm.network == "tcp" {
			l, err := tunnel.New(elm.addr, elm.target, elm.proxy, tcpIn)
			if err != nil {
				log.Errorln("Start tunnel %s error: %s", elm.target, err.Error())
				continue
			}
			tunnelTCPListeners[key] = l
			log.Infoln("Tunnel(tcp/%s) proxy %s listening at: %s", elm.target, elm.proxy, tunnelTCPListeners[key].Address())
		} else {
			l, err := tunnel.NewUDP(elm.addr, elm.target, elm.proxy, udpIn)
			if err != nil {
				log.Errorln("Start tunnel %s error: %s", elm.target, err.Error())
				continue
			}
			tunnelUDPListeners[key] = l
			log.Infoln("Tunnel(udp/%s) proxy %s listening at: %s", elm.target, elm.proxy, tunnelUDPListeners[key].Address())
		}
	}
}

func GetInbounds() []C.Inbound {
	return lo.Filter(getInbounds(), func(inbound C.Inbound, idx int) bool {
		return !inbound.IsFromPortCfg
	})
}

// GetInbounds return the inbounds of proxy servers
func getInbounds() []C.Inbound {
	var inbounds []C.Inbound
	for _inbound := range tcpListeners {
		inbounds = append(inbounds, _inbound)
	}
	for _inbound := range udpListeners {
		if _, ok := tcpListeners[_inbound]; !ok {
			inbounds = append(inbounds, _inbound)
		}
	}
	return inbounds
}

// GetPorts return the ports of proxy servers
func GetPorts() *Ports {
	ports := &Ports{}
	for _, _inbound := range getInbounds() {
		fillPort(_inbound, ports)
	}
	return ports
}

func fillPort(inbound C.Inbound, ports *Ports) {
	if inbound.IsFromPortCfg {
		port := getPort(inbound.BindAddress)
		switch inbound.Type {
		case C.InboundTypeHTTP:
			ports.Port = port
		case C.InboundTypeSocks:
			ports.SocksPort = port
		case C.InboundTypeTproxy:
			ports.TProxyPort = port
		case C.InboundTypeRedir:
			ports.RedirPort = port
		case C.InboundTypeMixed:
			ports.MixedPort = port
		default:
			// do nothing
		}
	}
}

func portIsZero(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if port == "0" || port == "" || err != nil {
		return true
	}
	return false
}

func genAddr(host string, port int, allowLan bool) string {
	if allowLan {
		if host == "*" {
			return fmt.Sprintf(":%d", port)
		}
		return fmt.Sprintf("%s:%d", host, port)
	}

	return fmt.Sprintf("127.0.0.1:%d", port)
}

func getPort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}
