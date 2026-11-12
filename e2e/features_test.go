package e2e

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func full(kite string) []string { return []string{"-w", kite, "--kitebuilder-full-scan"} }

func uniqueKeys(hits []hit) []string {
	seen := map[string]bool{}
	var ret []string
	for _, k := range keys(hits) {
		if !seen[k] {
			seen[k] = true
			ret = append(ret, k)
		}
	}
	return ret
}

func TestKiteParametersAreSent(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	scan(t, "", append([]string{"scan", tg.URL}, full(kite)...)...)
	reqs := tg.requestsTo("POST", "/onigokko/player")
	if len(reqs) != 1 {
		t.Fatalf("expected exactly one request to the player route, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Header.Get("Token") != "example-token" || r.Query != "QueryParam=example-query" ||
		!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") ||
		r.Body != `{"player":{"id":5,"name":"Nathan Reline"}}` {
		t.Errorf("parameters from the schema were not sent: query=%q token=%q content-type=%q body=%q",
			r.Query, r.Header.Get("Token"), r.Header.Get("Content-Type"), r.Body)
	}
}

func TestFilterAPI(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	hits := scan(t, "", append([]string{"scan", tg.URL, "--filter-api", strings.Repeat("b", 40)}, full(kite)...)...)
	assertExactly(t, hits, "POST /onigokko/player")
}

func TestForceMethod(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	hits := scan(t, "", append([]string{"scan", tg.URL, "--force-method", "PUT"}, full(kite)...)...)
	if got := uniqueKeys(hits); fmt.Sprint(got) != "[PUT /api/v1/users]" {
		t.Errorf("expected only PUT /api/v1/users, got %v", got)
	}
	for _, path := range []string{"/api/v1/users", "/admin", "/onigokko/player", "/graphql"} {
		for _, r := range tg.requestsTo("GET", path) {
			t.Errorf("route request was not forced to PUT: %s %s", r.Method, r.Path)
		}
	}

	r := kr(t, "", "-q", "scan", tg.URL, "-w", kite, "--force-method", "BOGUS")
	if r.ExitCode == 0 || !strings.Contains(r.Stderr, "BOGUS") {
		t.Errorf("an invalid method must be rejected, exit=%d stderr=%s", r.ExitCode, tail(r.Stderr))
	}
}

func TestHeaderAndUserAgent(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	scan(t, "", append([]string{"scan", tg.URL, "-H", "X-Custom: yes", "--user-agent", "kr-e2e-agent"}, full(kite)...)...)
	reqs := tg.requests()
	if len(reqs) == 0 {
		t.Fatal("no requests received")
	}
	for _, r := range reqs {
		if r.Header.Get("X-Custom") != "yes" || r.Header.Get("User-Agent") != "kr-e2e-agent" {
			t.Fatalf("request %s %s is missing the header or user agent: %v", r.Method, r.Path, r.Header)
		}
	}
}

func TestStatusCodeAndLengthFilters(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	base := append([]string{"scan", tg.URL}, full(kite)...)

	t.Run("success-status-codes", func(t *testing.T) {
		assertExactly(t, scan(t, "", append(base, "--success-status-codes", "200")...),
			"GET /api/v1/users", "PUT /api/v1/users", "GET /api/v1/users/*")
	})
	t.Run("fail-status-codes", func(t *testing.T) {
		assertExactly(t, scan(t, "", append(base, "--fail-status-codes", "403,201")...),
			"GET /api/v1/users", "PUT /api/v1/users", "GET /api/v1/users/*")
	})
	t.Run("ignore-length", func(t *testing.T) {
		// {"updated":true} is 16 bytes, {"created":true} is 16 bytes
		assertExactly(t, scan(t, "", append(base, "--ignore-length", "16")...),
			"GET /api/v1/users", "GET /api/v1/users/*", "GET /admin")
	})
}

func TestWildcardHost(t *testing.T) {
	kite, _ := testKite(t)
	tg := wildcardTarget(t)
	assertExactly(t, scan(t, "", append([]string{"scan", tg.URL}, full(kite)...)...))

	// with wildcard detection off every route is reported
	hits := scan(t, "", append([]string{"scan", tg.URL, "--wildcard-detection=false"}, full(kite)...)...)
	if len(uniqueKeys(hits)) < 10 {
		t.Errorf("expected every route to be reported without wildcard detection, got %v", keys(hits))
	}
}

// baselines of one target must never leak into another (they did through a pooled job)
func TestMultipleTargets(t *testing.T) {
	kite, _ := testKite(t)
	for i := 0; i < 5; i++ {
		real := fixtureTarget(t, "plain-404", false)
		wild := wildcardTarget(t)
		hosts := writeFile(t, "hosts.txt", real.URL+"\n"+wild.URL+"\n")
		hits := scan(t, "", append([]string{"scan", hosts}, full(kite)...)...)
		for _, h := range hits {
			if h.Target != real.URL {
				t.Fatalf("result for the wildcard host: %+v", h)
			}
		}
		assertExactly(t, hits, truth...)
	}
}

func TestTargetsFromStdin(t *testing.T) {
	kite, _ := testKite(t)
	for _, progress := range []string{"true", "false"} {
		t.Run("progress="+progress, func(t *testing.T) {
			tg := fixtureTarget(t, "plain-404", false)
			hits := scan(t, tg.URL+"\n", "scan", "-", "-w", kite, "--progress="+progress)
			assertExactly(t, hits, truth...)
		})
	}
	t.Run("brute", func(t *testing.T) {
		tg := fixtureTarget(t, "plain-404", false)
		words := writeFile(t, "w.txt", "api/v1/users\nnope\n")
		assertExactly(t, scan(t, tg.URL+"\n", "brute", "-", "-w", words), "GET /api/v1/users")
	})
}

func TestTargetWithoutScheme(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	assertExactly(t, scan(t, "", append([]string{"scan", strings.TrimPrefix(tg.URL, "http://")}, full(kite)...)...), truth...)
}

// timeouts must not be retried (fasthttp retried idempotent requests 5 times), and the scan must give up on the host
func TestUnresponsiveHost(t *testing.T) {
	kite, _ := testKite(t)
	tg := slowTarget(t)
	r := kr(t, "n\n", "-q", "-o", "json", "scan", tg.URL, "-w", kite, "-t", "1s", "--kitebuilder-full-scan")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
	}
	assertExactly(t, parseHits(t, r.Stdout))
	if r.Duration > 30*time.Second {
		t.Errorf("scan of an unresponsive host took %s", r.Duration)
	}
	counts := map[string]int{}
	for _, req := range tg.requests() {
		counts[req.Method+" "+req.Path]++
	}
	for k, n := range counts {
		if n > 1 {
			t.Errorf("%s was requested %d times, timeouts must not be retried", k, n)
		}
	}
}

func TestDelay(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	r := kr(t, "", "-q", "-o", "json", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "-x", "1", "--delay", "100ms")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
	}
	assertExactly(t, parseHits(t, r.Stdout), truth...)
	// routes are requested one after the other with a delay in between; preflight requests are not delayed
	routes := 0
	for _, req := range tg.requests() {
		if !regexp.MustCompile(`[0-9a-f]{16}|AAAA|^/$`).MatchString(req.Path) {
			routes++
		}
	}
	if min := time.Duration(routes-1) * 100 * time.Millisecond / 2; r.Duration < min {
		t.Errorf("%d route requests with a 100ms delay took only %s", routes, r.Duration)
	}
}

