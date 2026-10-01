package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

type ConflictError struct {
	Host    string
	Address net.IP
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s is already in use on this network (%s). Each clinic server needs a different address.", e.Host, e.Address)
}

// CheckAvailable bypasses the system resolver, including its cache and hosts file.
// Silence only means that no reachable device claimed the name during this check.
func CheckAvailable(name string) error {
	if err := ValidateLabel(name); err != nil {
		return err
	}
	links, err := lanInterfaces()
	if err != nil {
		return fmt.Errorf("could not check the clinic address: %w", err)
	}
	return checkAvailable(Label(name)+".local.", links, probeConflict)
}

// Resolve returns the LAN address of another device answering for name. It uses
// the same multicast probe as CheckAvailable, so it ignores this computer's
// hosts file: a client whose hosts file still points the clinic name at itself
// can still reach the real clinic. Silence is an error, not a loopback answer.
func Resolve(name string) (net.IP, error) {
	err := CheckAvailable(name)
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		return conflict.Address, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no device on this network answered for %s.local", Label(name))
}

func checkAvailable(host string, links []lanInterface, probe func(context.Context, string, lanInterface, []net.IP) error) error {
	if len(links) == 0 {
		return fmt.Errorf("could not check the clinic address: no LAN interface available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var ownIPs []net.IP
	for _, link := range links {
		ownIPs = append(ownIPs, link.ips...)
		ownIPs = append(ownIPs, link.ips6...)
	}
	results := make(chan error, len(links))
	for _, link := range links {
		go func() {
			err := probe(ctx, host, link, ownIPs)
			if err != nil {
				err = fmt.Errorf("%s: %w", link.iface.Name, err)
			}
			results <- err
		}()
	}
	var firstErr error
	for range links {
		err := <-results
		var conflict *ConflictError
		if errors.As(err, &conflict) {
			return conflict
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("could not check the clinic address: %w", firstErr)
	}
	return ctx.Err()
}

func probeConflict(ctx context.Context, host string, link lanInterface, ownIPs []net.IP) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: link.ips[0]})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	packet := ipv4.NewPacketConn(conn)
	for _, err := range []error{
		packet.SetMulticastInterface(&link.iface),
		packet.SetMulticastTTL(255),
		packet.SetMulticastLoopback(true),
	} {
		if err != nil {
			return err
		}
	}
	return queryConflicts(ctx, conn, host, ownIPs, multicastAddr)
}

func queryConflicts(ctx context.Context, conn *net.UDPConn, host string, ownIPs []net.IP, destination *net.UDPAddr) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("hostname conflict check requires a deadline")
	}
	var queries []*dns.Msg
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		query := new(dns.Msg)
		query.SetQuestion(host, qtype)
		query.RecursionDesired = false
		queries = append(queries, query)
	}
	buf := make([]byte, 9000)
	// Keep listening after our own answer; a second server may answer later.
	for range 3 {
		if err := ctx.Err(); err != nil {
			return err
		}
		until := time.Now().Add(500 * time.Millisecond)
		if deadline.Before(until) {
			until = deadline
		}
		if err := conn.SetDeadline(until); err != nil {
			return err
		}
		for _, query := range queries {
			wire, err := query.Pack()
			if err != nil {
				return err
			}
			if _, err := conn.WriteToUDP(wire, destination); err != nil {
				return err
			}
		}
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					break
				}
				return err
			}
			if from.Port != destination.Port {
				continue
			}
			var reply dns.Msg
			if err := reply.Unpack(buf[:n]); err != nil {
				return fmt.Errorf("invalid hostname response: %w", err)
			}
			if reply.Id != 0 && reply.Id != queries[0].Id && reply.Id != queries[1].Id {
				continue
			}
			if ip := foreignAddress(&reply, host, ownIPs); ip != nil {
				return &ConflictError{Host: strings.TrimSuffix(host, "."), Address: ip}
			}
		}
	}
	if !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return ctx.Err()
}

func foreignAddress(reply *dns.Msg, host string, ownIPs []net.IP) net.IP {
	if !reply.Response || !reply.Authoritative || reply.Opcode != dns.OpcodeQuery || reply.Rcode != dns.RcodeSuccess {
		return nil
	}
	for _, section := range [][]dns.RR{reply.Answer, reply.Extra} {
		for _, rr := range section {
			header := rr.Header()
			if header.Ttl == 0 || header.Class&0x7fff != dns.ClassINET || !strings.EqualFold(header.Name, host) {
				continue
			}
			var ip net.IP
			switch record := rr.(type) {
			case *dns.A:
				ip = record.A
			case *dns.AAAA:
				ip = record.AAAA
			}
			if ip != nil && !containsIP(ownIPs, ip) {
				return ip
			}
		}
	}
	return nil
}
