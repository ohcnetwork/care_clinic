package clinic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/tailscale"
)

const HomePort = 8480

var homeOrigin = fmt.Sprintf("https://*.ts.net:%d", tailscale.Port)

func (e *Clinic) HomePortOpen() bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(HomePort)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (e *Clinic) ApplyHomeAccessSettings() (bool, error) {
	host := e.host()
	frontend, err := e.rewriteEnvValue("frontend.env", "REACT_CARE_API_URL", func(value string) (string, bool) {
		trimmed := strings.TrimSuffix(strings.TrimSpace(value), "/")
		if strings.EqualFold(trimmed, "https://"+host) {
			return "", true
		}
		return value, false
	})
	if err != nil {
		return false, err
	}
	backend, err := e.rewriteEnvValue("backend.env", "CSRF_TRUSTED_ORIGINS", func(value string) (string, bool) {
		var origins []string
		if err := json.Unmarshal([]byte(value), &origins); err != nil || slices.Contains(origins, homeOrigin) {
			return value, false
		}
		out, err := json.Marshal(append(origins, homeOrigin))
		if err != nil {
			return value, false
		}
		return string(out), true
	})
	if err != nil {
		return false, err
	}
	if frontend || backend {
		e.logln("Settings updated for home access.")
	}
	return frontend || backend, nil
}

func (e *Clinic) rewriteEnvValue(rel, key string, change func(string) (string, bool)) (bool, error) {
	path := filepath.Join(e.InstallDir, filepath.FromSlash(rel))
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	env, err := dotenv.Parse(bytes.NewReader(b))
	if err != nil {
		return false, fmt.Errorf("%s: %w", rel, err)
	}
	value, ok := env[key]
	if !ok {
		return false, nil
	}
	next, changed := change(value)
	if !changed {
		return false, nil
	}
	line := regexp.MustCompile(`(?m)^([ \t]*(?:export[ \t]+)?` + regexp.QuoteMeta(key) + `[ \t]*=[ \t]*)(.*?)(\r?)$`)
	matches := line.FindAllSubmatchIndex(b, -1)
	if len(matches) != 1 {
		return false, nil
	}
	m := matches[0]
	raw := string(b[m[4]:m[5]])
	quote := ""
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		quote = "'"
	} else if strings.ContainsAny(raw, "\"#") && !strings.HasPrefix(raw, "[") {
		return false, nil
	}
	if strings.ContainsAny(next, "'#\n") {
		return false, nil
	}
	out := append([]byte{}, b[:m[4]]...)
	out = append(out, quote+next+quote...)
	out = append(out, b[m[5]:]...)
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if err := atomicfile.Write(path, out, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}
