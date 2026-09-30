package mdns

import (
	"errors"
	"fmt"
	"log"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	hmdns "github.com/hashicorp/mdns"
	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var multicastAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
var multicastAddr6 = &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353}

type lanInterface struct {
	iface    net.Interface
	ips      []net.IP
	ips6     []net.IP
	networks []*net.IPNet
	ipv6     bool
}

type responder struct {
	link  lanInterface
	zone  *hmdns.MDNSService
	conn  *ipv4.PacketConn
	conn6 *ipv6.PacketConn
	done  chan struct{}
	once  sync.Once
	wg    sync.WaitGroup
	logf  func(string)
}

func newResponder(name string, link lanInterface, logf func(string)) (*responder, error) {
	if logf == nil {
		logf = func(message string) { log.Print(message) }
	}
	zone, err := hmdns.NewMDNSService(name, "_https._tcp", "local.", name+".local.", 443, link.ips, []string{"CARE Desktop"})
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenMulticastUDP("udp4", &link.iface, multicastAddr)
	if err != nil {
		return nil, err
	}
	conn := ipv4.NewPacketConn(udp)
	for _, err := range []error{
		conn.SetMulticastInterface(&link.iface),
		conn.SetTTL(255),
		conn.SetMulticastTTL(255),
		conn.SetMulticastLoopback(true),
	} {
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	if err := conn.SetControlMessage(ipv4.FlagInterface, true); err != nil && runtime.GOOS != "windows" {
		_ = conn.Close()
		return nil, err
	}
	s := &responder{link: link, zone: zone, conn: conn, done: make(chan struct{}), logf: logf}
	if link.ipv6 {
		udp6, err := net.ListenMulticastUDP("udp6", &link.iface, multicastAddr6)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		s.conn6 = ipv6.NewPacketConn(udp6)
		for _, err := range []error{
			s.conn6.SetMulticastInterface(&link.iface),
			s.conn6.SetHopLimit(255),
			s.conn6.SetMulticastHopLimit(255),
			s.conn6.SetMulticastLoopback(true),
		} {
			if err != nil {
				_ = s.conn6.Close()
				_ = conn.Close()
				return nil, err
			}
		}
		if err := s.conn6.SetControlMessage(ipv6.FlagInterface, true); err != nil && runtime.GOOS != "windows" {
			_ = s.conn6.Close()
			_ = conn.Close()
			return nil, err
		}
	}
	if err := s.announce(120); err != nil {
		if s.conn6 != nil {
			_ = s.conn6.Close()
		}
		_ = conn.Close()
		return nil, err
	}
	s.wg.Add(2)
	go s.serve(func(buf []byte) (int, int, net.Addr, error) {
		n, control, source, err := conn.ReadFrom(buf)
		index := 0
		if control != nil {
			index = control.IfIndex
		}
		return n, index, source, err
	})
	if s.conn6 != nil {
		s.wg.Add(1)
		go s.serve(func(buf []byte) (int, int, net.Addr, error) {
			n, control, source, err := s.conn6.ReadFrom(buf)
			index := 0
			if control != nil {
				index = control.IfIndex
			}
			return n, index, source, err
		})
	}
	go func() {
		defer s.wg.Done()
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-s.done:
		case <-timer.C:
			if err := s.announce(120); err != nil && !errors.Is(err, net.ErrClosed) {
				s.logf(fmt.Sprintf("mDNS: announcement on %s failed: %v", link.iface.Name, err))
			}
		}
	}()
	return s, nil
}

func (s *responder) Shutdown() {
	s.once.Do(func() {
		close(s.done)
		// Finish any in-flight announcement before withdrawing the records.
		_ = s.conn.SetReadDeadline(time.Now())
		if s.conn6 != nil {
			_ = s.conn6.SetReadDeadline(time.Now())
		}
		s.wg.Wait()
		if err := s.announce(0); err != nil {
			s.logf(fmt.Sprintf("mDNS: goodbye on %s failed: %v", s.link.iface.Name, err))
		}
		_ = s.conn.Close()
		if s.conn6 != nil {
			_ = s.conn6.Close()
		}
	})
}

func (s *responder) announce(ttl uint32) error {
	records := s.zone.Records(dns.Question{Name: s.zone.Service + ".local.", Qtype: dns.TypePTR})
	for _, rr := range records {
		rr.Header().Ttl = ttl
		if rr.Header().Rrtype != dns.TypePTR {
			rr.Header().Class |= 1 << 15
		}
	}
	msg := &dns.Msg{
		MsgHdr: dns.MsgHdr{Response: true, Authoritative: true},
		Answer: records,
	}
	err := s.send(msg, multicastAddr)
	if s.conn6 != nil {
		err = errors.Join(err, s.send(msg, multicastAddr6))
	}
	return err
}

