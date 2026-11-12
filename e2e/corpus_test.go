package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The corpus test compiles Assetnote's real routes dataset (routes-small.json, ~2000 APIs from real swagger
// files) and full scans a server with it, checking that every request kr builds is well formed: a valid
// method, path and query, a parseable Content-Type, and a body that decodes as that content type.
//
// It needs the 14MB dataset, so it only runs when KR_E2E_CORPUS is set: to a routes-small.json file, or to
// "download" to fetch it into the user cache directory.
const corpusURL = "https://wordlists-cdn.assetnote.io/rawdata/kiterunner/routes-small.json.tar.gz"

func corpusFile(t *testing.T) string {
	t.Helper()
	v := os.Getenv("KR_E2E_CORPUS")
	switch v {
	case "":
		t.Skip("set KR_E2E_CORPUS=download (or to a routes-small.json) to run the real dataset tests")
	case "download":
	default:
		return v
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(cache, "kiterunner-e2e", "routes-small.json")
	if _, err := os.Stat(dst); err == nil {
		return dst
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(corpusURL)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("failed to download the corpus: %v %v", err, resp)
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("routes-small.json not found in the archive: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "routes-small.json" {
			tmp := dst + ".tmp"
			f, err := os.Create(tmp)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				t.Fatal(err)
			}
			f.Close()
			if err := os.Rename(tmp, dst); err != nil {
				t.Fatal(err)
			}
			return dst
		}
	}
}

type corpusStats struct {
	apis, routes int
	params       map[string]int
	// path prefixes of templates whose author percent encoded a "?" into the path itself (/a/%3Fb=c)
	encodedQuestionMark []string
}

// sourceStats counts what the swagger dataset contains
func sourceStats(t *testing.T, file string) corpusStats {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var apis []struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				In string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(b, &apis); err != nil {
		t.Fatal(err)
	}
	st := corpusStats{apis: len(apis), params: map[string]int{}}
	for _, a := range apis {
		for path, ops := range a.Paths {
			if i := strings.Index(strings.ToLower(path), "%3f"); i >= 0 {
				st.encodedQuestionMark = append(st.encodedQuestionMark, path[:i])
			}
			for _, op := range ops {
				st.routes++
				for _, p := range op.Parameters {
					st.params[strings.ToLower(strings.TrimSpace(p.In))]++
				}
			}
		}
	}
	return st
}

// checkRequest returns why a request is malformed, or "" if it is fine
func checkRequest(e exchange) string {
	switch e.Method {
	case "GET", "POST", "PUT", "DELETE", "PATCH":
	default:
		return "method " + e.Method
	}
	// the raw request target must be a valid origin-form URI (the decoded path may contain anything)
	if !strings.HasPrefix(e.RequestURI, "/") || strings.ContainsAny(e.RequestURI, " \t\r\n") {
		return "request target"
	}
	if _, err := url.ParseRequestURI(e.RequestURI); err != nil {
		return "request target"
	}
	// a route template with an embedded "?query" must not end up percent encoded in the path
	if strings.Contains(e.Path, "?") {
		return "query in path"
	}
	if _, err := url.ParseQuery(e.Query); err != nil {
		return "query"
	}
	ct := e.Header.Get("Content-Type")
	if e.Body == "" && ct == "" {
		return ""
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil || strings.Contains(mt, "*") {
		return "content-type"
	}
	if e.Body == "" {
		return ""
	}
	switch {
	case strings.Contains(mt, "json"):
		if !json.Valid([]byte(e.Body)) {
			return "json body"
		}
	case strings.Contains(mt, "xml"):
		d := xml.NewDecoder(strings.NewReader(e.Body))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				return "xml body"
			}
		}
	case mt == "multipart/form-data":
		mr := multipart.NewReader(strings.NewReader(e.Body), params["boundary"])
		if _, err := mr.ReadForm(1 << 20); err != nil {
			return "multipart body"
		}
	default:
		if _, err := url.ParseQuery(e.Body); err != nil {
			return "form body"
		}
	}
	return ""
}

