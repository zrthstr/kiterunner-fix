package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The realworld tests scan real applications (realworld/docker-compose.yml) on different stacks: Java/Jersey,
// Python/Flask, Node/Express behind a single page app, and Go. One schema contains the real routes of all four
// plus decoys; scanning each app must report exactly that app's routes. Everything else (the other apps'
// routes, decoys) must be filtered out against each app's own 404s, 405s, 500 error pages and SPA fallback.
//
// Start the apps, then run with KR_E2E_REALWORLD=1. The app URLs can be overridden with KR_E2E_<APP>_URL.

type realApp struct {
	name, envURL, defaultURL string
	ksuid                    string
	paths                    map[string]interface{}
	want                     []string
	setup                    func(t *testing.T, base string)
}

func realApps() []realApp {
	op := func(params ...map[string]interface{}) map[string]interface{} {
		ps := make([]interface{}, len(params))
		for i := range params {
			ps[i] = params[i]
		}
		return map[string]interface{}{"description": "d", "operationId": "o", "parameters": ps, "consumes": []string{"application/json"}}
	}
	body := func(props map[string]interface{}, required ...string) map[string]interface{} {
		return map[string]interface{}{"in": "body", "name": "body", "required": true,
			"schema": map[string]interface{}{"type": "object", "required": required, "properties": props}}
	}
	str := map[string]interface{}{"type": "string"}
	return []realApp{
		{
			name: "petstore", envURL: "KR_E2E_PETSTORE_URL", defaultURL: "http://127.0.0.1:18101", ksuid: strings.Repeat("1", 40),
			paths: map[string]interface{}{
				"/api/v3/pet/findByStatus": map[string]interface{}{"get": op(p("query", "status", "string", "enum", []string{"available", "pending", "sold"}))},
				"/api/v3/pet/{petId}":      map[string]interface{}{"get": op(p("path", "petId", "integer", "format", "int64"))},
				"/api/v3/pet": map[string]interface{}{"post": op(body(map[string]interface{}{
					"id": map[string]interface{}{"type": "integer", "format": "int64"}, "name": str,
					"photoUrls": map[string]interface{}{"type": "array", "items": str},
					"status":    map[string]interface{}{"type": "string", "enum": []string{"available", "pending", "sold"}},
				}, "name", "photoUrls"))},
				"/api/v3/store/inventory": map[string]interface{}{"get": op()},
				"/api/v3/user/login":      map[string]interface{}{"get": op(p("query", "username", "string"), p("query", "password", "string"))},
				"/api/v3/user/logout":     map[string]interface{}{"get": op()},
			},
			want: []string{"GET /api/v3/pet/findByStatus", "GET /api/v3/pet/*", "POST /api/v3/pet", "GET /api/v3/store/inventory", "GET /api/v3/user/login", "GET /api/v3/user/logout"},
		},
		{
			name: "vampi", envURL: "KR_E2E_VAMPI_URL", defaultURL: "http://127.0.0.1:18102", ksuid: strings.Repeat("2", 40),
			paths: map[string]interface{}{
				"/users/v1":            map[string]interface{}{"get": op()},
				"/users/v1/{username}": map[string]interface{}{"get": op(p("path", "username", "string"))},
				"/users/v1/register": map[string]interface{}{"post": op(body(map[string]interface{}{
					"username": str, "password": str, "email": map[string]interface{}{"type": "string", "format": "email"},
				}, "username", "password", "email"))},
				"/users/v1/login": map[string]interface{}{"post": op(body(map[string]interface{}{"username": str, "password": str}, "username", "password"))},
				"/books/v1":       map[string]interface{}{"get": op()},
				"/books/v1/{book_title}": map[string]interface{}{"get": op(p("path", "book_title", "string"),
					p("header", "Authorization", "string", "default", "Bearer invalid"))},
			},
			want: []string{"GET /users/v1", "GET /users/v1/*", "POST /users/v1/register", "POST /users/v1/login", "GET /books/v1", "GET /books/v1/*"},
			setup: func(t *testing.T, base string) {
				// vampi answers 500 until its database is initialised
				resp, err := http.Get(base + "/createdb")
				if err != nil || resp.StatusCode != 200 {
					t.Fatalf("failed to initialise vampi: %v %v", err, resp)
				}
				resp.Body.Close()
			},
		},
		{
			name: "juiceshop", envURL: "KR_E2E_JUICESHOP_URL", defaultURL: "http://127.0.0.1:18103", ksuid: strings.Repeat("3", 40),
			paths: map[string]interface{}{
				"/rest/products/search":           map[string]interface{}{"get": op(p("query", "q", "string"))},
				"/api/Products":                   map[string]interface{}{"get": op()},
				"/api/Challenges":                 map[string]interface{}{"get": op()},
				"/rest/user/login":                map[string]interface{}{"post": op(body(map[string]interface{}{"email": str, "password": str}, "email", "password"))},
				"/rest/admin/application-version": map[string]interface{}{"get": op()},
				"/api/Users":                      map[string]interface{}{"get": op()},
				// a single segment route on a framework that answers /metrics/ like /metrics: kr used to probe
				// the route itself as its own wildcard baseline and discard it
				"/metrics": map[string]interface{}{"get": op()},
			},
			want: []string{"GET /rest/products/search", "GET /api/Products", "GET /api/Challenges", "POST /rest/user/login", "GET /rest/admin/application-version", "GET /api/Users", "GET /metrics"},
		},
		{
			name: "httpbin", envURL: "KR_E2E_HTTPBIN_URL", defaultURL: "http://127.0.0.1:18104", ksuid: strings.Repeat("4", 40),
			paths: map[string]interface{}{
				"/get": map[string]interface{}{"get": op()}, "/post": map[string]interface{}{"post": op()},
				"/put": map[string]interface{}{"put": op()}, "/delete": map[string]interface{}{"delete": op()},
				"/patch": map[string]interface{}{"patch": op()}, "/headers": map[string]interface{}{"get": op()},
				"/uuid": map[string]interface{}{"get": op()}, "/json": map[string]interface{}{"get": op()},
			},
			want: []string{"GET /get", "POST /post", "PUT /put", "DELETE /delete", "PATCH /patch", "GET /headers", "GET /uuid", "GET /json"},
		},
	}
}