func TestBruteDepthAndExtensions(t *testing.T) {
	t.Run("default depth is 0", func(t *testing.T) {
		tg := fixtureTarget(t, "plain-404", false)
		words := []string{}
		for i := 0; i < 20; i++ {
			words = append(words, fmt.Sprintf("word%d", i))
		}
		scan(t, "", "brute", tg.URL, "-w", writeFile(t, "w.txt", strings.Join(words, "\n")))
		// 20 words plus the docroot preflight. depth 1 would add ~11 preflight requests per word
		if n := len(tg.requests()); n > 60 {
			t.Errorf("brute with the default depth sent %d requests for 20 words", n)
		}
	})

	t.Run("extensions", func(t *testing.T) {
		tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/secret.php" {
				reply(w, 200, "text/plain", "php secret page")
				return
			}
			reply(w, 404, "text/plain", "not found")
		})
		hits := scan(t, "", "brute", tg.URL, "-w", writeFile(t, "w.txt", "secret\n"), "-e", "php,bak")
		assertExactly(t, hits, "GET /secret.php")
		for _, p := range []string{"/secret", "/secret.php", "/secret.bak"} {
			if len(tg.requestsTo("GET", p)) != 1 {
				t.Errorf("expected exactly one request to %s", p)
			}
		}
	})

	t.Run("dirsearch", func(t *testing.T) {
		tg := fixtureTarget(t, "plain-404", false)
		scan(t, "", "brute", tg.URL, "-w", writeFile(t, "w.txt", "secret.%EXT%\nnope\n"), "-e", "php,bak", "-D")
		for _, p := range []string{"/secret.php", "/secret.bak", "/nope"} {
			if n := len(tg.requestsTo("GET", p)); n != 1 {
				t.Errorf("expected exactly one request to %s, got %d", p, n)
			}
		}
		for _, r := range tg.requests() {
			if strings.Contains(r.Path, "EXT") {
				t.Errorf("the %%EXT%% placeholder was requested literally: %s", r.Path)
			}
		}
	})
}