func TestCorpus(t *testing.T) {
	src := corpusFile(t)
	dir := t.TempDir()
	kite := filepath.Join(dir, "routes-small.kite")
	r := kr(t, "", "-q", "kb", "compile", src, kite)
	if r.ExitCode != 0 {
		t.Fatalf("compiling the dataset failed: %s", tail(r.Stderr))
	}

	want := sourceStats(t, src)
	out := filepath.Join(dir, "compiled.json")
	if r := kr(t, "", "-q", "kb", "convert", kite, out); r.ExitCode != 0 {
		t.Fatalf("kb convert failed: %s", tail(r.Stderr))
	}
	got := sourceStats(t, out)
	t.Logf("source: %d apis, %d routes, parameters %v", want.apis, want.routes, want.params)
	t.Logf("kite:   %d apis, %d routes, parameters %v", got.apis, got.routes, got.params)
	if got.apis != want.apis || got.routes != want.routes {
		t.Errorf("compiling lost apis or routes: %d/%d apis, %d/%d routes", got.apis, want.apis, got.routes, want.routes)
	}
	// formData is merged into body, a Content-Type header parameter becomes the content type
	for _, loc := range []string{"query", "path", "header"} {
		if loss := want.params[loc] - got.params[loc]; float64(loss) > 0.02*float64(want.params[loc]) {
			t.Errorf("lost %d of %d %s parameters", loss, want.params[loc], loc)
		}
	}
	if loss := want.params["body"] + want.params["formdata"] - got.params["body"]; float64(loss) > 0.02*float64(want.params["body"]+want.params["formdata"]) {
		t.Errorf("lost %d of %d body/formData parameters", loss, want.params["body"]+want.params["formdata"])
	}

	// full scan a server that answers everything with the same 404 and look at every request
	var (
		mu   sync.Mutex
		bad  = map[string][]string{}
		seen int
	)
	tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		reply(w, 404, "text/plain", "not found")
	})
	r = kr(t, "", "-q", "-o", "json", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "-x", "20", "-d", "0", "--progress=false")
	if r.ExitCode != 0 {
		t.Fatalf("scan failed: %s", tail(r.Stderr))
	}
	for _, e := range tg.requests() {
		seen++
		why := checkRequest(e)
		if why == "query in path" {
			for _, prefix := range want.encodedQuestionMark {
				if strings.HasPrefix(e.Path, prefix) {
					why = "" // the swagger file itself asks for an encoded "?" in the path
				}
			}
		}
		if why != "" {
			mu.Lock()
			if len(bad[why]) < 3 {
				bad[why] = append(bad[why], fmt.Sprintf("%s %s?%s content-type=%q body=%.200q", e.Method, e.Path, e.Query, e.Header.Get("Content-Type"), e.Body))
			} else {
				bad[why] = append(bad[why], "")
			}
			mu.Unlock()
		}
	}
	t.Logf("checked %d requests", seen)
	if seen < want.routes*9/10 {
		t.Errorf("expected about one request per route (%d), the server saw %d", want.routes, seen)
	}
	reasons := make([]string, 0, len(bad))
	for k := range bad {
		reasons = append(reasons, k)
	}
	sort.Strings(reasons)
	var report bytes.Buffer
	for _, k := range reasons {
		fmt.Fprintf(&report, "\n  %d malformed (%s), e.g.:", len(bad[k]), k)
		for _, ex := range bad[k] {
			if ex != "" {
				fmt.Fprintf(&report, "\n    %s", ex)
			}
		}
	}
	if report.Len() > 0 {
		t.Errorf("kr sent malformed requests:%s", report.String())
	}
	if hits := parseHits(t, r.Stdout); len(hits) != 0 {
		var ex []string
		for i, h := range hits {
			if i == 5 {
				break
			}
			ex = append(ex, fmt.Sprintf("%s %s %+v", h.Method, h.Path, h.Responses))
		}
		t.Errorf("a server that 404s everything must not produce results, got %d: %v", len(hits), ex)
	}
}
