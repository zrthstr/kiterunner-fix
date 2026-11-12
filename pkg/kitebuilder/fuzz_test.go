package kitebuilder

import (
	"bytes"
	"os"
	"testing"
)

// both schema loaders must survive any input and agree on how many APIs it contains
func FuzzLoadJSON(f *testing.F) {
	if b, err := os.ReadFile("testdata/simple.json"); err == nil {
		f.Add(b)
	}
	for _, s := range []string{`[]`, `[{}]`, `[[{}]]`, `{}`, `[1]`, `[{"paths":{"/a":{"get":{"parameters":[{"in":"query","name":"q","type":["string","null"]}]}}}}]`,
		`[{"ksuid":"x","url":"u","paths":{"/a":{"post":{"parameters":[{"in":"body","name":"b","schema":{"type":"object","additionalProperties":true}}]}}}}]`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		slow, slowErr := SlowLoadJSONBytes(data)
		var streamed []API
		streamErr := StreamJSON(bytes.NewReader(data), func(a API) error { streamed = append(streamed, a); return nil })
		if (slowErr == nil) != (streamErr == nil) {
			// only compare clean loads; both report per api problems differently from syntax errors
			return
		}
		if slowErr == nil && len(slow) != len(streamed) {
			t.Errorf("SlowLoadJSONBytes found %d apis, StreamJSON %d", len(slow), len(streamed))
		}
	})
}
