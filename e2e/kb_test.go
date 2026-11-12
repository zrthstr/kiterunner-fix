package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// normalizedJSON converts a kite file back to json with `kr kb convert` and returns it in a canonical form
func normalizedJSON(t *testing.T, kite string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.json")
	if r := kr(t, "", "-q", "kb", "convert", kite, out); r.ExitCode != 0 {
		t.Fatalf("kb convert failed: %s", tail(r.Stderr))
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var apis []map[string]interface{}
	if err := json.Unmarshal(b, &apis); err != nil {
		t.Fatal(err)
	}
	sort.Slice(apis, func(i, j int) bool { return apis[i]["ksuid"].(string) < apis[j]["ksuid"].(string) })
	canonical, _ := json.Marshal(apis)
	return string(canonical)
}

func TestKbCompile(t *testing.T) {
	kite, schemaJSON := testKite(t)

	// compiling from stdin used a different loader that dropped all parameters
	fromStdin := filepath.Join(t.TempDir(), "stdin.kite")
	in, _ := os.ReadFile(schemaJSON)
	if r := kr(t, string(in), "-q", "kb", "compile", "-", fromStdin); r.ExitCode != 0 {
		t.Fatalf("kb compile from stdin failed: %s", tail(r.Stderr))
	}
	fileJSON, stdinJSON := normalizedJSON(t, kite), normalizedJSON(t, fromStdin)
	if fileJSON != stdinJSON {
		t.Errorf("compiling from a file and from stdin must produce the same schema\nfile:  %s\nstdin: %s", fileJSON, stdinJSON)
	}
	for _, want := range []string{"example-token", "example-query", "Nathan Reline", "application/json"} {
		if !strings.Contains(stdinJSON, want) {
			t.Errorf("compiled schema lost %q", want)
		}
	}
}

func TestKbParse(t *testing.T) {
	_, schemaJSON := testKite(t)
	for _, src := range []string{"file", "stdin"} {
		var r run
		if src == "file" {
			r = kr(t, "", "-q", "kb", "parse", schemaJSON)
		} else {
			in, _ := os.ReadFile(schemaJSON)
			r = kr(t, string(in), "-q", "kb", "parse", "-")
		}
		if r.ExitCode != 0 {
			t.Fatalf("%s: kb parse failed: %s", src, tail(r.Stderr))
		}
		if !strings.Contains(r.Stderr+r.Stdout, "post /onigokko/player {header:Token(string)} {query:QueryParam(string)}") {
			t.Errorf("%s: kb parse must show the route parameters:\n%s", src, tail(r.Stderr))
		}
	}

	r := kr(t, "", "-q", "kb", "parse", "/does/not/exist.json")
	if r.ExitCode == 0 {
		t.Error("parsing a missing file must fail")
	}
}

func TestKbConvert(t *testing.T) {
	dir := t.TempDir()
	words := writeFile(t, "words.txt", "/a\n/b/c\n/d\n")
	kite := filepath.Join(dir, "words.kite")
	back := filepath.Join(dir, "back.txt")
	for _, args := range [][]string{{words, kite}, {kite, back}} {
		if r := kr(t, "", append([]string{"-q", "kb", "convert"}, args...)...); r.ExitCode != 0 {
			t.Fatalf("kb convert %v failed: %s", args, tail(r.Stderr))
		}
	}
	got, _ := os.ReadFile(back)
	lines := strings.Fields(string(got))
	sort.Strings(lines)
	if strings.Join(lines, " ") != "/a /b/c /d" {
		t.Errorf("txt -> kite -> txt round trip changed the routes: %q", got)
	}

	// the compiled test schema converts to txt with every route
	testKiteFile, _ := testKite(t)
	routes := filepath.Join(dir, "routes.txt")
	if r := kr(t, "", "-q", "kb", "convert", testKiteFile, routes); r.ExitCode != 0 {
		t.Fatalf("kite -> txt failed: %s", tail(r.Stderr))
	}
	b, _ := os.ReadFile(routes)
	for _, want := range []string{"/api/v1/users", "/onigokko/player", "/graphql"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("converted routes are missing %s", want)
		}
	}
}

func TestKbReplay(t *testing.T) {
	kite, _ := testKite(t)
	tg := fixtureTarget(t, "plain-404", false)
	ksuid := strings.Repeat("b", 40)

	t.Run("ksuid method route host", func(t *testing.T) {
		before := len(tg.requestsTo("POST", "/onigokko/player"))
		r := kr(t, "", "-q", "kb", "replay", "-w", kite, ksuid, "POST", "/onigokko/player", tg.URL)
		if r.ExitCode != 0 {
			t.Fatalf("replay failed: %s", tail(r.Stderr))
		}
		if !strings.Contains(r.Stdout, "HTTP/1.1 201") {
			t.Errorf("replay should print the 201 response:\n%s", r.Stdout)
		}
		if len(tg.requestsTo("POST", "/onigokko/player")) != before+1 {
			t.Error("replay did not send the request")
		}
	})

	t.Run("without host only prints", func(t *testing.T) {
		before := len(tg.requests())
		r := kr(t, "", "-q", "kb", "replay", "-w", kite, ksuid, "POST", "/onigokko/player")
		if r.ExitCode != 0 || !strings.Contains(r.Stdout, "Token: example-token") {
			t.Errorf("replay without a host should print the request, exit=%d stdout=%s", r.ExitCode, r.Stdout)
		}
		if len(tg.requests()) != before {
			t.Error("replay without a host must not send anything")
		}
	})

	t.Run("result line", func(t *testing.T) {
		out := kr(t, "", "-q", "-o", "text", "scan", tg.URL, "-w", kite, "--kitebuilder-full-scan", "--filter-api", ksuid)
		line := ""
		for _, l := range strings.Split(out.Stdout, "\n") {
			l = strings.TrimSpace(regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(l, ""))
			if strings.HasPrefix(l, "POST") {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("no POST result line in:\n%s", out.Stdout)
		}
		r := kr(t, "", "-q", "kb", "replay", "-w", kite, line)
		if r.ExitCode != 0 || !strings.Contains(r.Stdout, "HTTP/1.1 201") {
			t.Errorf("replaying a scan result line failed, exit=%d\n%s\n%s", r.ExitCode, r.Stdout, tail(r.Stderr))
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		for _, args := range [][]string{{"not a result line"}, {"a", "b"}} {
			r := kr(t, "", append([]string{"-q", "kb", "replay", "-w", kite}, args...)...)
			if r.ExitCode == 0 {
				t.Errorf("replay %v must fail", args)
			}
		}
	})
}
