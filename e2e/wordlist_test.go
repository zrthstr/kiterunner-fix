package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// wordlistServer mimics wordlists.assetnote.io: the manifests and the files they link to, laid out like the
// real ones. routes-small is published as a compiled kite and as the json it was built from, and kr must use
// the json (the official kite has no parameters). routes-large must come from the kite.
type wordlistServer struct {
	*target
	mu        sync.Mutex
	downloads map[string]int
}

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// a macOS metadata entry like in assetnote's archives, which must be skipped
	tw.WriteHeader(&tar.Header{Name: "._" + name, Mode: 0o644, Size: 4, Typeflag: tar.TypeReg})
	tw.Write([]byte("junk"))
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write(content)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func newWordlistServer(t *testing.T) *wordlistServer {
	t.Helper()
	kite, schemaJSON := testKite(t)
	kiteBytes, err := os.ReadFile(kite)
	if err != nil {
		t.Fatal(err)
	}
	jsonBytes, err := os.ReadFile(schemaJSON)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"/data/kiterunner/routes-small.kite.tar.gz":    []byte("must not be downloaded"),
		"/rawdata/kiterunner/routes-small.json.tar.gz": tarGz(t, "routes-small.json", jsonBytes),
		"/data/kiterunner/routes-large.kite.tar.gz":    tarGz(t, "routes-large.kite", kiteBytes),
		"/rawdata/kiterunner/routes-large.json.tar.gz": []byte("must not be downloaded"),
	}

	ws := &wordlistServer{downloads: map[string]int{}}
	ws.target = newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		// links point back at this server, like the real manifests point at the CDN
		link := func(p string) string { return fmt.Sprintf("<a href='http://%s/./%s'>Download</a>", r.Host, p) }
		row := func(name, path string) map[string]interface{} {
			return map[string]interface{}{"Filename": name, "Line Count": 3, "File Size": "1kb", "Date": 1.6e9, "Download": link(path)}
		}
		manifest := func(rows ...map[string]interface{}) {
			b, _ := json.Marshal(map[string]interface{}{"data": rows})
			reply(w, 200, "application/json", string(b))
		}
		if body, ok := files[r.URL.Path]; ok {
			ws.mu.Lock()
			ws.downloads[filepath.Base(r.URL.Path)]++
			ws.mu.Unlock()
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(body)
			return
		}
		switch r.URL.Path {
		case "/data/automated.json":
			manifest(row("httparchive_apiroutes_2026_02_27.txt", "data/automated/httparchive_apiroutes_2026_02_27.txt"))
		case "/data/manual.json":
			manifest(row("broken.txt", "data/manual/broken.txt"))
		case "/data/kiterunner.json":
			manifest(
				row("swagger-files.tar", "rawdata/kiterunner/swagger-files.tar"),
				row("routes-large.json.tar.gz", "rawdata/kiterunner/routes-large.json.tar.gz"),
				row("routes-small.json.tar.gz", "rawdata/kiterunner/routes-small.json.tar.gz"),
				row("routes-small.kite.tar.gz", "data/kiterunner/routes-small.kite.tar.gz"),
				row("routes-large.kite.tar.gz", "data/kiterunner/routes-large.kite.tar.gz"),
			)
		case "/data/automated/httparchive_apiroutes_2026_02_27.txt":
			reply(w, 200, "text/plain", "api/v1/users\nadmin\nnope\n")
		case "/data/manual/broken.txt":
			reply(w, 503, "text/html", "<html>service unavailable</html>")
		default:
			reply(w, 404, "text/plain", "not found")
		}
	})
	return ws
}

func (ws *wordlistServer) downloaded(name string) int {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.downloads[name]
}

// env returns the environment for a kr process that uses this server and its own empty cache
func (ws *wordlistServer) env(t *testing.T) (env []string, cacheDir string) {
	home := t.TempDir()
	return []string{"HOME=" + home, "KITERUNNER_WORDLIST_URL=" + ws.URL + "/data/"}, filepath.Join(home, ".cache", "kiterunner", "wordlists")
}

func TestWordlistList(t *testing.T) {
	ws := newWordlistServer(t)
	env, _ := ws.env(t)
	r := krEnv(t, env, "", "-q", "wordlist", "list")
	if r.ExitCode != 0 {
		t.Fatalf("wordlist list failed: %s", tail(r.Stderr))
	}
	for _, want := range []string{"apiroutes-260227", "broken", "routes-small.json.tar.gz", "routes-large.kite.tar.gz"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("wordlist list is missing %s:\n%s", want, r.Stdout)
		}
	}
	for _, unwanted := range []string{"routes-small.kite.tar.gz", "routes-large.json.tar.gz", "swagger-files"} {
		if strings.Contains(r.Stdout, unwanted) {
			t.Errorf("wordlist list shows %s, which kr must not use", unwanted)
		}
	}
}

