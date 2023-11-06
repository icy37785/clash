package statistic

import (
	"net"
	"time"

	C "github.com/icy37785/clash/constant"

	"github.com/gofrs/uuid/v5"
	"go.uber.org/atomic"
)

type tracker interface {
	ID() string
	Close() error
}

type trackerInfo struct {
	UUID          uuid.UUID     `json:"id"`
	Metadata      *C.Metadata   `json:"metadata"`
	UploadTotal   *atomic.Int64 `json:"upload"`
	DownloadTotal *atomic.Int64 `json:"download"`
	Start         time.Time     `json:"start"`
	Chain         C.Chain       `json:"chains"`
	Rule          string        `json:"rule"`
	RulePayload   string        `json:"rulePayload"`
}

type TcpTracker struct {
	C.Conn `json:"-"`
	*trackerInfo
	manager *Manager
}

func (tt *TcpTracker) ID() string {
	return tt.UUID.String()
}

func (tt *TcpTracker) Read(b []byte) (int, error) {
	n, err := tt.Conn.Read(b)
	download := int64(n)
	tt.manager.PushDownloaded(download)
	tt.DownloadTotal.Add(download)
	return n, err
}

func (tt *TcpTracker) Write(b []byte) (int, error) {
	n, err := tt.Conn.Write(b)
	upload := int64(n)
	tt.manager.PushUploaded(upload)
	tt.UploadTotal.Add(upload)
	return n, err
}

func (tt *TcpTracker) Close() error {
	tt.manager.Leave(tt)
	return tt.Conn.Close()
}

func NewTCPTracker(conn C.Conn, manager *Manager, metadata *C.Metadata, rule C.Rule) *TcpTracker {
	_uuid, _ := uuid.NewV4()

	t := &TcpTracker{
		Conn:    conn,
		manager: manager,
		trackerInfo: &trackerInfo{
			UUID:          _uuid,
			Start:         time.Now(),
			Metadata:      metadata,
			Chain:         conn.Chains(),
			Rule:          "",
			UploadTotal:   atomic.NewInt64(0),
			DownloadTotal: atomic.NewInt64(0),
		},
	}

	if rule != nil {
		t.trackerInfo.Rule = rule.RuleType().String()
		t.trackerInfo.RulePayload = rule.Payload()
	}

	manager.Join(t)
	return t
}

type UdpTracker struct {
	C.PacketConn `json:"-"`
	*trackerInfo
	manager *Manager
}

func (ut *UdpTracker) ID() string {
	return ut.UUID.String()
}

func (ut *UdpTracker) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := ut.PacketConn.ReadFrom(b)
	download := int64(n)
	ut.manager.PushDownloaded(download)
	ut.DownloadTotal.Add(download)
	return n, addr, err
}

func (ut *UdpTracker) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := ut.PacketConn.WriteTo(b, addr)
	upload := int64(n)
	ut.manager.PushUploaded(upload)
	ut.UploadTotal.Add(upload)
	return n, err
}

func (ut *UdpTracker) Close() error {
	ut.manager.Leave(ut)
	return ut.PacketConn.Close()
}

func NewUDPTracker(conn C.PacketConn, manager *Manager, metadata *C.Metadata, rule C.Rule) *UdpTracker {
	_uuid, _ := uuid.NewV4()

	ut := &UdpTracker{
		PacketConn: conn,
		manager:    manager,
		trackerInfo: &trackerInfo{
			UUID:          _uuid,
			Start:         time.Now(),
			Metadata:      metadata,
			Chain:         conn.Chains(),
			Rule:          "",
			UploadTotal:   atomic.NewInt64(0),
			DownloadTotal: atomic.NewInt64(0),
		},
	}

	if rule != nil {
		ut.trackerInfo.Rule = rule.RuleType().String()
		ut.trackerInfo.RulePayload = rule.Payload()
	}

	manager.Join(ut)
	return ut
}