func (s *responder) serve(read func([]byte) (int, int, net.Addr, error)) {
	defer s.wg.Done()
	buf := make([]byte, 9000)
	for {
		n, index, source, err := read(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.logf(fmt.Sprintf("mDNS: listener on %s failed: %v", s.link.iface.Name, err))
				return
			}
		}
		from, ok := source.(*net.UDPAddr)
		if !ok || !s.onLink(index, from) {
			continue
		}
		var query dns.Msg
		if err := query.Unpack(buf[:n]); err != nil {
			s.logf(fmt.Sprintf("mDNS: invalid packet on %s: %v", s.link.iface.Name, err))
			continue
		}
		for _, reply := range s.replies(&query, from) {
			if err := s.send(reply.msg, reply.to); err != nil {
				s.logf(fmt.Sprintf("mDNS: reply on %s failed: %v", s.link.iface.Name, err))
			}
		}
	}
}

func (s *responder) onLink(index int, from *net.UDPAddr) bool {
	if index != 0 {
		return index == s.link.iface.Index
	}
	// Windows does not provide the receiving interface as ancillary data.
	if from.IP.To4() == nil {
		return from.Zone == s.link.iface.Name || from.Zone == fmt.Sprint(s.link.iface.Index)
	}
	for _, network := range s.link.networks {
		if network.Contains(from.IP) {
			return true
		}
	}
	return false
}

type reply struct {
	msg *dns.Msg
	to  *net.UDPAddr
}

func (s *responder) replies(query *dns.Msg, from *net.UDPAddr) []reply {
	if query.Response || query.Opcode != dns.OpcodeQuery || query.Rcode != dns.RcodeSuccess {
		return nil
	}
	legacy := from.Port != 5353
	groupAddr := multicastAddr
	if from.IP.To4() == nil {
		groupAddr = multicastAddr6
	}
	var multicast, unicast, multicastExtra, unicastExtra []dns.RR
	for _, q := range query.Question {
		if class := q.Qclass & 0x7fff; class != dns.ClassINET && class != dns.ClassANY {
			continue
		}
		q.Name = strings.ToLower(q.Name)
		wantUnicast := legacy || q.Qclass&(1<<15) != 0
		if q.Name == s.zone.HostName && q.Qtype == dns.TypeANY {
			q.Qtype = dns.TypeA
		}
		stamp := func(rr dns.RR) dns.RR {
			if legacy {
				rr.Header().Ttl = 10
			} else if rr.Header().Rrtype != dns.TypePTR {
				rr.Header().Class |= 1 << 15
			}
			return rr
		}
		records := s.zone.Records(q)
		var answers []dns.RR
		for _, rr := range records {
			if !wantUnicast && knownAnswer(query.Answer, rr) {
				continue
			}
			answers = append(answers, stamp(rr))
		}
		if len(records) > 0 && len(answers) == 0 {
			continue
		}
		var extra []dns.RR
		if q.Name == s.zone.HostName && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA) {
			extra = append(extra, stamp(&dns.NSEC{
				Hdr:        dns.RR_Header{Name: s.zone.HostName, Rrtype: dns.TypeNSEC, Class: dns.ClassINET, Ttl: 120},
				NextDomain: s.zone.HostName,
				TypeBitMap: []uint16{dns.TypeA, dns.TypeNSEC},
			}))
		}
		if wantUnicast {
			unicast, unicastExtra = append(unicast, answers...), append(unicastExtra, extra...)
		} else {
			multicast, multicastExtra = append(multicast, answers...), append(multicastExtra, extra...)
		}
	}
	var out []reply
	for _, group := range []struct {
		records []dns.RR
		extra   []dns.RR
		to      *net.UDPAddr
	}{{multicast, multicastExtra, groupAddr}, {unicast, unicastExtra, from}} {
		if len(group.records) == 0 && len(group.extra) == 0 {
			continue
		}
		msg := &dns.Msg{
			MsgHdr: dns.MsgHdr{Response: true, Authoritative: true},
			Answer: group.records,
			Extra:  group.extra,
		}
		if legacy {
			msg.Id = query.Id
			msg.Question = query.Question
		}
		out = append(out, reply{msg: msg, to: group.to})
	}
	return out
}

func knownAnswer(answers []dns.RR, rr dns.RR) bool {
	for _, answer := range answers {
		known := dns.Copy(answer)
		known.Header().Class &^= 1 << 15
		if known.Header().Ttl >= rr.Header().Ttl/2 && dns.IsDuplicate(known, rr) {
			return true
		}
	}
	return false
}

func (s *responder) send(msg *dns.Msg, to *net.UDPAddr) error {
	msg.Compress = true
	wire, err := msg.Pack()
	if err != nil {
		return err
	}
	if to.IP.To4() == nil {
		if s.conn6 == nil {
			return fmt.Errorf("IPv6 listener is unavailable")
		}
		if err := s.conn6.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return err
		}
		if _, err := s.conn6.WriteTo(wire, nil, to); err != nil {
			return fmt.Errorf("send to %s: %w", to, err)
		}
		return nil
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	if _, err := s.conn.WriteTo(wire, nil, to); err != nil {
		return fmt.Errorf("send to %s: %w", to, err)
	}
	return nil
}
