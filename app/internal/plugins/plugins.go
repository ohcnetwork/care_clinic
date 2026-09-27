package plugins

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/ohcnetwork/care_desktop/app/internal/sys/atomicfile"
	"go.yaml.in/yaml/v3"
)

type Manager struct {
	Dir string
}

func New(dir string) *Manager { return &Manager{Dir: dir} }

const (
	additionalPlugsKey = "ADDITIONAL_PLUGS"
	listFile           = "plugins.json"
	ManagedKey         = "managed_by"
	ManagedValue       = "care-desktop"
)

type Backend struct {
	Name        string         `json:"name"`
	PackageName string         `json:"package_name"`
	Version     string         `json:"version,omitempty"`
	Configs     map[string]any `json:"configs,omitempty"`
}

type Frontend struct {
	Slug string         `json:"slug"`
	URL  string         `json:"url"`
	Meta map[string]any `json:"meta,omitempty"`
}

type Plugin struct {
	ID       string    `json:"id"`
	Label    string    `json:"label,omitempty"`
	Catalog  bool      `json:"catalog,omitempty"`
	Backend  *Backend  `json:"backend,omitempty"`
	Frontend *Frontend `json:"frontend,omitempty"`
}

type CatalogEntry struct {
	Plugin      Plugin `json:"plugin"`
	Description string `json:"description,omitempty"`
}

//go:embed catalog.yml
var catalogYAML []byte

func Catalog() ([]CatalogEntry, error) {
	var raw any
	if err := yaml.Unmarshal(catalogYAML, &raw); err != nil {
		return nil, fmt.Errorf("the bundled plugin catalog is invalid: %w", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("the bundled plugin catalog is invalid: %w", err)
	}
	entries := []CatalogEntry{}
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("the bundled plugin catalog is invalid: %w", err)
	}
	for i := range entries {
		entries[i].Plugin.Catalog = true
	}
	return entries, nil
}

func (m *Manager) backendEnvPath() string { return filepath.Join(m.Dir, "backend.env") }
func (m *Manager) listPath() string       { return filepath.Join(m.Dir, listFile) }

func (m *Manager) ReadPlugins() ([]Plugin, error) {
	b, err := os.ReadFile(m.listPath())
	if errors.Is(err, fs.ErrNotExist) {
		return m.fromAdditionalPlugs()
	}
	if err != nil {
		return nil, err
	}
	list := []Plugin{}
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", listFile, err)
	}
	if list == nil {
		return []Plugin{}, nil
	}
	return list, nil
}

func (m *Manager) fromAdditionalPlugs() ([]Plugin, error) {
	backends, err := m.readBackends()
	if err != nil {
		return nil, err
	}
	list := make([]Plugin, 0, len(backends))
	for i := range backends {
		list = append(list, Plugin{ID: backends[i].Name, Backend: &backends[i]})
	}
	return list, nil
}

