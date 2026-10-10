// Command care-clinic-configure fills a clinic's frontend settings into the
// prebuilt CARE frontend bundle each time the container starts.
//
// The CARE frontend is compiled once in CI with a placeholder for every
// setting listed in runtime-keys.json. At start this copies the pristine
// bundle to the directory nginx serves and replaces each placeholder with the
// clinic's value from frontend.env, or the setting's default when the clinic
// leaves it blank. CARE's own source is never modified.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// placeholder must match PlaceholderPrefix in app/internal/compose/build.go.
var placeholder = regexp.MustCompile(`https://care-clinic\.invalid/__CARE_CLINIC_([A-Z0-9_]+)__`)

var (
	nullRevision = regexp.MustCompile(`revision:null`)
	revision     = regexp.MustCompile(`revision:"([^"\\]*)"`)
)

func main() {
	template := flag.String("template", "/care-clinic/html", "pristine bundle")
	out := flag.String("out", "/usr/share/nginx/html", "directory nginx serves")
	keys := flag.String("keys", "/care-clinic/runtime-keys.json", "settings applied at start, with defaults")
	env := flag.String("env", "/care-clinic/frontend.env", "the clinic's frontend settings")
	flag.Parse()
	if err := run(*template, *out, *keys, *env); err != nil {
		fmt.Fprintln(os.Stderr, "care-clinic-configure:", err)
		os.Exit(1)
	}
}

func run(template, out, keysPath, envPath string) error {
	defaults, err := readKeys(keysPath)
	if err != nil {
		return err
	}
	env := map[string]string{}
	if data, err := os.ReadFile(envPath); err == nil {
		if env, err = parseEnv(data); err != nil {
			return fmt.Errorf("%s: %w", envPath, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	values := resolve(defaults, env)
	n, err := render(template, out, values)
	if err != nil {
		return err
	}
	fmt.Printf("care-clinic-configure: applied %d settings to %d files\n", len(values), n)
	return nil
}

func readKeys(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var keys map[string]string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return keys, nil
}

// resolve picks the clinic's value for each runtime setting. A blank value
// takes the default: the compiled bundle may have folded CARE's own fallback
// away, because the placeholder it was built with is never empty.
func resolve(defaults, env map[string]string) map[string]string {
	values := make(map[string]string, len(defaults))
	for k, def := range defaults {
		if v := env[k]; v != "" {
			values[k] = v
		} else {
			values[k] = def
		}
	}
	return values
}

// configHash identifies a set of values so browsers re-fetch the bundle when
// any of them changes.
func configHash(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%q\n", k, values[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func render(template, out string, values map[string]string) (int, error) {
	if _, err := os.Stat(template); err != nil {
		return 0, err
	}
	if err := emptyDir(out); err != nil {
		return 0, err
	}
	hash := configHash(values)
	files := 0
	err := filepath.WalkDir(template, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(template, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(out, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data, err = fill(rel, data, values)
		if err != nil {
			return err
		}
		if isServiceWorker(rel) {
			data = bumpRevisions(data, hash)
		}
		files++
		return os.WriteFile(dst, data, 0o644)
	})
	return files, err
}

// emptyDir empties dir without removing it: it may be a mount point.
func emptyDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func fill(name string, data []byte, values map[string]string) ([]byte, error) {
	var escape func(string) string
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs", ".cjs":
		escape = escapeJS
	case ".html", ".htm":
		escape = escapeHTML
	case ".json", ".webmanifest":
		escape = escapeJSON
	default:
		if placeholder.Match(data) {
			return nil, fmt.Errorf("%s contains a setting placeholder in a file type that cannot be filled safely", name)
		}
		return data, nil
	}
	var missing error
	data = placeholder.ReplaceAllFunc(data, func(m []byte) []byte {
		key := string(placeholder.FindSubmatch(m)[1])
		v, ok := values[key]
		if !ok {
			missing = fmt.Errorf("%s needs %s, which runtime-keys.json does not list", name, key)
			return m
		}
		return []byte(escape(v))
	})
	return data, missing
}

// escapeJS makes v safe inside any JavaScript string literal: single, double
// or template quoted.
func escapeJS(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\'':
			b.WriteString(`\'`)
		case '`':
			b.WriteString("\\`")
		case '$':
			b.WriteString(`\$`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '<':
			b.WriteString(`\x3c`)
		case '\u2028':
			b.WriteString(`\u2028`)
		case '\u2029':
			b.WriteString(`\u2029`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func escapeHTML(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(v)
}

func escapeJSON(v string) string {
	data, _ := json.Marshal(v)
	return string(data[1 : len(data)-1])
}

func isServiceWorker(name string) bool {
	base := filepath.Base(name)
	return base == "service-worker.js" || base == "sw.js"
}

// bumpRevisions changes every precache revision in the service worker so a
// browser that cached the bundle with other settings downloads it again.
func bumpRevisions(data []byte, hash string) []byte {
	data = nullRevision.ReplaceAll(data, []byte(`revision:"cfg-`+hash+`"`))
	return revision.ReplaceAllFunc(data, func(m []byte) []byte {
		old := revision.FindSubmatch(m)[1]
		if bytes.HasPrefix(old, []byte("cfg-")) {
			return m
		}
		return []byte(`revision:"` + string(old) + `-` + hash + `"`)
	})
}

// parseEnv reads the dotenv subset CARE Clinic writes: KEY=value lines with
// optional single, double or backtick quotes, comments and blank lines.
func parseEnv(data []byte) (map[string]string, error) {
	env := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, raw, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("line %d is not KEY=value", line)
		}
		value, err := unquote(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		env[key] = value
	}
	return env, sc.Err()
}

func unquote(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch q := raw[0]; q {
	case '\'', '`':
		end := strings.IndexByte(raw[1:], q)
		if end < 0 {
			return "", errors.New("unterminated quote")
		}
		return raw[1 : end+1], nil
	case '"':
		var b strings.Builder
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			if c == '"' {
				return b.String(), nil
			}
			if c == '\\' && i+1 < len(raw) {
				i++
				switch raw[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(raw[i])
				}
				continue
			}
			b.WriteByte(c)
		}
		return "", errors.New("unterminated quote")
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw), nil
}
