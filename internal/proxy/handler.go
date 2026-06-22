package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"requestlens/internal/config"
	"requestlens/internal/db"
)

type Handler struct {
	store              *db.Store
	defaultMaxBodySize int64
	transport          http.RoundTripper
}

func NewHandler(store *db.Store, cfg config.Config) *Handler {
	return &Handler{
		store:              store,
		defaultMaxBodySize: cfg.DefaultMaxBodySize,
		transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			DisableCompression:    true,
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestID := newRequestID()
	clientIP := ClientIP(r)
	originalURL := absoluteURL(r)
	isWebSocket := IsWebSocketRequest(r)

	enabled := true
	rules, err := h.store.ListRules(r.Context(), db.RuleFilter{Enabled: &enabled})
	if err != nil {
		http.Error(w, "failed to load proxy rules", http.StatusInternalServerError)
		log.Printf("load rules: %v", err)
		return
	}

	rule, ok := matchRule(rules, r.URL.Path)
	if !ok {
		http.Error(w, "RequestLens: no proxy rule matched this path", http.StatusNotFound)
		h.insertLog(r.Context(), db.HTTPLog{
			RequestID:      requestID,
			StartedAt:      started.UTC().Format(time.RFC3339Nano),
			FinishedAt:     time.Now().UTC().Format(time.RFC3339Nano),
			DurationMS:     time.Since(started).Milliseconds(),
			Method:         r.Method,
			OriginalURL:    originalURL,
			Scheme:         requestScheme(r),
			Host:           r.Host,
			Path:           r.URL.Path,
			Query:          r.URL.RawQuery,
			RequestHeaders: HeadersToJSON(r.Header, r.Host, true),
			ResponseStatus: http.StatusNotFound,
			ErrorMessage:   "no proxy rule matched this path",
			ClientIP:       clientIP,
			IsWebSocket:    isWebSocket,
		})
		return
	}

	target, err := buildTargetURL(rule, r.URL)
	if err != nil {
		http.Error(w, "RequestLens: invalid target URL", http.StatusBadGateway)
		h.insertLog(r.Context(), db.HTTPLog{
			RuleID:         &rule.ID,
			RequestID:      requestID,
			StartedAt:      started.UTC().Format(time.RFC3339Nano),
			FinishedAt:     time.Now().UTC().Format(time.RFC3339Nano),
			DurationMS:     time.Since(started).Milliseconds(),
			Method:         r.Method,
			OriginalURL:    originalURL,
			Scheme:         requestScheme(r),
			Host:           r.Host,
			Path:           r.URL.Path,
			Query:          r.URL.RawQuery,
			RequestHeaders: HeadersToJSON(r.Header, r.Host, rule.RedactSensitiveHeaders),
			ResponseStatus: http.StatusBadGateway,
			ErrorMessage:   err.Error(),
			ClientIP:       clientIP,
			IsWebSocket:    isWebSocket,
		})
		return
	}

	if strings.EqualFold(rule.CorsMode, "local") && r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		writeLocalCORSHeaders(w.Header(), r)
		w.WriteHeader(http.StatusNoContent)
		h.insertLog(r.Context(), db.HTTPLog{
			RuleID:         &rule.ID,
			RequestID:      requestID,
			StartedAt:      started.UTC().Format(time.RFC3339Nano),
			FinishedAt:     time.Now().UTC().Format(time.RFC3339Nano),
			DurationMS:     time.Since(started).Milliseconds(),
			Method:         r.Method,
			OriginalURL:    originalURL,
			ProxiedURL:     target.String(),
			Scheme:         requestScheme(r),
			Host:           r.Host,
			Path:           r.URL.Path,
			Query:          r.URL.RawQuery,
			RequestHeaders: HeadersToJSON(r.Header, r.Host, rule.RedactSensitiveHeaders),
			ResponseStatus: http.StatusNoContent,
			ClientIP:       clientIP,
		})
		return
	}

	maxBodySize := rule.MaxBodySize
	if maxBodySize < 0 {
		maxBodySize = h.defaultMaxBodySize
	}

	requestContentType := r.Header.Get("Content-Type")
	reqClass := ClassifyContent(requestContentType)
	storeReqBody, reqOmittedReason := CapturePlan(rule.CaptureRequestBody, reqClass, rule.AllowBinaryPreview, rule.AllowStreamPreview)
	reqCapture := NewBodyCapture(maxBodySize, storeReqBody, reqOmittedReason)
	var upstreamBody io.ReadCloser = http.NoBody
	upstreamContentLength := r.ContentLength
	if r.Body != nil && r.Body != http.NoBody {
		body, contentLength, err := prepareRequestBodyForForward(r, reqCapture, storeReqBody, maxBodySize, reqClass)
		if err != nil {
			http.Error(w, "RequestLens failed to read request body: "+err.Error(), http.StatusBadRequest)
			h.insertLog(r.Context(), db.HTTPLog{
				RuleID:                   &rule.ID,
				RequestID:                requestID,
				StartedAt:                started.UTC().Format(time.RFC3339Nano),
				FinishedAt:               time.Now().UTC().Format(time.RFC3339Nano),
				DurationMS:               time.Since(started).Milliseconds(),
				Method:                   r.Method,
				OriginalURL:              originalURL,
				ProxiedURL:               target.String(),
				Scheme:                   requestScheme(r),
				Host:                     r.Host,
				Path:                     r.URL.Path,
				Query:                    r.URL.RawQuery,
				RequestHeaders:           HeadersToJSON(r.Header, r.Host, rule.RedactSensitiveHeaders),
				RequestContentType:       requestContentType,
				RequestBody:              reqCapture.Snapshot().Body,
				RequestBodyTruncated:     reqCapture.Snapshot().Truncated,
				RequestBodySize:          reqCapture.Snapshot().Size,
				RequestBodyOmittedReason: reqCapture.Snapshot().OmittedReason,
				ResponseStatus:           http.StatusBadRequest,
				ErrorMessage:             err.Error(),
				ClientIP:                 clientIP,
				IsWebSocket:              isWebSocket,
			})
			return
		}
		upstreamBody = body
		upstreamContentLength = contentLength
	}

	var responseStatus int
	var responseHeaders string
	var responseContentType string
	var resClass Classification
	var resCapture = NewBodyCapture(maxBodySize, false, "no response body")
	var proxyError error

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), upstreamBody)
	if err != nil {
		proxyError = err
		responseStatus = http.StatusBadGateway
		http.Error(w, "RequestLens upstream request error: "+err.Error(), http.StatusBadGateway)
	} else {
		outReq.Header = cloneHeader(r.Header)
		outReq.ContentLength = upstreamContentLength
		outReq.Trailer = cloneHeader(r.Trailer)
		outReq.RemoteAddr = r.RemoteAddr
		outReq.RequestURI = ""
		if rule.PreserveHost {
			outReq.Host = r.Host
		} else {
			outReq.Host = target.Host
		}
		removeHopByHopHeaders(outReq.Header)
		// RequestLens is a body inspection tool. Asking upstream for an uncompressed
		// response keeps captured JSON/Text readable in the inspector.
		outReq.Header.Del("Accept-Encoding")
		AppendForwardedHeaders(outReq, r, clientIP)
		resp, err := h.transport.RoundTrip(outReq)
		if err != nil {
			proxyError = err
			responseStatus = http.StatusBadGateway
			http.Error(w, "RequestLens upstream error: "+err.Error(), http.StatusBadGateway)
		} else {
			responseStatus = resp.StatusCode
			if rule.RewriteRedirect {
				rewriteLocationHeader(resp, rule, r)
			}
			if strings.EqualFold(rule.CorsMode, "local") {
				writeLocalCORSHeaders(resp.Header, r)
			}
			responseContentType = resp.Header.Get("Content-Type")
			resClass = ClassifyContent(responseContentType)
			if isWebSocket || resp.StatusCode == http.StatusSwitchingProtocols {
				resClass.IsStream = true
			}
			if hasChunkedTransfer(resp) && resp.ContentLength < 0 {
				resClass.IsStream = true
			}
			storeResBody, resOmittedReason := CapturePlan(rule.CaptureResponseBody, resClass, rule.AllowBinaryPreview, rule.AllowStreamPreview)
			resCapture = NewBodyCapture(maxBodySize, storeResBody, resOmittedReason)
			if rule.CaptureResponseHeaders {
				responseHeaders = HeadersToJSON(resp.Header, "", rule.RedactSensitiveHeaders)
			}
			if resp.StatusCode == http.StatusSwitchingProtocols {
				proxyError = tunnelUpgradedConnection(w, resp)
			} else {
				removeHopByHopHeaders(resp.Header)
				copyHeader(w.Header(), resp.Header)
				w.WriteHeader(resp.StatusCode)
				if responseAllowsBody(r.Method, resp.StatusCode) && resp.Body != nil {
					wrappedBody := WrapReadCloser(resp.Body, resCapture)
					if _, err := copyAndFlush(w, wrappedBody); err != nil {
						proxyError = err
					}
					_ = wrappedBody.Close()
				} else if resp.Body != nil {
					_ = resp.Body.Close()
				}
			}
		}
	}

	finished := time.Now()
	reqSnapshot := reqCapture.Snapshot()
	resSnapshot := resCapture.Snapshot()
	if errors.Is(proxyError, context.Canceled) {
		proxyError = nil
	}
	errorMessage := ""
	if proxyError != nil {
		errorMessage = proxyError.Error()
	}
	requestHeaders := ""
	if rule.CaptureRequestHeaders {
		requestHeaders = HeadersToJSON(r.Header, r.Host, rule.RedactSensitiveHeaders)
	}

	h.insertLog(r.Context(), db.HTTPLog{
		RuleID:                    &rule.ID,
		RequestID:                 requestID,
		StartedAt:                 started.UTC().Format(time.RFC3339Nano),
		FinishedAt:                finished.UTC().Format(time.RFC3339Nano),
		DurationMS:                finished.Sub(started).Milliseconds(),
		Method:                    r.Method,
		OriginalURL:               originalURL,
		ProxiedURL:                target.String(),
		Scheme:                    requestScheme(r),
		Host:                      r.Host,
		Path:                      r.URL.Path,
		Query:                     r.URL.RawQuery,
		RequestHeaders:            requestHeaders,
		RequestContentType:        requestContentType,
		RequestBody:               reqSnapshot.Body,
		RequestBodyTruncated:      reqSnapshot.Truncated,
		RequestBodySize:           reqSnapshot.Size,
		RequestBodyOmittedReason:  reqSnapshot.OmittedReason,
		ResponseStatus:            responseStatus,
		ResponseHeaders:           responseHeaders,
		ResponseBody:              resSnapshot.Body,
		ResponseBodyTruncated:     resSnapshot.Truncated,
		ResponseBodySize:          resSnapshot.Size,
		ResponseBodyOmittedReason: resSnapshot.OmittedReason,
		ContentType:               responseContentType,
		IsJSON:                    resClass.IsJSON,
		IsText:                    resClass.IsText,
		IsBinary:                  resClass.IsBinary,
		IsStream:                  resClass.IsStream,
		IsWebSocket:               isWebSocket || responseStatus == http.StatusSwitchingProtocols,
		ErrorMessage:              errorMessage,
		ClientIP:                  clientIP,
	})
}

