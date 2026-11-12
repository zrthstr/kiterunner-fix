package kiterunner

import (
	"sync"

	"github.com/assetnote/kiterunner/pkg/http"
)

type job struct {
	t     *http.Target
	routes []*http.Route

	wcr []WildcardResponse

	client    *http.HTTPClient
}

func (j *job) HTTPRequest() *http.Request {
	r := http.AcquireRequest()
	r.Target = j.t
	return r
}

func (j *job) reset() {
	j.t = nil
	j.routes = j.routes[:0]
	// jobs are pooled and the baselines are appended on acquire. Without clearing these, a reused job keeps
	// the wildcard baselines of whatever target used it previously, and valid results get discarded
	j.wcr = j.wcr[:0]
	j.client = nil
}

var (
	jobPool sync.Pool
)

// acquireJob retrieves a host from the shared header pool
func acquireJob() *job {
	v := jobPool.Get()
	if v == nil {
		return &job{
			// we always expect at least one...
			wcr: make([]WildcardResponse, 0, 1),
		}
	}
	return v.(*job)
}

// releaseJob releases a host into the shared header pool
func releaseJob(h *job) {
	h.reset()
	jobPool.Put(h)
}

type subpathBaselineChan chan *subpathBaseline

var (
	jobSemPool sync.Pool
)

// acquireJobSem retrieves a job semaphore from the pool. this can only be initialized once
// TODO: make this abstraction more logical and tied to the config value
func acquireSubpathBaselineChan(size int) subpathBaselineChan {
	// pooled channels may have been created with a different capacity (e.g. by an engine with a different number
	// of preflight routes). an undersized channel deadlocks the preflight workers, so only reuse ones that fit
	if v, ok := jobSemPool.Get().(subpathBaselineChan); ok && cap(v) >= size {
		return v
	}
	return make(subpathBaselineChan, size)
}

// releaseJob releases a host into the shared header pool
func releaseSubpathBaselineChan(h subpathBaselineChan) {
	jobSemPool.Put(h)
}

type subpathRoutesChan chan subpathRoutes

var (
	subpathRoutesPool sync.Pool
)

// acquireJobSem retrieves a job semaphore from the pool. this can only be initialized once
// TODO: make this abstraction more logical and tied to the config value
func acquireSubpathRoutesChan(size int) subpathRoutesChan {
	if v, ok := subpathRoutesPool.Get().(subpathRoutesChan); ok && cap(v) >= size {
		return v
	}
	return make(subpathRoutesChan, size)
}

// releaseJob releases a host into the shared header pool
func releaseSubpathRoutesChan(h subpathRoutesChan) {
	subpathRoutesPool.Put(h)
}

var (
	waitgroupPool sync.Pool
)

func acquireWaitGroup() *sync.WaitGroup{
	v := waitgroupPool.Get()
	if v == nil {
		v := sync.WaitGroup{}
		return &v
	}
	return v.(*sync.WaitGroup)
}

// releaseJob releases a host into the shared header pool
func releaseWaitGroup(v *sync.WaitGroup) {
	waitgroupPool.Put(v)
}
