package http

import (
	"bufio"
	"net"
	nethttp "net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

// serve accepts connections and hands each one to handle, counting the requests read
func serve(t *testing.T, handle func(n int64, c net.Conn)) (addr string, requests *int64) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	var count int64
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if _, err := nethttp.ReadRequest(bufio.NewReader(c)); err != nil {
					return
				}
				handle(atomic.AddInt64(&count, 1), c)
			}(c)
		}
	}()
	return l.Addr().String(), &count
}

func doGet(addr string, timeout time.Duration) error {
	c := NewHTTPClient(addr, false)
	c.ReadTimeout = timeout
	c.WriteTimeout = timeout
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)
	req.SetRequestURI("http://" + addr + "/")
	return c.Do(req, resp)
}

// A timeout must not be retried, otherwise --timeout is multiplied by fasthttp's retry count
func TestClientDoesNotRetryTimeouts(t *testing.T) {
	addr, requests := serve(t, func(_ int64, c net.Conn) {
		time.Sleep(2 * time.Second)
	})

	start := time.Now()
	err := doGet(addr, 300*time.Millisecond)
	assert.Error(t, err)
	assert.Less(t, int64(time.Since(start)), int64(time.Second), "request should give up after one timeout")
	// give a (wrong) retry a moment to arrive before counting
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(1), atomic.LoadInt64(requests))
}

// A connection closed before the response (e.g. a stale keep-alive) must be retried
func TestClientRetriesClosedConnection(t *testing.T) {
	addr, requests := serve(t, func(n int64, c net.Conn) {
		if n == 1 {
			return // close without responding
		}
		c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
	})

	assert.NoError(t, doGet(addr, time.Second))
	assert.Equal(t, int64(2), atomic.LoadInt64(requests))
}
