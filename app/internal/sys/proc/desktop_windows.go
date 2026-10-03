package proc

import "golang.org/x/sys/windows"

func DesktopDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
}
