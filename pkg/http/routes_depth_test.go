package http

import "testing"

func TestGetDepth(t *testing.T) {
	for _, tc := range []struct {
		path  string
		depth int64
		want  string
	}{
		{"/admin", 1, ""},                  // a single segment route lives in the docroot
		{"/admin", 2, ""},                  //
		{"/admin/", 1, ""},                 //
		{"/api/Products", 1, "/api"},       //
		{"/api/Products", 2, "/api"},       // never the route itself
		{"/users/v1/name", 2, "/users/v1"}, //
		{"/users/v1/name", 1, "/users"},    //
		{"/a/b/c/d", 2, "/a/b"},            //
		{"a/b", 1, "/a"},                   // missing leading slash
		{"/api/v3/pet/{petId}", 1, "/api"}, //
		{"/api/Products", 0, ""},           // depth 0: everything in the docroot
		{"", 1, ""},                        //
	} {
		if got := getDepth(tc.path, tc.depth); got != tc.want {
			t.Errorf("getDepth(%q, %d) = %q, want %q", tc.path, tc.depth, got, tc.want)
		}
	}
}
