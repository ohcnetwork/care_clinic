package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type dialer func() (net.Conn, error)

func unixTarget(path string) dialer {
	return func() (net.Conn, error) { return net.DialTimeout("unix", path, 5*time.Second) }
}

func tcpTarget(addr string) dialer {
	return func() (net.Conn, error) { return net.DialTimeout("tcp", addr, 5*time.Second) }
}

// serveProxy copies bytes both ways, so streaming Docker calls (logs, attach,
// exec, builds) work without the agent understanding them.
func serveProxy(l net.Listener, target dialer) error {
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			log.Printf("agent: accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go pipe(c, target)
	}
}

func pipe(c net.Conn, target dialer) {
	defer func() { _ = c.Close() }()
	t, err := target()
	if err != nil {
		return
	}
	defer func() { _ = t.Close() }()
	var wg sync.WaitGroup
	wg.Add(2)
	half := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go half(t, c)
	go half(c, t)
	wg.Wait()
}

// serverTLS accepts only clients holding the same per-boot certificate.
func serverTLS(pem []byte) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(pem, pem)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("agent: no certificate in credentials")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}
