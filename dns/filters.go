package dns

import (
	"net"
	"strings"

	"github.com/icy37785/clash/component/mmdb"
	"github.com/icy37785/clash/component/trie"
)

type fallbackIPFilter interface {
	Match(net.IP) bool
}

type geoipFilter struct {
	code string
}

func (gf *geoipFilter) Match(ip net.IP) bool {
	record, _ := mmdb.Instance().Country(ip)
	return !strings.EqualFold(record.Country.IsoCode, gf.code) && !ip.IsPrivate()
}

type ipnetFilter struct {
	ipnet *net.IPNet
}

func (inf *ipnetFilter) Match(ip net.IP) bool {
	return inf.ipnet.Contains(ip)
}

type fallbackDomainFilter interface {
	Match(domain string) bool
}

type DomainFilter struct {
	tree *trie.DomainTrie
}

func NewDomainFilter(domains []string) *DomainFilter {
	df := DomainFilter{tree: trie.New()}
	for _, domain := range domains {
		_ = df.tree.Insert(domain, "")
	}
	return &df
}

func (df *DomainFilter) Match(domain string) bool {
	return df.tree.Search(domain) != nil
}
