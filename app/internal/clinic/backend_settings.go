package clinic

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
)

var addedBackendSettings = []struct{ Key, Value string }{
	{"MAINTAIN_PATIENT_NAME_IDENTIFIER", "True"},
	{"MAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER", "True"},
}

func (e *Clinic) AddMissingBackendSettings() ([]string, error) {
	path := filepath.Join(e.InstallDir, "backend.env")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	env, err := dotenv.Parse(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("backend.env: %w", err)
	}
	var added []string
	var lines strings.Builder
	for _, s := range addedBackendSettings {
		if _, ok := env[s.Key]; ok {
			continue
		}
		added = append(added, s.Key)
		lines.WriteString(s.Key + "=" + s.Value + "\n")
	}
	if len(added) == 0 {
		return nil, nil
	}
	out := append([]byte{}, b...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, lines.String()...)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := atomicfile.Write(path, out, info.Mode().Perm()); err != nil {
		return nil, err
	}
	return added, nil
}
