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
	"sync/atomic"
	"testing"
)

// wordlistServer mimics wordlists.assetnote.io: the manifests plus the files they link to
type wordlistServer struct {
	*target
	kiteDownloads int64
}

func newWordlistServer(t *testing.T) *wordlistServer {
	t.Helper()
	kite, _ := testKite(t)
	kiteBytes, err := os.ReadFile(kite)
	if err != nil {
		t.Fatal(err)
	}
	// routes-test.kite.tar.gz, laid out like the archives assetnote publishes
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "routes-test.kite", Mode: 0o644, Size: int64(len(kiteBytes)), Typeflag: tar.TypeReg})
	tw.Write(kiteBytes)
	tw.Close()
	gz.Close()

	ws := &wordlistServer{}
	ws.target = newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		// links point back at this server, like the real manifests point at the CDN
		link := func(p string) string { return fmt.Sprintf("<a href='http://%s/./%s'>Download</a>", r.Host, p) }
		manifest := func(rows ...map[string]interface{}) {
			b, _ := json.Marshal(map[string]interface{}{"data": rows})
			reply(w, 200, "application/json", string(b))
		}
		switch r.URL.Path {
		case "/data/automated.json":
			manifest(map[string]interface{}{"Filename": "httparchive_apiroutes_2026_02_27.txt", "Line Count": 3, "File Size": "1kb", "Date": 1.7e9, "Download": link("data/automated/httparchive_apiroutes_2026_02_27.txt")})
		case "/data/manual.json":
			manifest(map[string]interface{}{"Filename": "broken.txt", "Line Count": 1, "File Size": "1kb", "Date": 1.6e9, "Download": link("data/manual/broken.txt")})
		case "/data/kiterunner.json":
			manifest(
				map[string]interface{}{"Filename": "routes-test.kite.tar.gz", "Line Count": 3, "File Size": "1kb", "Date": 1.6e9, "Download": link("data/kiterunner/routes-test.kite.tar.gz")},
				map[string]interface{}{"Filename": "routes-test.json.tar.gz", "Line Count": 3, "File Size": "1kb", "Date": 1.6e9, "Download": link("rawdata/kiterunner/routes-test.json.tar.gz")},
				map[string]interface{}{"Filename": "swagger-files.tar", "Line Count": 3, "File Size": "4.4gb", "Date": 1.6e9, "Download": link("rawdata/kiterunner/swagger-files.tar")},
			)
		case "/data/automated/httparchive_apiroutes_2026_02_27.txt":
			reply(w, 200, "text/plain", "api/v1/users\nadmin\nnope\n")
		case "/data/kiterunner/routes-test.kite.tar.gz":
			atomic.AddInt64(&ws.kiteDownloads, 1)
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(archive.Bytes())
		case "/data/manual/broken.txt":
			reply(w, 503, "text/html", "<html>service unavailable</html>")
		default:
			reply(w, 404, "text/plain", "not found")
		}
	})
	return ws
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
	for _, want := range []string{"apiroutes-260227", "routes-test", "broken"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("wordlist list is missing %s:\n%s", want, r.Stdout)
		}
	}
	for _, unwanted := range []string{"json.tar.gz", "swagger-files"} {
		if strings.Contains(r.Stdout, unwanted) {
			t.Errorf("wordlist list shows %s, which kr can't use", unwanted)
		}
	}
}

// -A with a compiled kite wordlist: downloaded once, unpacked, cached and scanned
func TestScanWithKiteWordlist(t *testing.T) {
	ws := newWordlistServer(t)
	env, cache := ws.env(t)
	for i := 0; i < 2; i++ {
		tg := fixtureTarget(t, "plain-404", false)
		r := krEnv(t, env, "", "-q", "-o", "json", "scan", tg.URL, "-A", "routes-test", "--kitebuilder-full-scan")
		if r.ExitCode != 0 {
			t.Fatalf("scan -A routes-test failed: %s", tail(r.Stderr))
		}
		assertExactly(t, parseHits(t, r.Stdout), truth...)
	}
	if n := atomic.LoadInt64(&ws.kiteDownloads); n != 1 {
		t.Errorf("the kite archive should be downloaded once and then cached, got %d downloads", n)
	}
	if _, err := os.Stat(filepath.Join(cache, "routes-test.kite")); err != nil {
		t.Errorf("the unpacked kite file should be cached: %v", err)
	}

	// brute needs a plain wordlist
	r := krEnv(t, env, "", "-q", "brute", "http://127.0.0.1:1", "-A", "routes-test")
	if r.ExitCode == 0 || !strings.Contains(r.Stderr, "kr scan -A routes-test") {
		t.Errorf("brute with a kite wordlist must fail with a hint, exit=%d stderr=%s", r.ExitCode, tail(r.Stderr))
	}
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
	for _, args := range [][]string{{"routes-test"}, {"routes-test"}, {"routes-test", "--no-cache"}} {
		if r := krEnv(t, env, "", append([]string{"-q", "wordlist", "save"}, args...)...); r.ExitCode != 0 {
			t.Fatalf("wordlist save %v failed: %s", args, tail(r.Stderr))
		}
	}
	if n := atomic.LoadInt64(&ws.kiteDownloads); n != 2 {
		t.Errorf("expected a download, a cache hit, then a forced download with --no-cache; got %d downloads", n)
	}
	if _, err := os.Stat(filepath.Join(cache, "routes-test.kite.tar.gz")); err != nil {
		t.Errorf("the archive should be cached: %v", err)
	}
}

// the cache follows $XDG_CACHE_HOME when it is set
func TestWordlistCacheLocation(t *testing.T) {
	ws := newWordlistServer(t)
	env, _ := ws.env(t)
	xdg := t.TempDir()
	if r := krEnv(t, append(env, "XDG_CACHE_HOME="+xdg), "", "-q", "wordlist", "save", "routes-test"); r.ExitCode != 0 {
		t.Fatalf("wordlist save failed: %s", tail(r.Stderr))
	}
	if _, err := os.Stat(filepath.Join(xdg, "kiterunner", "wordlists", "routes-test.kite.tar.gz")); err != nil {
		t.Errorf("the wordlist should be cached below $XDG_CACHE_HOME: %v", err)
	}
}