func TestRealWorld(t *testing.T) {
	if os.Getenv("KR_E2E_REALWORLD") == "" {
		t.Skip("start realworld/docker-compose.yml and set KR_E2E_REALWORLD=1 to scan real applications")
	}
	apps := realApps()

	// one schema with every app's routes, plus decoys that exist nowhere
	schema := []map[string]interface{}{}
	for _, a := range apps {
		schema = append(schema, map[string]interface{}{"ksuid": a.ksuid, "url": a.name, "securityDefinitions": map[string]interface{}{}, "paths": a.paths})
	}
	decoy := func() map[string]interface{} { return map[string]interface{}{"description": "d", "operationId": "o"} }
	schema = append(schema, map[string]interface{}{"ksuid": strings.Repeat("9", 40), "url": "decoys", "securityDefinitions": map[string]interface{}{}, "paths": map[string]interface{}{
		"/v1/orders":               map[string]interface{}{"get": decoy(), "post": decoy()},
		"/v1/sessions":             map[string]interface{}{"delete": decoy()},
		"/v1/settings":             map[string]interface{}{"put": decoy(), "patch": decoy()},
		"/internal/health/details": map[string]interface{}{"get": decoy()},
		"/api/v3/admin":            map[string]interface{}{"get": decoy()},
		"/api/Nope":                map[string]interface{}{"get": decoy()},
		"/rest/nope/deeper":        map[string]interface{}{"post": decoy()},
		"/users/v2":                map[string]interface{}{"get": decoy()},
		"/graphql":                 map[string]interface{}{"get": decoy(), "post": decoy()},
	}})
	b, _ := json.Marshal(schema)
	dir := t.TempDir()
	src, kite := filepath.Join(dir, "realworld.json"), filepath.Join(dir, "realworld.kite")
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := kr(t, "", "-q", "kb", "compile", src, kite); r.ExitCode != 0 {
		t.Fatalf("kb compile failed: %s", tail(r.Stderr))
	}

	for _, a := range apps {
		a := a
		t.Run(a.name, func(t *testing.T) {
			base := os.Getenv(a.envURL)
			if base == "" {
				base = a.defaultURL
			}
			client := http.Client{Timeout: 5 * time.Second}
			if resp, err := client.Get(base + "/"); err != nil {
				t.Fatalf("%s is not reachable at %s: %v", a.name, base, err)
			} else {
				resp.Body.Close()
			}
			if a.setup != nil {
				a.setup(t, base)
			}
			// the default two phase scan, as most people run it, and a full scan of every route
			for _, mode := range [][]string{nil, {"--kitebuilder-full-scan"}} {
				hits := scan(t, "n\n", append([]string{"scan", base, "-w", kite}, mode...)...)
				t.Logf("%v: %v", mode, keys(hits))
				assertExactly(t, hits, a.want...)
			}

			// deeper preflights take their baselines closer to the route. A route ending in a path parameter
			// (/users/v1/{username}) then answers like the random paths next to it, so it can be missed; that
			// is the trade off of -d. It must never cause false positives
			hits := scan(t, "n\n", "scan", base, "-w", kite, "--kitebuilder-full-scan", "-d", "2")
			t.Logf("[-d 2]: %v", keys(hits))
			for _, k := range keys(hits) {
				ok := false
				for _, w := range a.want {
					ok = ok || matches(w, k)
				}
				if !ok {
					t.Errorf("-d 2: false positive %s", k)
				}
			}
		})
	}
}
