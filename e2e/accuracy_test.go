package e2e

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// Every fixture serves the same real routes (app) but answers everything else differently. A full scan must
// report exactly the real routes for each of them: no false positives, no false negatives.
func TestAccuracyFullScan(t *testing.T) {
	kite, _ := testKite(t)
	names := make([]string, 0, len(fixtures))
	for name := range fixtures {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		for _, useTLS := range []bool{false, true} {
			name, useTLS := name, useTLS
			label := name
			if useTLS {
				label += "/https"
			}
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				tg := fixtureTarget(t, name, useTLS)
				want := truth
				if name == "method-405" {
					// the schema has DELETE /api/v1/users, which the server knows as a path but not as a method.
					// a 405 reveals that the route exists, so reporting it is correct
					want = append(append([]string{}, truth...), "DELETE /api/v1/users")
				}
				assertExactly(t, scan(t, "", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan"), want...)
			})
		}
	}
}

// The application lives below a base path; requests outside it get a different 404.
func TestAccuracyBasePath(t *testing.T) {
	kite, _ := testKite(t)
	for _, name := range []string{"plain-404", "echo-404", "spa-catch-all"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fallback := fixtures[name]
			tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/v2app/") {
					reply(w, 404, "text/plain", "outside of the app")
					return
				}
				r.URL.Path = strings.TrimPrefix(r.URL.Path, "/v2app")
				if !app(w, r) {
					fallback(w, r)
				}
			})
			hits := scan(t, "", "scan", tg.URL+"/v2app/", "-w", kite, "--kitebuilder-full-scan")
			assertExactly(t, hits, truth...)
			for _, r := range tg.requests() {
				if !strings.HasPrefix(r.Path, "/v2app/") && r.Path != "/v2app" {
					t.Errorf("request escaped the base path: %s %s", r.Method, r.Path)
					break
				}
			}
		})
	}
}

// When every request (real routes included) gets the same 401, nothing can be told apart. kr must not report
// the 401s as findings.
func TestAccuracyAuthEverything(t *testing.T) {
	kite, _ := testKite(t)
	tg := newTarget(t, false, func(w http.ResponseWriter, r *http.Request) {
		reply(w, 401, "application/json", `{"error":"unauthorized"}`)
	})
	assertExactly(t, scan(t, "", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan"))
}

// The default two phase scan only scans the APIs whose first route responded. Whatever it reports must still be
// real (no false positives), and each finding must be reported once.
func TestAccuracyQuickScan(t *testing.T) {
	kite, _ := testKite(t)
	for name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tg := fixtureTarget(t, name, false)
			hits := scan(t, "n\n", "scan", tg.URL, "-w", kite)
			allowed := append(append([]string{}, truth...), "DELETE /api/v1/users")
			seen := map[string]bool{}
			for _, k := range keys(hits) {
				if seen[k] {
					t.Errorf("duplicate result %s", k)
				}
				seen[k] = true
				ok := false
				for _, a := range allowed {
					ok = ok || matches(a, k)
				}
				if !ok {
					t.Errorf("false positive %s", k)
				}
			}
		})
	}
}

// brute mode with a plain wordlist: real words must be found, decoys must not
func TestAccuracyBrute(t *testing.T) {
	words := writeFile(t, "words.txt", strings.Join([]string{
		"api/v1/users", "admin", // real
		"backup", "orders", "graphql", "settings", "login", "static/app.js", "api/v2/users", "admin/panel", // decoys
	}, "\n")+"\n")
	for name := range fixtures {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tg := fixtureTarget(t, name, false)
			want := []string{"GET /api/v1/users", "GET /admin"}
			if name == "method-405" {
				want = want[:1] // GET is allowed on /api/v1/users, nothing else differs
				want = append(want, "GET /admin")
			}
			assertExactly(t, scan(t, "", "brute", tg.URL, "-w", words), want...)
		})
	}
}
