package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestRecoverySaveDirectoryUsesWindowsDesktop(t *testing.T) {
	want, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := recoverySaveDirectory()
	if err != nil || got != want {
		t.Fatalf("recovery directory = %q, %v; want %q", got, err, want)
	}
}
