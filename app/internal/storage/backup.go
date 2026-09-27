package storage

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	BackupFloor  = 1 * GB
	backupMargin = 256 * MB
	StaleAfter   = 26 * time.Hour
)

type BackupSet struct {
	Stamp      string    `json:"stamp"`
	At         time.Time `json:"-"`
	DumpBytes  uint64    `json:"dump_bytes"`
	FilesBytes uint64    `json:"files_bytes"`
	Manual     bool      `json:"manual"`
}

func (s BackupSet) Bytes() uint64 { return s.DumpBytes + s.FilesBytes }

var dailyDump = regexp.MustCompile(`^care-(\d{8}-\d{6})\.dump(?:\.enc)?$`)
var manualDump = regexp.MustCompile(`^care-manual-(\d{8}-\d{6})\.dump(?:\.enc)?$`)

func LatestSets(dir string) (daily, newest BackupSet, found bool, err error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return BackupSet{}, BackupSet{}, false, nil
	}
	if err != nil {
		return BackupSet{}, BackupSet{}, false, err
	}
	sizes := map[string]uint64{}
	var sets []BackupSet
	for _, en := range entries {
		info, err := en.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		sizes[en.Name()] = uint64(info.Size())
		if m := dailyDump.FindStringSubmatch(en.Name()); m != nil {
			sets = append(sets, BackupSet{Stamp: m[1], At: info.ModTime(), DumpBytes: uint64(info.Size())})
		} else if m := manualDump.FindStringSubmatch(en.Name()); m != nil {
			sets = append(sets, BackupSet{Stamp: m[1], At: info.ModTime(), DumpBytes: uint64(info.Size()), Manual: true})
		}
	}
	for i := range sets {
		prefix := "files-"
		if sets[i].Manual {
			prefix = "files-manual-"
		}
		for _, name := range []string{prefix + sets[i].Stamp + ".tar.gz.enc", prefix + sets[i].Stamp + ".tar.gz"} {
			if n, ok := sizes[name]; ok {
				sets[i].FilesBytes = n
				break
			}
		}
	}
	if len(sets) == 0 {
		return BackupSet{}, BackupSet{}, false, nil
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].Stamp > sets[j].Stamp })
	newest = sets[0]
	daily = newest
	for _, s := range sets {
		if !s.Manual {
			daily = s
			break
		}
	}
	return daily, newest, true, nil
}

func BackupNeed(set BackupSet, found bool) uint64 {
	if !found {
		return BackupFloor
	}
	d, f := set.DumpBytes, set.FilesBytes
	peak := max(2*d, d+2*f)
	return max(peak*5/4+backupMargin, BackupFloor)
}

func DaysLeft(free, need, perDay uint64) int {
	if perDay == 0 {
		return -1
	}
	if free <= need {
		return 0
	}
	return int((free - need) / perDay)
}

type BackupRun struct {
	State     string `json:"state"`
	Reason    string `json:"reason"`
	At        int64  `json:"at"`
	NeedBytes uint64 `json:"need_bytes"`
	FreeBytes uint64 `json:"free_bytes"`
	Message   string `json:"message"`
}

const StatusFile = "backup-status"

func ReadBackupRun(dir string) (BackupRun, error) {
	f, err := os.Open(filepath.Join(dir, StatusFile))
	if os.IsNotExist(err) {
		return BackupRun{}, nil
	}
	if err != nil {
		return BackupRun{}, err
	}
	defer func() { _ = f.Close() }()
	var run BackupRun
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "state":
			run.State = v
		case "reason":
			run.Reason = v
		case "at":
			run.At, _ = strconv.ParseInt(v, 10, 64)
		case "need_kb":
			n, _ := strconv.ParseUint(v, 10, 64)
			run.NeedBytes = n << 10
		case "free_kb":
			n, _ := strconv.ParseUint(v, 10, 64)
			run.FreeBytes = n << 10
		case "message":
			run.Message = v
		}
	}
	return run, sc.Err()
}
