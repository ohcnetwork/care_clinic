package proc

import (
	"os"

	"golang.org/x/sys/windows"
)

func Reveal(path string) error {
	args := `/select,"` + path + `"`
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		args = `"` + path + `"`
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString("explorer.exe")
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(args)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, params, nil, windows.SW_SHOWNORMAL)
}