func prepareRequestBodyForForward(r *http.Request, capture *BodyCapture, store bool, maxBodySize int64, class Classification) (io.ReadCloser, int64, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return http.NoBody, 0, nil
	}
	if shouldPreBufferRequestBody(r, store, maxBodySize, class) {
		data, err := io.ReadAll(r.Body)
		if closeErr := r.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, 0, err
		}
		capture.WriteObserved(data)
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
	}
	return WrapReadCloser(r.Body, capture), r.ContentLength, nil
}

func shouldPreBufferRequestBody(r *http.Request, store bool, maxBodySize int64, class Classification) bool {
	if !store || class.IsStream {
		return false
	}
	if r.ContentLength < 0 {
		return false
	}
	return maxBodySize == 0 || r.ContentLength <= maxBodySize
}

func copyAndFlush(w http.ResponseWriter, src io.Reader) (int64, error) {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	var written int64
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := w.Write(buf[:nr])
			written += int64(nw)
			if canFlush {
				flusher.Flush()
			}
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}

func responseAllowsBody(method string, status int) bool {
	if method == http.MethodHead {
		return false
	}
	if status >= 100 && status < 200 {
		return false
	}
	return status != http.StatusNoContent && status != http.StatusNotModified
}

func tunnelUpgradedConnection(w http.ResponseWriter, resp *http.Response) error {
	upstream, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return errors.New("upstream upgrade connection is not writable")
	}
	defer upstream.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return errors.New("client connection does not support hijacking")
	}
	client, rw, err := hijacker.Hijack()
	if err != nil {
		return err
	}
	defer client.Close()

	body := resp.Body
	resp.Body = nil
	resp.ContentLength = 0
	if err := resp.Write(rw); err != nil {
		resp.Body = body
		return err
	}
	resp.Body = body
	if err := rw.Flush(); err != nil {
		return err
	}

	errc := make(chan error, 2)
	go func() {
		if buffered := rw.Reader.Buffered(); buffered > 0 {
			if _, err := io.CopyN(upstream, rw, int64(buffered)); err != nil {
				errc <- err
				return
			}
		}
		_, err := io.Copy(upstream, client)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(client, upstream)
		errc <- err
	}()
	return <-errc
}

