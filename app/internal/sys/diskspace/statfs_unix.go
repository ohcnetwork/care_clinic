//go:build !windows

package diskspace

import (
	"strconv"

	"golang.org/x/sys/unix"
)

func statVolume(path string) (Usage, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return Usage{}, err
	}
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return Usage{}, err
	}
	bsize := uint64(fs.Bsize)
	return Usage{
		Volume: strconv.FormatUint(uint64(st.Dev), 10),
		Free:   uint64(fs.Bavail) * bsize,
		Total:  uint64(fs.Blocks) * bsize,
	}, nil
}
