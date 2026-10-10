package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ph = "https://care-clinic.invalid/__CARE_CLINIC_"

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fixture(t *testing.T) (template, out, keys string) {
	t.Helper()
	root := t.TempDir()
	template, out, keys = filepath.Join(root, "html"), filepath.Join(root, "out"), filepath.Join(root, "keys.json")
	write(t, filepath.Join(template, "index.html"), `<title>`+ph+`REACT_APP_TITLE__</title>`)
	write(t, filepath.Join(template, "assets", "index-abc.js"),
		`const e={REACT_APP_TITLE:"`+ph+`REACT_APP_TITLE__",REACT_CARE_API_URL:"`+ph+`REACT_CARE_API_URL__"};`)
	write(t, filepath.Join(template, "service-worker.js"),
		`precache([{url:"assets/index-abc.js",revision:null},{url:"index.html",revision:"5f2a"}]);`)
	write(t, filepath.Join(template, "logo.png"), "\x89PNG")
	write(t, keys, `{"REACT_APP_TITLE":"CARE","REACT_CARE_API_URL":""}`)
	write(t, filepath.Join(out, "stale.js"), "old")
	return template, out, keys
}

func TestRunFillsSettingsAndDefaults(t *testing.T) {
	template, out, keys := fixture(t)
	env := filepath.Join(t.TempDir(), "frontend.env")
	write(t, env, "# comment\nREACT_APP_TITLE='Clinic \"A\" <b>'\nREACT_CARE_API_URL=\nUNRELATED=1\n")
	if err := run(template, out, keys, env); err != nil {
		t.Fatal(err)
	}
	js := read(t, filepath.Join(out, "assets", "index-abc.js"))
	if want := `REACT_APP_TITLE:"Clinic \"A\" \x3cb>"`; !strings.Contains(js, want) {
		t.Fatalf("title not escaped for JavaScript: %s", js)
	}
	if !strings.Contains(js, `REACT_CARE_API_URL:""`) {
		t.Fatalf("blank setting did not take its default: %s", js)
	}
	if html := read(t, filepath.Join(out, "index.html")); html != "<title>Clinic &quot;A&quot; &lt;b&gt;</title>" {
		t.Fatalf("title not escaped for HTML: %s", html)
	}
	if read(t, filepath.Join(out, "logo.png")) != "\x89PNG" {
		t.Fatal("binary file changed")
	}
	if _, err := os.Stat(filepath.Join(out, "stale.js")); !os.IsNotExist(err) {
		t.Fatal("previous bundle was not cleared")
	}
	if _, err := os.Stat(filepath.Join(template, "assets", "index-abc.js")); err != nil {
		t.Fatal("template was modified")
	}
	if strings.Contains(read(t, filepath.Join(template, "assets", "index-abc.js")), "Clinic") {
		t.Fatal("template was filled in place")
	}
}

func TestMissingEnvUsesDefaults(t *testing.T) {
	template, out, keys := fixture(t)
	if err := run(template, out, keys, filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Fatal(err)
	}
	if html := read(t, filepath.Join(out, "index.html")); html != "<title>CARE</title>" {
		t.Fatalf("default title not applied: %s", html)
	}
}

func TestServiceWorkerRevisionsFollowSettings(t *testing.T) {
	template, out, keys := fixture(t)
	env := filepath.Join(t.TempDir(), "frontend.env")
	sw := func(title string) string {
		write(t, env, "REACT_APP_TITLE="+title+"\n")
		if err := run(template, out, keys, env); err != nil {
			t.Fatal(err)
		}
		return read(t, filepath.Join(out, "service-worker.js"))
	}
	a, b, again := sw("One"), sw("Two"), sw("One")
	if a == b {
		t.Fatal("service worker did not change with the settings")
	}
	if a != again {
		t.Fatal("service worker is not stable for the same settings")
	}
	if strings.Contains(a, "revision:null") || !strings.Contains(a, `revision:"cfg-`) || !strings.Contains(a, `revision:"5f2a-`) {
		t.Fatalf("revisions not rewritten: %s", a)
	}
}

func TestUnknownPlaceholderFails(t *testing.T) {
	template, out, keys := fixture(t)
	write(t, filepath.Join(template, "assets", "other.js"), `x="`+ph+`REACT_NOT_LISTED__"`)
	if err := run(template, out, keys, ""); err == nil || !strings.Contains(err.Error(), "REACT_NOT_LISTED") {
		t.Fatalf("unknown placeholder accepted: %v", err)
	}
}

func TestPlaceholderInUnsupportedFileFails(t *testing.T) {
	template, out, keys := fixture(t)
	write(t, filepath.Join(template, "assets", "style.css"), `a{content:"`+ph+`REACT_APP_TITLE__"}`)
	if err := run(template, out, keys, ""); err == nil {
		t.Fatal("placeholder in a CSS file was ignored")
	}
}

func TestEscapeJSCoversEveryQuoteStyle(t *testing.T) {
	got := escapeJS("a\\b\"c'd`e${f}\ng\u2028h\x01")
	want := `a\\b\"c\'d` + "\\`" + `e\${f}\ng\u2028h\x01`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestParseEnv(t *testing.T) {
	env, err := parseEnv([]byte(strings.Join([]string{
		"# comment", "", "export A=plain # trailing", `B="line\nnext \"q\""`, "C='lit $X \\n'", "D=`tick`", "E=", "F=a#b",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "plain", "B": "line\nnext \"q\"", "C": `lit $X \n`, "D": "tick", "E": "", "F": "a#b"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
	for _, bad := range []string{"NOEQUALS", "A='open", `B="open`} {
		if _, err := parseEnv([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
