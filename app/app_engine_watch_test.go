package main

import (
	"testing"
	"time"
)

func TestEngineWatchWaitsOutABriefOutage(t *testing.T) {
	var w engineWatch
	start := time.Now()
	if w.observe(start, true) || w.observe(start.Add(engineWatchInterval), true) {
		t.Fatal("restarted Rancher before the outage outlasted the grace period")
	}
	if w.observe(start.Add(engineWatchInterval+time.Second), false) {
		t.Fatal("restarted Rancher after Docker answered again")
	}
	if w.observe(start.Add(2*engineWatchInterval), true) {
		t.Fatal("a recovered outage kept counting toward a restart")
	}
}

func TestEngineWatchRestartsOnceThenBacksOff(t *testing.T) {
	var w engineWatch
	start := time.Now()
	w.observe(start, true)
	if !w.observe(start.Add(engineDownGrace), true) {
		t.Fatal("did not restart Rancher after the grace period")
	}
	if w.observe(start.Add(engineDownGrace+engineWatchInterval), true) {
		t.Fatal("retried a restart before the back-off")
	}
	if !w.observe(start.Add(engineDownGrace+engineRetryAfter), true) {
		t.Fatal("never retried after the back-off")
	}
}

func TestEngineWatchForgetsBackoffAfterRecovery(t *testing.T) {
	var w engineWatch
	start := time.Now()
	w.observe(start, true)
	w.observe(start.Add(engineDownGrace), true)
	w.observe(start.Add(2*engineDownGrace), false)
	next := start.Add(3 * engineDownGrace)
	w.observe(next, true)
	if !w.observe(next.Add(engineDownGrace), true) {
		t.Fatal("a new outage after recovery waited out the earlier attempt's back-off")
	}
}