// -A routes-small compiles assetnote's json (the official kite has no parameters), -A routes-large uses the
// official kite. Each is downloaded once and then served from the cache
func TestScanWithAssetnoteRoutes(t *testing.T) {
	ws := newWordlistServer(t)
	env, cache := ws.env(t)
	for _, name := range []string{"routes-small", "routes-large"} {
		for i := 0; i < 2; i++ {
			tg := fixtureTarget(t, "plain-404", false)
			r := krEnv(t, env, "", "-q", "-o", "json", "scan", tg.URL, "-A", name, "--kitebuilder-full-scan")
			if r.ExitCode != 0 {
				t.Fatalf("scan -A %s failed: %s", name, tail(r.Stderr))
			}
			assertExactly(t, parseHits(t, r.Stdout), truth...)
			// the schema's parameters are sent, so the player route answers 201
			if reqs := tg.requestsTo("POST", "/onigokko/player"); len(reqs) != 1 || reqs[0].Header.Get("Token") != "example-token" {
				t.Errorf("-A %s: the route parameters were not sent", name)
			}
		}
	}
	for file, want := range map[string]int{
		"routes-small.json.tar.gz": 1, "routes-large.kite.tar.gz": 1,
		"routes-small.kite.tar.gz": 0, "routes-large.json.tar.gz": 0,
	} {
		if got := ws.downloaded(file); got != want {
			t.Errorf("%s was downloaded %d times, want %d", file, got, want)
		}
	}
	for _, f := range []string{"routes-small.compiled.kite", "routes-large.kite"} {
		if _, err := os.Stat(filepath.Join(cache, f)); err != nil {
			t.Errorf("%s should be cached: %v", f, err)
		}
	}

	// brute needs a plain wordlist
	for _, name := range []string{"routes-small", "routes-large"} {
		r := krEnv(t, env, "", "-q", "brute", "http://127.0.0.1:1", "-A", name)
		if r.ExitCode == 0 || !strings.Contains(r.Stderr, "kr scan -A "+name) {
			t.Errorf("brute with %s must fail with a hint, exit=%d stderr=%s", name, r.ExitCode, tail(r.Stderr))
		}
	}
}

// a cache from an older version holds the official routes-small kite (which has no parameters). It must not be
// used for -A routes-small
func TestStaleRoutesSmallCache(t *testing.T) {
	ws := newWordlistServer(t)
	env, cache := ws.env(t)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"routes-small.kite", "routes-small.kite.tar.gz"} {
		if err := os.WriteFile(filepath.Join(cache, f), []byte("stale official file"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tg := fixtureTarget(t, "plain-404", false)
	r := krEnv(t, env, "", "-q", "-o", "json", "scan", tg.URL, "-A", "routes-small", "--kitebuilder-full-scan")
	if r.ExitCode != 0 {
		t.Fatalf("scan -A routes-small with a stale cache failed: %s", tail(r.Stderr))
	}
	assertExactly(t, parseHits(t, r.Stdout), truth...)
}

func TestBruteWithRemoteWordlist(t *testing.T) {
	ws := newWordlistServer(t)
	env, _ := ws.env(t)
	tg := fixtureTarget(t, "plain-404", false)
	r := krEnv(t, env, "", "-q", "-o", "json", "brute", tg.URL, "-A", "apiroutes-260227")
	if r.ExitCode != 0 {
		t.Fatalf("brute -A failed: %s", tail(r.Stderr))
	}
	assertExactly(t, parseHits(t, r.Stdout), "GET /api/v1/users", "GET /admin")

	// name:N only uses the first N words
	tg = fixtureTarget(t, "plain-404", false)
	r = krEnv(t, env, "", "-q", "-o", "json", "brute", tg.URL, "-A", "apiroutes-260227:1")
	assertExactly(t, parseHits(t, r.Stdout), "GET /api/v1/users")

	// more than the wordlist has must not crash
	r = krEnv(t, env, "", "-q", "-o", "json", "brute", tg.URL, "-A", "apiroutes-260227:999999")
	if r.ExitCode != 0 {
		t.Errorf("a length above the wordlist size failed: %s", tail(r.Stderr))
	}
}

// a failed download must be an error, and must never end up in the cache
func TestWordlistDownloadFailure(t *testing.T) {
	ws := newWordlistServer(t)
	env, cache := ws.env(t)
	r := krEnv(t, env, "", "-q", "wordlist", "save", "broken")
	if r.ExitCode == 0 || !strings.Contains(r.Stderr, "503") {
		t.Errorf("a 503 download must fail and say why, exit=%d stderr=%s", r.ExitCode, tail(r.Stderr))
	}
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		t.Errorf("nothing should be cached after a failed download, found %s", e.Name())
	}

	tg := fixtureTarget(t, "plain-404", false)
	r = krEnv(t, env, "", "-q", "brute", tg.URL, "-A", "broken")
	if r.ExitCode == 0 {
		t.Error("brute with a wordlist that fails to download must fail")
	}
	if len(tg.requests()) != 0 {
		t.Error("nothing should be scanned when the wordlist failed to download")
	}
}

func TestWordlistSaveNoCache(t *testing.T) {
	ws := newWordlistServer(t)
	env, cache := ws.env(t)
	for _, args := range [][]string{{"routes-large"}, {"routes-large"}, {"routes-large", "--no-cache"}} {
		if r := krEnv(t, env, "", append([]string{"-q", "wordlist", "save"}, args...)...); r.ExitCode != 0 {
			t.Fatalf("wordlist save %v failed: %s", args, tail(r.Stderr))
		}
	}
	if n := ws.downloaded("routes-large.kite.tar.gz"); n != 2 {
		t.Errorf("expected a download, a cache hit, then a forced download with --no-cache; got %d downloads", n)
	}
	if _, err := os.Stat(filepath.Join(cache, "routes-large.kite.tar.gz")); err != nil {
		t.Errorf("the archive should be cached: %v", err)
	}
}

// the cache follows $XDG_CACHE_HOME when it is set
func TestWordlistCacheLocation(t *testing.T) {
	ws := newWordlistServer(t)
	env, _ := ws.env(t)
	xdg := t.TempDir()
	if r := krEnv(t, append(env, "XDG_CACHE_HOME="+xdg), "", "-q", "wordlist", "save", "routes-large"); r.ExitCode != 0 {
		t.Fatalf("wordlist save failed: %s", tail(r.Stderr))
	}
	if _, err := os.Stat(filepath.Join(xdg, "kiterunner", "wordlists", "routes-large.kite.tar.gz")); err != nil {
		t.Errorf("the wordlist should be cached below $XDG_CACHE_HOME: %v", err)
	}
}
