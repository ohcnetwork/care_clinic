package appremoval

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ohcnetwork/care_desktop/app/internal/sys/elevate"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/proc"
)

func Target() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return bundleOf(exe)
	case "windows":
		uninstaller := filepath.Join(filepath.Dir(exe), "uninstall.exe")
		if !proc.FileExists(uninstaller) {
			return "", errors.New("this copy of CARE Desktop was not installed with the Windows installer; delete its folder instead")
		}
		return uninstaller, nil
	}
	return "", errors.New("removing the app is not supported on this system")
}

func bundleOf(exe string) (string, error) {
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if filepath.Ext(bundle) != ".app" || filepath.Base(filepath.Dir(exe)) != "MacOS" {
		return "", errors.New("CARE Desktop is not running from an app bundle")
	}
	if strings.Contains(bundle, "/AppTranslocation/") || strings.HasPrefix(bundle, "/Volumes/") {
		return "", errors.New("CARE Desktop is running from the disk image or a temporary copy; drag it to the Trash from the folder it was installed in")
	}
	return bundle, nil
}

func Remove(target string) error {
	if runtime.GOOS == "windows" {
		out, err := proc.Command("powershell", "-NoProfile", "-Command",
			"Start-Process -FilePath "+elevate.PSQuote(target)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("couldn't start the uninstaller: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return trash(target)
}

func OtherInstanceRunning(wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if !otherInstance() {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func otherInstance() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	exe, err := executable()
	if err != nil {
		return false
	}
	out, err := proc.Command("tasklist", "/FI", "IMAGENAME eq "+filepath.Base(exe), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false
	}
	return otherPIDs(string(out), os.Getpid())
}

func otherPIDs(tasklist string, self int) bool {
	rows, err := csv.NewReader(strings.NewReader(tasklist)).ReadAll()
	if err != nil {
		return false
	}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		pid, err := strconv.Atoi(row[1])
		if err == nil && pid != self {
			return true
		}
	}
	return false
}

func ForeignSession() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	current, err := user.Current()
	if err != nil {
		return false
	}
	out, err := proc.Command("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_ComputerSystem).UserName").Output()
	if err != nil {
		return false
	}
	return differentUser(strings.TrimSpace(string(out)), current.Username)
}

func differentUser(sessionUser, processUser string) bool {
	return sessionUser != "" && !strings.EqualFold(sessionUser, processUser)
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
