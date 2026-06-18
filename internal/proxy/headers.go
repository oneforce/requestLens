package proxy

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	"proxy-authorization": {},
}

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func HeadersToJSON(headers http.Header, host string, redact bool) string {
	out := map[string][]string{}
	if host != "" {
		out["Host"] = []string{host}
	}
	for key, values := range headers {
		copied := append([]string(nil), values...)
		if redact {
			if _, ok := sensitiveHeaders[strings.ToLower(key)]; ok {
				for i := range copied {
					copied[i] = "******"
				}
			}
		}
		out[key] = copied
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func cloneHeader(src http.Header) http.Header {
	dst := make(http.Header, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func copyHeader(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func removeHopByHopHeaders(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				headers.Del(token)
			}
		}
	}
	for _, header := range hopByHopHeaders {
		headers.Del(header)
	}
}

func AppendForwardedHeaders(out *http.Request, in *http.Request, clientIP string) {
	if clientIP != "" {
		if prior := out.Header.Get("X-Forwarded-For"); prior != "" {
			out.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			out.Header.Set("X-Forwarded-For", clientIP)
		}
	}
	if in.Host != "" {
		out.Header.Set("X-Forwarded-Host", in.Host)
	}
	out.Header.Set("X-Forwarded-Proto", requestScheme(in))
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.Split(proto, ",")[0]
	}
	return "http"
}

func IsWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