func (h *Handler) insertLog(requestCtx context.Context, logEntry db.HTTPLog) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if requestCtx != nil && requestCtx.Err() == nil {
		ctx = requestCtx
	}
	if err := h.store.InsertLog(ctx, logEntry); err != nil {
		log.Printf("insert log %s: %v", logEntry.RequestID, err)
	}
}

func matchRule(rules []db.ProxyRule, requestPath string) (db.ProxyRule, bool) {
	sort.SliceStable(rules, func(i, j int) bool {
		return len(rules[i].Prefix) > len(rules[j].Prefix)
	})
	for _, rule := range rules {
		if strings.HasPrefix(requestPath, rule.Prefix) {
			return rule, true
		}
		trimmed := strings.TrimSuffix(rule.Prefix, "/")
		if trimmed != "" && requestPath == trimmed {
			return rule, true
		}
	}
	return db.ProxyRule{}, false
}

func buildTargetURL(rule db.ProxyRule, original *url.URL) (*url.URL, error) {
	base, err := url.Parse(rule.TargetBaseURL)
	if err != nil {
		return nil, err
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, errors.New("target_base_url must use http or https")
	}
	remainder := strings.TrimPrefix(original.Path, rule.Prefix)
	if remainder == original.Path {
		remainder = strings.TrimPrefix(original.Path, strings.TrimSuffix(rule.Prefix, "/"))
	}
	target := *base
	target.Path = singleJoiningSlash(base.Path, remainder)
	target.RawQuery = original.RawQuery
	return &target, nil
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func absoluteURL(r *http.Request) string {
	u := *r.URL
	u.Scheme = requestScheme(r)
	u.Host = r.Host
	return u.String()
}

func hasChunkedTransfer(resp *http.Response) bool {
	for _, encoding := range resp.TransferEncoding {
		if strings.EqualFold(encoding, "chunked") {
			return true
		}
	}
	return false
}

func rewriteLocationHeader(resp *http.Response, rule db.ProxyRule, r *http.Request) {
	location := resp.Header.Get("Location")
	if location == "" {
		return
	}
	base, err := url.Parse(rule.TargetBaseURL)
	if err != nil {
		return
	}
	loc, err := url.Parse(location)
	if err != nil || !loc.IsAbs() {
		return
	}
	targetPrefix := strings.TrimRight(base.String(), "/")
	if !strings.HasPrefix(strings.TrimRight(loc.String(), "/"), targetPrefix) {
		return
	}
	localBase := requestScheme(r) + "://" + r.Host + strings.TrimRight(rule.Prefix, "/")
	rewritten := localBase + strings.TrimPrefix(loc.String(), targetPrefix)
	resp.Header.Set("Location", rewritten)
}

func writeLocalCORSHeaders(header http.Header, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Vary", "Origin")
	header.Set("Access-Control-Allow-Credentials", "true")
	header.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS,HEAD")
	if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
		header.Set("Access-Control-Allow-Headers", requested)
	} else {
		header.Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Accept,Origin,X-Requested-With")
	}
}

func newRequestID() string {
	var randomBytes [8]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString(randomBytes[:])
}