func TestRedirects(t *testing.T) {
	tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redir":
			http.Redirect(w, r, "/landing", http.StatusFound)
		case "/landing":
			reply(w, 200, "text/plain", "you landed here")
		case "/ext":
			http.Redirect(w, r, "http://blocked.invalid/x", http.StatusFound)
		default:
			reply(w, 404, "text/plain", "not found")
		}
	})
	words := writeFile(t, "w.txt", "redir\next\n")
	chain := func(hits []hit, path string) []int {
		for _, h := range hits {
			if h.Path == path {
				var scs []int
				for _, r := range h.Responses {
					scs = append(scs, r.SC)
				}
				return scs
			}
		}
		return nil
	}

	hits := scan(t, "", "brute", tg.URL, "-w", words)
	if got := fmt.Sprint(chain(hits, "/redir")); got != "[302 200]" {
		t.Errorf("redirect should be followed, got chain %s", got)
	}

	hits = scan(t, "", "brute", tg.URL, "-w", words, "--max-redirects", "0")
	if got := fmt.Sprint(chain(hits, "/redir")); got != "[302]" {
		t.Errorf("with --max-redirects 0 the redirect must not be followed, got chain %s", got)
	}

	// a blacklisted redirect is recorded with its location but not requested (status 0)
	hits = scan(t, "", "brute", tg.URL, "-w", words, "--blacklist-domain", "blocked.invalid")
	if got := fmt.Sprint(chain(hits, "/ext")); got != "[302 0]" {
		t.Errorf("a redirect to a blacklisted domain must not be followed, got chain %s", got)
	}
	for _, h := range hits {
		if h.Path == "/ext" && (len(h.Responses) != 2 || h.Responses[1].URI != "http://blocked.invalid/x") {
			t.Errorf("the blacklisted location should be shown, got %+v", h.Responses)
		}
	}
}

func TestQuickScanPrompt(t *testing.T) {
	kite, _ := testKite(t)
	// only the decoy API: the quick scan finds nothing and asks whether to continue
	decoys := []string{"scan", "", "-w", kite, "--filter-api", strings.Repeat("c", 40)}
	counts := map[string]int{}
	for _, answer := range []string{"n\n", "y\n", ""} {
		tg := fixtureTarget(t, "plain-404", false)
		decoys[1] = tg.URL
		r := kr(t, answer, append([]string{"-q", "-o", "json"}, decoys...)...)
		if r.ExitCode != 0 {
			t.Fatalf("exit %d: %s", r.ExitCode, tail(r.Stderr))
		}
		if !strings.Contains(r.Stderr, "Continue Scanning with full wordlist?") {
			t.Errorf("expected the continue prompt on stderr")
		}
		counts[answer] = len(tg.requests())
	}
	if !(counts["y\n"] > counts["n\n"] && counts["n\n"] == counts[""]) {
		t.Errorf("y must continue the scan, n and EOF must stop it. requests: %v", counts)
	}
}

func TestOutputStreams(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	for _, format := range []string{"text", "pretty"} {
		r := kr(t, "", "-o", format, "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan")
		lines := 0
		for _, l := range strings.Split(r.Stdout, "\n") {
			l = strings.TrimSpace(regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(l, ""))
			if l == "" {
				continue
			}
			if !regexp.MustCompile(`^(GET|PUT|POST|DELETE|PATCH) +\d{3} `).MatchString(l) {
				t.Errorf("-o %s: stdout must only contain results, got %q", format, l)
			}
			lines++
		}
		if lines != len(truth) {
			t.Errorf("-o %s: expected %d results on stdout, got %d", format, len(truth), lines)
		}
	}
}

func TestConfigFile(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	cfg := writeFile(t, "kr.yaml", "output: json\nquiet: true\nverbose: error\n")

	r := kr(t, "", "--config", cfg, "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan")
	assertExactly(t, parseHits(t, r.Stdout), truth...)
	if strings.Contains(r.Stderr, "▄") {
		t.Error("quiet from the config file must hide the banner")
	}

	// a flag overrides the config file
	r = kr(t, "", "--config", cfg, "-o", "text", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan")
	if strings.Contains(r.Stdout, `"method"`) || !strings.Contains(r.Stdout, "GET ") {
		t.Errorf("-o text must override the config file, stdout: %s", r.Stdout)
	}
}

func TestScanRejectsJSONWordlist(t *testing.T) {
	_, schemaJSON := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	r := kr(t, "", "-q", "scan", tg.URL, "-w", schemaJSON)
	if r.ExitCode == 0 || !strings.Contains(r.Stderr, "kb compile") {
		t.Errorf("a json schema passed to -w must fail with a hint to compile it, exit=%d stderr=%s", r.ExitCode, tail(r.Stderr))
	}
}
