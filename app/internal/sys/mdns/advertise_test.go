package mdns

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	hmdns "github.com/hashicorp/mdns"
	"github.com/miekg/dns"
)

func testLink(index int, address string) lanInterface {
	ip, network, _ := net.ParseCIDR(address)
	network.IP = ip
	return lanInterface{
		iface:    net.Interface{Index: index, Name: "lan"},
		ips:      []net.IP{ip.To4()},
		networks: []*net.IPNet{network},
	}
}

func testResponder(t *testing.T) *responder {
	t.Helper()
	link := testLink(1, "192.0.2.10/24")
	zone, err := hmdns.NewMDNSService("care", "_https._tcp", "local.", "care.local.", 443, link.ips, []string{"CARE Clinic"})
	if err != nil {
		t.Fatal(err)
	}
	return &responder{link: link, zone: zone}
}

func TestHostnameReplyRoutingAndFraming(t *testing.T) {
	s := testResponder(t)
	for _, tc := range []struct {
		name    string
		port    int
		unicast bool
		host    string
		qtype   uint16
	}{
		{"multicast", 5353, false, "care.local.", dns.TypeA},
		{"QU", 5353, true, "care.local.", dns.TypeA},
		{"legacy", 12345, false, "care.local.", dns.TypeA},
		{"legacy-QU", 12345, true, "care.local.", dns.TypeA},
		{"case-insensitive", 5353, false, "CARE.LOCAL.", dns.TypeA},
		{"ANY", 5353, false, "care.local.", dns.TypeANY},
		{"IPv6-negative-answer", 5353, false, "care.local.", dns.TypeAAAA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := new(dns.Msg)
			query.SetQuestion(tc.host, tc.qtype)
			query.Id = 123
			if tc.unicast {
				query.Question[0].Qclass |= 1 << 15
			}
			from := &net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: tc.port}
			replies := s.replies(query, from)
			if len(replies) != 1 {
				t.Fatalf("expected one reply, got %d", len(replies))
			}
			reply := replies[0]
			if reply.to.IP.IsMulticast() != (tc.port == 5353 && !tc.unicast) {
				t.Fatalf("wrong destination %s", reply.to)
			}
			if !reply.to.IP.IsMulticast() && reply.to.String() != from.String() {
				t.Fatal("unicast response did not target requester")
			}
			if !reply.msg.Response || !reply.msg.Authoritative || reply.msg.Rcode != dns.RcodeSuccess {
				t.Fatalf("incorrect response header: %+v", reply.msg.MsgHdr)
			}
			if tc.port != 5353 {
				if reply.msg.Id != query.Id || len(reply.msg.Question) != 1 || reply.msg.Question[0] != query.Question[0] {
					t.Fatal("legacy reply must repeat ID and question")
				}
			} else if reply.msg.Id != 0 || len(reply.msg.Question) != 0 {
				t.Fatal("mDNS reply must have zero ID and no question")
			}
			address, negative := false, false
			for _, rr := range append(append([]dns.RR{}, reply.msg.Answer...), reply.msg.Extra...) {
				if (rr.Header().Class&(1<<15) != 0) != (tc.port == 5353) {
					t.Fatal("incorrect cache-flush bit")
				}
				if tc.port != 5353 && rr.Header().Ttl > 10 {
					t.Fatal("legacy TTL exceeds 10 seconds")
				}
				switch rr := rr.(type) {
				case *dns.A:
					address = rr.A.Equal(s.link.ips[0])
				case *dns.NSEC:
					negative = len(rr.TypeBitMap) == 2 && rr.TypeBitMap[0] == dns.TypeA
				}
			}
			for _, rr := range reply.msg.Answer {
				if _, ok := rr.(*dns.NSEC); ok {
					t.Fatal("NSEC must travel in the additional section (RFC 6762 6.1)")
				}
			}
			if tc.qtype == dns.TypeAAAA {
				if address || !negative {
					t.Fatal("IPv4-only host must explicitly deny AAAA")
				}
			} else if !address {
				t.Fatal("no interface-local hostname address")
			}
		})
	}
}

