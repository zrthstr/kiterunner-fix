package proute

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/zrthstr/kiterunner-fix/pkg/kitebuilder"
)

// turning any schema into requests must never panic, and the requests must be well formed
func FuzzSchemaToRequests(f *testing.F) {
	if b, err := os.ReadFile("../kitebuilder/testdata/simple.json"); err == nil {
		f.Add(b)
	}
	for _, s := range []string{
		`[{"ksuid":"x","url":"u","paths":{"/a/{id}?page=1":{"get":{"parameters":[{"in":"path","name":"id","type":"integer"},{"in":"header","name":"${h}","type":"string"}]}}}}]`,
		`[{"ksuid":"x","url":"u","paths":{"/x":{"post":{"consumes":["application/xml"],"parameters":[{"in":"body","name":"Request body","schema":{"type":"object","properties":{"a b":{"type":"array","items":{"type":"object"}}}}}]}}}}]`,
		`[{"ksuid":"x","url":"u","paths":{"/f":{"post":{"consumes":["multipart/form-data"],"parameters":[{"in":"formData","name":"f","type":"file"},{"in":"query","name":"e","type":"string","enum":[1,"a",true]}]}}}}]`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = kitebuilder.StreamJSON(bytes.NewReader(data), func(api kitebuilder.API) error {
			a, _ := FromKitebuilderAPI(api)
			routes, _ := ToKiterunnerRoutes(a)
			for _, r := range routes {
				for _, h := range r.Headers {
					if !validHeaderName(h.Key) {
						t.Errorf("invalid header name %q", h.Key)
					}
				}
				if bytes.ContainsRune(r.Path, '?') {
					t.Errorf("query left in the path %q", r.Path)
				}
				if strings.ContainsAny(string(r.Method), " \\t") {
					t.Errorf("invalid method %q", r.Method)
				}
			}
			return nil
		})
	})
}

// a .kite file is a protobuf; decoding a corrupt one must return an error, not panic
func FuzzDecodeProtoAPI(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})
	f.Add([]byte("not a kite file"))
	f.Fuzz(func(t *testing.T, data []byte) {
		apis, err := DecodeProtoAPI(bytes.NewReader(data))
		if err != nil {
			return
		}
		for _, a := range apis {
			routes, _ := ToKiterunnerRoutes(a)
			_ = routes
		}
	})
}
