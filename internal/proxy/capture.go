package proxy

import (
	"bytes"
	"io"
)

type BodyCapture struct {
	max           int64
	store         bool
	omittedReason string
	size          int64
	truncated     bool
	buf           bytes.Buffer
}

type BodySnapshot struct {
	Body          []byte
	Size          int64
	Truncated     bool
	OmittedReason string
	StoredBytes   int
}

func NewBodyCapture(max int64, store bool, omittedReason string) *BodyCapture {
	if max < 0 {
		max = 0
	}
	return &BodyCapture{
		max:           max,
		store:         store,
		omittedReason: omittedReason,
	}
}

func (c *BodyCapture) WriteObserved(p []byte) {
	if len(p) == 0 {
		return
	}
	c.size += int64(len(p))
	if !c.store {
		return
	}
	if c.max == 0 {
		c.buf.Write(p)
		return
	}
	remaining := c.max - int64(c.buf.Len())
	if remaining <= 0 {
		c.truncated = true
		return
	}
	if int64(len(p)) > remaining {
		c.buf.Write(p[:remaining])
		c.truncated = true
		return
	}
	c.buf.Write(p)
}

func (c *BodyCapture) Snapshot() BodySnapshot {
	body := make([]byte, c.buf.Len())
	copy(body, c.buf.Bytes())
	return BodySnapshot{
		Body:          body,
		Size:          c.size,
		Truncated:     c.truncated,
		OmittedReason: c.omittedReason,
		StoredBytes:   len(body),
	}
}

type captureReadCloser struct {
	rc      io.ReadCloser
	capture *BodyCapture
}

func WrapReadCloser(rc io.ReadCloser, capture *BodyCapture) io.ReadCloser {
	if rc == nil || capture == nil {
		return rc
	}
	return &captureReadCloser{rc: rc, capture: capture}
}

func (c *captureReadCloser) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 {
		c.capture.WriteObserved(p[:n])
	}
	return n, err
}

func (c *captureReadCloser) Close() error {
	return c.rc.Close()
}
