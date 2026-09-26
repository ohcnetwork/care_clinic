package diskspace

import (
	"strings"

	"golang.org/x/sys/windows"
)

func statVolume(path string) (Usage, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Usage{}, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return Usage{}, err
	}
	volume := make([]uint16, windows.MAX_PATH+1)
	id := ""
	if err := windows.GetVolumePathName(p, &volume[0], uint32(len(volume))); err == nil {
		id = strings.ToUpper(windows.UTF16ToString(volume))
	}
	return Usage{Volume: id, Free: free, Total: total}, nil
}
