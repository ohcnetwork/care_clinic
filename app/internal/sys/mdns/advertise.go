package mdns

import (
	"context"
	"fmt"
	"maps"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Advertiser struct {
	name    string
	links   []lanInterface
	mu      sync.Mutex
	servers []*responder
}

func Advertise(name string, logf func(string)) (*Advertiser, error) {
	name = Label(name)
	if err := ValidateLabel(name); err != nil {
		return nil, err
	}
	links, err := lanInterfaces()
	if err != nil {
		return nil, err
	}
	if err := checkAvailable(name+".local.", links, probeConflict); err != nil {
		return nil, err
	}
	a := &Advertiser{name: name, links: links}
	for _, link := range links {
		server, err := newResponder(name, link, logf)
		if err != nil {
			a.Stop()
			return nil, fmt.Errorf("advertise on %s: %w", link.iface.Name, err)
		}
		a.servers = append(a.servers, server)
	}
	return a, nil
}

func (a *Advertiser) Name() string { return a.name }

func (a *Advertiser) Stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, server := range a.servers {
		server.Shutdown()
	}
	a.servers = nil
}

func (a *Advertiser) IPsChanged() (bool, error) {
	cur, err := lanInterfaces()
	if err != nil {
		return false, err
	}
	return !sameLinks(a.links, cur), nil
}

func (a *Advertiser) Resolves() error {
	return a.resolves(probeHostname)
}

func (a *Advertiser) resolves(query func(context.Context, string, lanInterface) error) error {
	if a == nil || a.stopped() {
		return fmt.Errorf("name responder is stopped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := make(chan error, len(a.links))
	for _, link := range a.links {
		go func() {
			if err := query(ctx, a.name+".local.", link); err != nil {
				results <- fmt.Errorf("%s: %w", link.iface.Name, err)
			} else {
				results <- nil
			}
		}()
	}
	for range a.links {
		select {
		case err := <-results:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.stopped() {
		return fmt.Errorf("name responder stopped during query")
	}
	return nil
}

func (a *Advertiser) stopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.servers) == 0
}

type NameStatus struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func Label(name string) string {
	name = strings.ToLower(strings.Trim(strings.TrimSpace(name), "."))
	name = strings.TrimSuffix(name, ".local")
	return strings.Trim(name, ".")
}

var labelRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

func ValidateLabel(name string) error {
	label := strings.ToLower(Label(name))
	switch {
	case label == "":
		return fmt.Errorf("enter a name, for example care")
	case len(label) > 63:
		return fmt.Errorf("name is too long (63 characters at most)")
	case !labelRe.MatchString(label):
		return fmt.Errorf("use lowercase letters, numbers and hyphens only, for example care-test")
	}
	return nil
}

var virtualIface = []string{"docker", "br-", "veth", "virbr", "vboxnet", "vmnet", "utun", "tun", "tap"}

func lanInterfaces() ([]lanInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var links []lanInterface
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 ||
			ifi.Flags&net.FlagMulticast == 0 ||
			ifi.Flags&net.FlagLoopback != 0 ||
			ifi.Flags&net.FlagPointToPoint != 0 ||
			isVirtual(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			return nil, fmt.Errorf("read addresses on %s: %w", ifi.Name, err)
		}
		link := lanInterface{iface: ifi}
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if network.IP.To4() == nil && !network.IP.IsLoopback() && !network.IP.IsUnspecified() {
				link.ipv6 = true
				link.ips6 = append(link.ips6, network.IP)
			}
			if ip4 := network.IP.To4(); ip4 != nil && ip4.IsGlobalUnicast() && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
				link.ips = append(link.ips, ip4)
				link.networks = append(link.networks, network)
			}
		}
		if len(link.ips) > 0 {
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("no multicast-capable LAN IPv4 interface found")
	}
	return links, nil
}

func isVirtual(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range virtualIface {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func sameLinks(a, b []lanInterface) bool {
	snapshot := func(links []lanInterface) map[string]bool {
		keys := make(map[string]bool)
		for _, link := range links {
			for _, network := range link.networks {
				keys[fmt.Sprintf("%d/%s/%t/%s", link.iface.Index, link.iface.Name, link.ipv6, network)] = true
			}
		}
		return keys
	}
	return maps.Equal(snapshot(a), snapshot(b))
}
