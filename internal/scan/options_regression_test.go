package scan

import (
	"testing"

	"github.com/assetnote/kiterunner/pkg/http"
	"github.com/stretchr/testify/assert"
)

func paths(routes []*http.Route) []string {
	ret := make([]string, 0, len(routes))
	for _, v := range routes {
		ret = append(ret, string(v.Path))
	}
	return ret
}

func TestWordRoutes(t *testing.T) {
	exts := []string{"php", "bak"}

	assert.Equal(t, []string{"/secret", "/secret.php", "/secret.bak"},
		paths(wordRoutes([]byte("/secret"), exts, false)), "normal mode appends extensions")

	assert.Equal(t, []string{"/secret.php", "/secret.bak"},
		paths(wordRoutes([]byte("/secret.%EXT%"), exts, true)), "dirsearch mode expands %EXT% and never requests it raw")

	assert.Equal(t, []string{"/nope"},
		paths(wordRoutes([]byte("/nope"), exts, true)), "dirsearch mode leaves words without %EXT% alone")

	assert.Equal(t, []string{"/secret.%EXT%"},
		paths(wordRoutes([]byte("/secret.%EXT%"), nil, true)), "no extensions means nothing to expand")
}

func TestFilteredRoutesForceMethod(t *testing.T) {
	s := NewDefaultScanOptions()
	s.Routes = []*http.Route{
		{Method: http.GET, Path: []byte("/a"), Source: "x"},
		{Method: http.POST, Path: []byte("/b"), Source: "y"},
	}
	assert.NoError(t, ForceMethod("put")(s))

	for _, v := range s.FilteredRoutes() {
		assert.Equal(t, http.PUT, v.Method)
	}

	assert.Error(t, ForceMethod("BOGUS")(s))
}