func (m *Manager) SavePlugins(list []Plugin) error {
	list, err := Prepare(list)
	if err != nil {
		return err
	}
	backends := []Backend{}
	for _, p := range list {
		if p.Backend != nil {
			backends = append(backends, *p.Backend)
		}
	}
	if err := m.writeBackends(backends); err != nil {
		return err
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(m.listPath(), append(b, '\n'), 0o600)
}

func (m *Manager) FrontendRows() (map[string]map[string]any, error) {
	list, err := m.ReadPlugins()
	if err != nil {
		return nil, err
	}
	rows := map[string]map[string]any{}
	for _, p := range list {
		if p.Frontend == nil {
			continue
		}
		meta := map[string]any{"name": p.Frontend.Slug}
		for k, v := range p.Frontend.Meta {
			meta[k] = v
		}
		meta["url"] = p.Frontend.URL
		meta[ManagedKey] = ManagedValue
		rows[p.Frontend.Slug] = meta
	}
	return rows, nil
}

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	modulePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	slugPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

func Prepare(list []Plugin) ([]Plugin, error) {
	catalog, err := Catalog()
	if err != nil {
		return nil, err
	}
	byID := map[string]Plugin{}
	for _, e := range catalog {
		byID[e.Plugin.ID] = e.Plugin
	}
	out := make([]Plugin, 0, len(list))
	ids, modules, slugs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, p := range list {
		p.ID = strings.TrimSpace(p.ID)
		p.Label = strings.TrimSpace(p.Label)
		if p.Catalog {
			if entry, ok := byID[p.ID]; ok {
				p = fromCatalog(p, entry)
			} else {
				p.Catalog = false
			}
		}
		if err := check(&p); err != nil {
			return nil, err
		}
		if ids[p.ID] {
			return nil, fmt.Errorf("two plugins are named %q", p.ID)
		}
		ids[p.ID] = true
		if p.Backend != nil {
			if modules[p.Backend.Name] {
				return nil, fmt.Errorf("two plugins use the backend module %q", p.Backend.Name)
			}
			modules[p.Backend.Name] = true
		}
		if p.Frontend != nil {
			if slugs[p.Frontend.Slug] {
				return nil, fmt.Errorf("two plugins use the frontend name %q", p.Frontend.Slug)
			}
			slugs[p.Frontend.Slug] = true
		}
		out = append(out, p)
	}
	return out, nil
}

func fromCatalog(p, entry Plugin) Plugin {
	out := Plugin{ID: entry.ID, Label: entry.Label, Catalog: true}
	if entry.Backend != nil {
		b := *entry.Backend
		if p.Backend != nil {
			b.Configs = p.Backend.Configs
		}
		out.Backend = &b
	}
	if entry.Frontend != nil {
		f := *entry.Frontend
		if p.Frontend != nil {
			f.Meta = p.Frontend.Meta
		}
		out.Frontend = &f
	}
	return out
}

func check(p *Plugin) error {
	if p.ID == "" {
		return errors.New("every plugin needs a name")
	}
	if !idPattern.MatchString(p.ID) {
		return fmt.Errorf("plugin name %q can only use letters, numbers, '.', '_' and '-'", p.ID)
	}
	if p.Backend == nil && p.Frontend == nil {
		return fmt.Errorf("plugin %q needs a backend, a frontend, or both", p.ID)
	}
	if b := p.Backend; b != nil {
		b.Name = strings.TrimSpace(b.Name)
		b.PackageName = strings.TrimSpace(b.PackageName)
		b.Version = strings.TrimSpace(b.Version)
		if !modulePattern.MatchString(b.Name) {
			return fmt.Errorf("plugin %q: the backend module %q is not a Python module name", p.ID, b.Name)
		}
		if b.PackageName == "" || strings.ContainsAny(b.PackageName, " \t\r\n") {
			return fmt.Errorf("plugin %q: the backend package needs a pip source without spaces", p.ID)
		}
		if b.Version != "" && !strings.ContainsAny(b.Version[:1], "@=<>~!") {
			return fmt.Errorf("plugin %q: the backend version %q must start with '@' (like @main) or a pip operator (like ==1.2)", p.ID, b.Version)
		}
	}
	if f := p.Frontend; f != nil {
		f.Slug = strings.TrimSpace(f.Slug)
		f.URL = strings.TrimSpace(f.URL)
		if !slugPattern.MatchString(f.Slug) {
			return fmt.Errorf("plugin %q: the frontend name %q can only use letters, numbers, '_' and '-'", p.ID, f.Slug)
		}
		u, err := url.Parse(f.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("plugin %q: the frontend URL must be an http(s) link to its remoteEntry.js", p.ID)
		}
		if _, ok := f.Meta["url"]; ok {
			return fmt.Errorf("plugin %q: set the frontend URL in its own field, not in the frontend settings", p.ID)
		}
	}
	return nil
}

func (m *Manager) readBackends() ([]Backend, error) {
	raw, err := m.AdditionalPlugs()
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return []Backend{}, nil
	}
	plugs := []Backend{}
	if err := json.Unmarshal([]byte(raw), &plugs); err != nil {
		return nil, err
	}
	if plugs == nil {
		return []Backend{}, nil
	}
	return plugs, nil
}

func (m *Manager) writeBackends(plugs []Backend) error {
	raw := ""
	if len(plugs) > 0 {
		b, err := json.Marshal(plugs)
		if err != nil {
			return err
		}
		raw = "'" + strings.ReplaceAll(string(b), "'", `\'`) + "'"
	}
	return m.setEnvVar(additionalPlugsKey, raw)
}

func (m *Manager) AdditionalPlugs() (string, error) {
	value, err := m.envVar(additionalPlugsKey)
	return strings.TrimSpace(value), err
}

func (m *Manager) envVar(key string) (string, error) {
	b, err := os.ReadFile(m.backendEnvPath())
	if err != nil {
		return "", err
	}
	env, err := dotenv.Parse(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	return env[key], nil
}

func (m *Manager) setEnvVar(key, val string) error {
	path := m.backendEnvPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := make([]string, 0)
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		text := strings.TrimPrefix(strings.TrimSpace(line), "export ")
		name, _, ok := strings.Cut(text, "=")
		if ok && strings.TrimSpace(name) == key {
			if !found && val != "" {
				lines = append(lines, key+"="+val)
			}
			found = true
			continue
		}
		lines = append(lines, line)
	}
	if !found && val != "" {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = append(lines[:n-1], key+"="+val, "")
		} else {
			lines = append(lines, key+"="+val)
		}
	}
	output := strings.Join(lines, "\n")
	if _, err := dotenv.Parse(strings.NewReader(output)); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(output), 0o600)
}