func TestResponderIgnoresUnrelatedAndKnownAnswers(t *testing.T) {
	s := testResponder(t)
	from := &net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 5353}
	query := new(dns.Msg)
	query.SetQuestion("other.local.", dns.TypeA)
	if len(s.replies(query, from)) != 0 {
		t.Fatal("answered unrelated hostname")
	}
	query.SetQuestion("care.local.", dns.TypeA)
	reply := s.replies(query, from)[0].msg
	if len(s.replies(reply, from)) != 0 {
		t.Fatal("answered a response packet")
	}
	query.Answer = reply.Answer
	if len(s.replies(query, from)) != 0 {
		t.Fatal("repeated sufficiently fresh known answers")
	}
	for _, rr := range query.Answer {
		rr.Header().Ttl = 1
	}
	if len(s.replies(query, from)) == 0 {
		t.Fatal("failed to refresh expiring known answers")
	}
	query.Answer = nil
	query.Question[0].Qclass = dns.ClassCHAOS
	if len(s.replies(query, from)) != 0 {
		t.Fatal("answered unsupported question class")
	}
	if s.onLink(2, from) ||
		s.onLink(0, &net.UDPAddr{IP: net.ParseIP("198.51.100.20")}) || !s.onLink(0, from) {
		t.Fatal("incorrect receiving-interface filtering")
	}
	from = &net.UDPAddr{IP: net.ParseIP("fe80::20"), Port: 5353, Zone: "lan"}
	query.Question[0].Qclass = dns.ClassINET
	if !s.onLink(0, from) || !s.replies(query, from)[0].to.IP.Equal(multicastAddr6.IP) {
		t.Fatal("IPv6 transport must use its receiving interface and multicast group")
	}
}

func TestHostnameProbeRejectsStaleAndUnrelatedResponses(t *testing.T) {
	s := testResponder(t)
	query := new(dns.Msg)
	query.SetQuestion("care.local.", dns.TypeA)
	reply := s.replies(query, &net.UDPAddr{IP: net.ParseIP("192.0.2.20"), Port: 12345})[0].msg
	if !matchesHostname(reply, query, s.link.ips) {
		t.Fatal("valid direct hostname response rejected")
	}
	for _, change := range []func(*dns.Msg){
		func(m *dns.Msg) { m.Id++ },
		func(m *dns.Msg) { m.Question = nil },
		func(m *dns.Msg) { m.Response = false },
		func(m *dns.Msg) { m.Answer = nil },
		func(m *dns.Msg) { m.Answer[0].(*dns.A).A = net.ParseIP("127.0.0.1") },
		func(m *dns.Msg) { m.Answer[0].(*dns.A).A = net.ParseIP("192.0.2.11") },
		func(m *dns.Msg) { m.Answer[0].Header().Name = "other.local." },
		func(m *dns.Msg) { m.Answer[0].Header().Ttl = 0 },
	} {
		other := reply.Copy()
		change(other)
		if matchesHostname(other, query, s.link.ips) {
			t.Fatal("invalid hostname response accepted")
		}
	}
}

func TestResolvesChecksEveryInterfaceWithDeadline(t *testing.T) {
	for _, fail := range []bool{false, true} {
		a := &Advertiser{
			name: "care", servers: []*responder{{}},
			links: []lanInterface{testLink(1, "192.0.2.10/24"), testLink(2, "198.51.100.10/24")},
		}
		err := a.resolves(func(ctx context.Context, host string, link lanInterface) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 2*time.Second || host != "care.local." {
				t.Error("incorrect probe hostname or deadline")
			}
			if fail && link.iface.Index == 2 {
				return errors.New("unreachable")
			}
			return nil
		})
		if (err != nil) != fail {
			t.Fatalf("Resolves error = %v, failure expected = %v", err, fail)
		}
	}
}

func TestStoppedAdvertiserDoesNotResolve(t *testing.T) {
	for _, a := range []*Advertiser{nil, {name: "care"}} {
		if a.resolves(func(context.Context, string, lanInterface) error {
			t.Error("queried for a stopped advertiser")
			return nil
		}) == nil {
			t.Fatal("stopped advertiser resolved")
		}
	}
	a := &Advertiser{name: "care", servers: []*responder{{}}, links: []lanInterface{testLink(1, "192.0.2.10/24")}}
	if a.resolves(func(context.Context, string, lanInterface) error {
		a.mu.Lock()
		a.servers = nil
		a.mu.Unlock()
		return nil
	}) == nil {
		t.Fatal("shutdown during query must not report success")
	}
}

func TestTopologyChangesAndNameValidation(t *testing.T) {
	first, second := testLink(1, "192.0.2.10/24"), testLink(2, "198.51.100.10/24")
	if !sameLinks([]lanInterface{first, second}, []lanInterface{second, first}) {
		t.Fatal("reordering must not restart discovery")
	}
	for _, changed := range []lanInterface{
		testLink(2, "192.0.2.10/24"),
		testLink(1, "192.0.2.11/24"),
		testLink(1, "192.0.2.10/16"),
	} {
		if sameLinks([]lanInterface{first}, []lanInterface{changed}) {
			t.Fatal("interface, address or subnet change went undetected")
		}
	}
	if sameLinks([]lanInterface{first}, nil) {
		t.Fatal("interface removal went undetected")
	}
	for _, name := range []string{"", "-care", "care.invalid", "care with spaces"} {
		if _, err := Advertise(name, nil); err == nil {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
}
