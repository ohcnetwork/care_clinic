package mdns

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestForeignAddress(t *testing.T) {
	own := net.ParseIP("192.0.2.10")
	foreign := net.ParseIP("192.0.2.20")
	base := &dns.Msg{
		MsgHdr: dns.MsgHdr{Response: true, Authoritative: true},
		Answer: []dns.RR{
			&dns.A{Hdr: dns.RR_Header{Name: "CARE.local.", Rrtype: dns.TypeA, Class: dns.ClassINET | 1<<15, Ttl: 120}, A: foreign},
		},
	}
	for _, tc := range []struct {
		name   string
		change func(*dns.Msg)
		want   bool
	}{
		{"foreign A", func(*dns.Msg) {}, true},
		{"own address", func(m *dns.Msg) { m.Answer[0].(*dns.A).A = own }, false},
		{"goodbye", func(m *dns.Msg) { m.Answer[0].Header().Ttl = 0 }, false},
		{"unrelated name", func(m *dns.Msg) { m.Answer[0].Header().Name = "other.local." }, false},
		{"query known answer", func(m *dns.Msg) { m.Response = false }, false},
		{"not authoritative", func(m *dns.Msg) { m.Authoritative = false }, false},
		{"failure response", func(m *dns.Msg) { m.Rcode = dns.RcodeServerFailure }, false},
		{"wrong class", func(m *dns.Msg) { m.Answer[0].Header().Class = dns.ClassCHAOS }, false},
		{"additional records", func(m *dns.Msg) { m.Extra, m.Answer = m.Answer, nil }, true},
		{"IPv6 claim", func(m *dns.Msg) {
			m.Answer = []dns.RR{&dns.AAAA{
				Hdr:  dns.RR_Header{Name: "care.local.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 120},
				AAAA: net.ParseIP("fe80::20"),
			}}
		}, true},
		{"own then foreign", func(m *dns.Msg) {
			m.Answer = append([]dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: "care.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120}, A: own,
			}}, m.Answer...)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := base.Copy()
			tc.change(reply)
			if got := foreignAddress(reply, "care.local.", []net.IP{own}); (got != nil) != tc.want {
				t.Fatalf("foreign address = %s, want conflict %v", got, tc.want)
			}
		})
	}
}

func TestAvailabilityChecksEveryInterface(t *testing.T) {
	links := []lanInterface{testLink(1, "192.0.2.10/24"), testLink(2, "198.51.100.10/24")}
	for _, outcome := range []string{"available", "conflict", "network error"} {
		t.Run(outcome, func(t *testing.T) {
			var calls atomic.Int32
			err := checkAvailable("care.local.", links, func(ctx context.Context, host string, link lanInterface, own []net.IP) error {
				calls.Add(1)
				if _, ok := ctx.Deadline(); !ok || host != "care.local." || len(own) != 2 {
					t.Error("missing deadline, chosen hostname or local interface addresses")
				}
				if link.iface.Index == 2 {
					switch outcome {
					case "conflict":
						return &ConflictError{Host: "care.local", Address: net.ParseIP("198.51.100.20")}
					case "network error":
						return errors.New("could not send")
					}
				}
				return nil
			})
			if (err != nil) != (outcome != "available") {
				t.Fatalf("result = %v", err)
			}
			if outcome == "conflict" {
				var conflict *ConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("lost conflict error: %v", err)
				}
			} else if calls.Load() != 2 {
				t.Fatal("did not check every interface")
			}
		})
	}
	if err := checkAvailable("care.local.", nil, nil); err == nil {
		t.Fatal("no interfaces must not mean available")
	}
}

func conflictTestSocket(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestConflictQueryKeepsListeningAfterOwnReply(t *testing.T) {
	server, client := conflictTestSocket(t), conflictTestSocket(t)
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 9000)
		_ = server.SetDeadline(time.Now().Add(2 * time.Second))
		n, from, err := server.ReadFromUDP(buf)
		if err != nil {
			done <- err
			return
		}
		var query dns.Msg
		if err := query.Unpack(buf[:n]); err != nil {
			done <- err
			return
		}
		for _, address := range []string{"192.0.2.10", "192.0.2.20"} {
			reply := new(dns.Msg)
			reply.SetReply(&query)
			reply.Authoritative = true
			reply.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: "care.local.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
				A:   net.ParseIP(address),
			}}
			wire, err := reply.Pack()
			if err == nil {
				_, err = server.WriteToUDP(wire, from)
			}
			if err != nil {
				done <- err
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		done <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := queryConflicts(ctx, client, "care.local.", []net.IP{net.ParseIP("192.0.2.10")}, server.LocalAddr().(*net.UDPAddr))
	if serverErr := <-done; serverErr != nil {
		t.Fatal(serverErr)
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Address.String() != "192.0.2.20" {
		t.Fatalf("own response masked foreign response: %v", err)
	}
}

func TestConflictQuerySilenceRetriesAndErrors(t *testing.T) {
	server, client := conflictTestSocket(t), conflictTestSocket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := queryConflicts(ctx, client, "unused.local.", nil, server.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 1500*time.Millisecond {
		t.Fatal("reported availability before the full listening window")
	}
	for range 6 {
		_ = server.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 9000)
		if _, _, err := server.ReadFromUDP(buf); err != nil {
			t.Fatalf("missing repeated A/AAAA query: %v", err)
		}
	}
	cancel()
	if err := queryConflicts(ctx, client, "unused.local.", nil, server.LocalAddr().(*net.UDPAddr)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query reported availability: %v", err)
	}
	_ = client.Close()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := queryConflicts(ctx, client, "unused.local.", nil, server.LocalAddr().(*net.UDPAddr)); err == nil {
		t.Fatal("socket error reported availability")
	}
}

// This check never advertises the real clinic's name.
func TestExistingNameConflictOverLAN(t *testing.T) {
	host := os.Getenv("CARE_MDNS_CONFLICT_TEST_HOST")
	if host == "" {
		t.Skip("set CARE_MDNS_CONFLICT_TEST_HOST to an existing remote clinic hostname")
	}
	err := CheckAvailable(host)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected existing clinic conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), Label(host)+".local") {
		t.Fatal("error did not name the conflicting address")
	}
	t.Log(err)
}
