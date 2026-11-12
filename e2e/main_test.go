// Package e2e builds the real kr binary and runs it against in-process HTTP servers whose behaviour is known,
// asserting on what kr reports (false positives / false negatives) and on what it sends.
//
// It does not depend on the network, upstream kiterunner, or the user's ~/.kiterunner.yaml / wordlist cache.
// Set KR_E2E_RACE=1 to build kr with the race detector; any reported race fails the test that triggered it.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var (
	krBin  string
	krHome string // isolated $HOME for every kr invocation
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kr-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	krBin = filepath.Join(dir, "kr")
	krHome = filepath.Join(dir, "home")
	if err := os.MkdirAll(krHome, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	args := []string{"build", "-o", krBin}
	if os.Getenv("KR_E2E_RACE") == "1" {
		args = append(args, "-race")
	}
	args = append(args, "../cmd/kiterunner")
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build kr: %v\n%s", err, out)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type run struct {
	Stdout, Stderr string
	ExitCode       int
	Duration       time.Duration
}

// kr runs the binary with args. stdin is fed to the process (use "" for none, which is EOF).
func kr(t *testing.T, stdin string, args ...string) run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, krBin, args...)
	cmd.Env = append(os.Environ(), "HOME="+krHome, "NO_COLOR=1")
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	r := run{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(start)}
	if ctx.Err() != nil {
		t.Fatalf("kr %v timed out\nstderr:\n%s", args, tail(r.Stderr))
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		r.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run kr: %v", err)
	}
	if strings.Contains(r.Stderr, "WARNING: DATA RACE") {
		t.Fatalf("data race in kr %v:\n%s", args, r.Stderr)
	}
	if strings.Contains(r.Stderr, "panic:") || strings.Contains(r.Stderr, "goroutine ") && strings.Contains(r.Stderr, "[running]") {
		t.Fatalf("kr %v panicked:\n%s", args, tail(r.Stderr))
	}
	return r
}

func tail(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return strings.Join(lines, "\n")
}

// hit is one result line of `kr -o json`
type hit struct {
	Method    string `json:"method"`
	Target    string `json:"target"`
	Path      string `json:"path"`
	Responses []struct {
		URI string `json:"uri"`
		SC  int    `json:"sc"`
		Len int    `json:"len"`
	} `json:"responses"`
}

func (h hit) key() string { return h.Method + " " + h.Path }

// scan runs kr with json output and returns the results. It fails the test on a non zero exit.
func scan(t *testing.T, stdin string, args ...string) []hit {
	t.Helper()
	r := kr(t, stdin, append([]string{"-q", "-o", "json"}, args...)...)
	if r.ExitCode != 0 {
		t.Fatalf("kr %v exited %d\nstderr:\n%s", args, r.ExitCode, tail(r.Stderr))
	}
	return parseHits(t, r.Stdout)
}

func parseHits(t *testing.T, stdout string) []hit {
	t.Helper()
	var hits []hit
	sc := bufio.NewScanner(strings.NewReader(stdout))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var h hit
		if err := json.Unmarshal([]byte(line), &h); err != nil {
			t.Fatalf("stdout must only contain json results, got %q: %v", line, err)
		}
		if h.Method == "" {
			t.Fatalf("stdout must only contain results, got %q", line)
		}
		hits = append(hits, h)
	}
	return hits
}

func keys(hits []hit) []string {
	ret := make([]string, 0, len(hits))
	for _, h := range hits {
		ret = append(ret, h.key())
	}
	sort.Strings(ret)
	return ret
}

// matches reports whether a result key matches an expectation, where * matches exactly one path segment
func matches(pattern, key string) bool {
	p, k := strings.Split(pattern, "/"), strings.Split(key, "/")
	if len(p) != len(k) {
		return false
	}
	for i := range p {
		if p[i] != "*" && p[i] != k[i] {
			return false
		}
	}
	return true
}

// assertExactly checks that hits are exactly the expected set (no false positives, no false negatives,
// no duplicates) and reports both lists on failure
func assertExactly(t *testing.T, hits []hit, expected ...string) {
	t.Helper()
	var falsePos, dupes []string
	found := make(map[string]int)
	seen := make(map[string]bool)
	for _, k := range keys(hits) {
		if seen[k] {
			dupes = append(dupes, k)
		}
		seen[k] = true
		ok := false
		for _, e := range expected {
			if matches(e, k) {
				found[e]++
				ok = true
				break
			}
		}
		if !ok {
			falsePos = append(falsePos, k)
		}
	}
	var falseNeg []string
	for _, e := range expected {
		if found[e] == 0 {
			falseNeg = append(falseNeg, e)
		}
	}
	if len(falsePos)+len(falseNeg)+len(dupes) > 0 {
		t.Errorf("results did not match\n  false positives: %v\n  false negatives: %v\n  duplicates:      %v\n  got:             %v",
			falsePos, falseNeg, dupes, keys(hits))
	}
}
