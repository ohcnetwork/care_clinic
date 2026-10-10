package vm

import (
	"context"
	"io"
	"net"
	"sync"
)

type closeWriter interface{ CloseWrite() error }

// pipe copies both ways until both directions finish, passing half-closes
// through so request/response protocols (docker attach, HTTP) end cleanly.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	wg.Add(2)
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}

// forward accepts on ln and connects each client to dial().
func forward(ctx context.Context, ln net.Listener, dial func(context.Context) (net.Conn, error), logf func(string, ...any)) {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				logf("forward %s: %v", ln.Addr(), err)
			}
			return
		}
		go func() {
			up, err := dial(ctx)
			if err != nil {
				logf("forward %s: %v", ln.Addr(), err)
				_ = c.Close()
				return
			}
			pipe(c, up)
		}()
	}
}
