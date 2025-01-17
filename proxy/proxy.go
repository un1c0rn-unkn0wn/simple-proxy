package proxy

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
	//"strings"
)

func NewProxyHandler(timeoutSeconds int) *ProxyHandler {
	return &ProxyHandler{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	}
}

type ProxyHandler struct {
	Timeout    time.Duration
	Username   *string
	Password   *string
	LogAuth    bool
	LogHeaders bool
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(os.Stdout, "[INFO]: Serving '%s' request from '%s' to '%s'\n", r.Method, r.RemoteAddr, r.Host)
	if p.LogHeaders {
		for name, values := range r.Header {
			for i, value := range values {
				fmt.Fprintf(os.Stdout, "'%s': [%d] %s", name, i, value)
			}
		}
	}
	if p.Username != nil && p.Password != nil {
		username, password, ok := proxyBasicAuth(r)
		if !ok || username != *p.Username || password != *p.Password {
			//Unauthorized message
			fmt.Fprintf(os.Stdout, "[AUTH]: Unauthorized request from %s\n", r.RemoteAddr)
			fmt.Fprintf(os.Stderr, "[AUTH]: Unauthorized request from %s\n", r.RemoteAddr)
			w.Header().Set("Proxy-Authenticate", "Basic")
			http.Error(w, "Unauthorized", http.StatusProxyAuthRequired)
			return
		}
	}
	if r.Method == http.MethodConnect {
		handleTunneling(w, r, p.Timeout)
	} else {
		handleHTTP(w, r)
	}
}

func handleTunneling(w http.ResponseWriter, r *http.Request, timeout time.Duration) {
	dest_conn, err := net.DialTimeout("tcp", r.Host, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR]: Failed to dial host, %s\n", err.Error())
		fmt.Fprintf(os.Stdout, "[ERROR]: Failed to dial host, %s\n", err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		fmt.Fprintln(os.Stderr, "[ERROR]: Attempted to hijack connection that does not support it")
		fmt.Fprintln(os.Stdout, "[ERROR]: Attempted to hijack connection that does not support it")
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	client_conn, _, err := hijacker.Hijack()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR]: Failed to hijack connection, %s\n", err.Error())
		fmt.Fprintf(os.Stdout, "[ERROR]: Failed to hijack connection, %s\n", err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	go transfer(dest_conn, client_conn)
	go transfer(client_conn, dest_conn)
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	io.Copy(destination, source)
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR]: Failed to proxy request, %s\n", err.Error())
		fmt.Fprintf(os.Stdout, "[ERROR]: Failed to proxy request, %s\n", err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func proxyBasicAuth(r *http.Request) (username, password string, ok bool) {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return
	}
	return parseBasicAuth(auth)
}

// parseBasicAuth parses an HTTP Basic Authentication string.
// "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==" returns ("Aladdin", "open sesame", true).
func parseBasicAuth(auth string) (username, password string, ok bool) {
	const prefix = "Basic "
	// Case insensitive prefix match. See Issue 22736.
	if len(auth) < len(prefix) || !equalFold(auth[:len(prefix)], prefix) {
		return
	}
	c, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return
	}
	cs := string(c)
	//s := strings.IndexByte(cs, ':')
	// No pkg code
	var s = -1
	for i := 0; i < len(cs); i++ {
		if cs[i] == ':' {
			s = i
			break
		}
	}
	// End of no pkg code
	if s < 0 {
		return
	}
	return cs[:s], cs[s+1:], true
}

// EqualFold is strings.EqualFold, ASCII only. It reports whether s and t
// are equal, ASCII-case-insensitively.
func equalFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if lower(s[i]) != lower(t[i]) {
			return false
		}
	}
	return true
}

// lower returns the ASCII lowercase version of b.
func lower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
