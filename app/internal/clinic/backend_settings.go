package clinic

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
)

var addedBackendSettings = []struct{ Key, Value string }{
	{"MAINTAIN_PATIENT_NAME_IDENTIFIER", "True"},
	{"MAINTAIN_PATIENT_PHONE_NUMBER_IDENTIFIER", "True"},
	{"DJANGO_EMAIL_BACKEND", "django.core.mail.backends.dummy.EmailBackend"},
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
	var tail []byte
	if len(b) > 0 && b[len(b)-1] != '\n' {
		tail = append(tail, '\n')
	}
	tail = append(tail, lines.String()...)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(tail); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return added, nil
}
