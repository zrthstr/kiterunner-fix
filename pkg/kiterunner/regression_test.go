package kiterunner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zrthstr/kiterunner-fix/pkg/http"
)

// A pooled job must not carry the wildcard baselines of the previous target that used it,
// otherwise baselines leak between targets and valid results get discarded.
func TestReleaseJobClearsBaselines(t *testing.T) {
	// releaseJob calls reset before pooling; assert on reset directly since the pool may hand back another job
	j := &job{}
	j.wcr = append(j.wcr, WildcardResponse{DefaultStatusCode: 200, DefaultWordCount: 2, DefaultLineCount: 1})
	j.reset()
	assert.Empty(t, j.wcr)
}

// Pooled channels must have at least the requested capacity. An undersized (e.g. unbuffered) channel
// left behind by an engine with fewer preflight routes deadlocks the preflight workers.
func TestAcquireSubpathBaselineChanCapacity(t *testing.T) {
	releaseSubpathBaselineChan(make(subpathBaselineChan))
	c := acquireSubpathBaselineChan(5)
	assert.True(t, cap(c) >= 5, "got capacity %d", cap(c))

	releaseSubpathRoutesChan(make(subpathRoutesChan))
	r := acquireSubpathRoutesChan(3)
	assert.True(t, cap(r) >= 3, "got capacity %d", cap(r))
}

func TestWildcardResponseValidatorStatusCodeDistance(t *testing.T) {
	// baseline: 404 "not found" -> 2 words, 1 line, 9 bytes
	baseline := []WildcardResponse{{
		DefaultStatusCode:     404,
		DefaultContentLength:  9,
		AdjustedContentLength: 9,
		DefaultWordCount:      2,
		DefaultLineCount:      1,
	}}
	route := &http.Route{Method: http.PUT, Path: []byte("/api/v1/users")}
	resp := func(sc int) http.Response {
		return http.Response{
			StatusCode: sc, Words: 2, Lines: 1, BodyLength: 6,
			OriginRequest: http.Request{Route: route},
		}
	}
	v := &WildcardResponseValidator{}

	// a 200 is nowhere near the 404 baseline, so matching word/line counts must not discard it
	assert.NoError(t, v.Validate(resp(200), baseline, nil))
	// a 403 that only shares the word/line count with the 404 baseline is a different page and is kept
	assert.NoError(t, v.Validate(resp(403), baseline, nil))
	// the load balancer case: the identical body with a nearby status is discarded on length
	sameBody := resp(403)
	sameBody.BodyLength = 9
	assert.Equal(t, ErrLengthMatch, v.Validate(sameBody, baseline, nil))
	// the same status with a matching word/line count (a soft 404 with a varying length) is discarded
	assert.Equal(t, ErrWordCountMatch, v.Validate(resp(404), baseline, nil))
	// a 5xx far above the baseline is also not compared
	assert.NoError(t, v.Validate(resp(500), baseline, nil))
}
