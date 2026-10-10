//go:build !linux

package main

import (
	"errors"
	"net"
	"time"
)

// The agent only runs in the Linux guest; these let its portable parts build
// and be tested on a developer machine.
var errGuestOnly = errors.New("only available inside the CARE engine VM")

func listenVsock(uint32) (net.Listener, error)         { return nil, errGuestOnly }
func mountShare(string, string) error                  { return errGuestOnly }
func setClock(time.Time) error                         { return errGuestOnly }
func diskUsage(string) (free, total uint64, err error) { return 0, 0, errGuestOnly }
