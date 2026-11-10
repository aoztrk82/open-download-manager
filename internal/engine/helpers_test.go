package engine

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

var timeZero time.Time

func itoa(n int) string { return strconv.Itoa(n) }

type countWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	k, err := c.ResponseWriter.Write(p)
	c.n.Add(int64(k))
	return k, err
}
