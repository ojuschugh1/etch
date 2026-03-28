package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/ojuschugh1/etch/internal/ca"
)

// ProxyMode indicates whether the proxy is recording or testing.
type ProxyMode int

const (
	// ModeRecord captures baseline snapshots.
	ModeRecord ProxyMode = iota
	// ModeTest compares live responses against stored snapshots.
	ModeTest
)

// String returns the human-readable name of the mode.
func (m ProxyMode) String() string {
	switch m {
	case ModeRecord:
		return "record"
	case ModeTest:
		return "test"
	default:
		return "unknown"
	}
}

// RequestHandler processes intercepted request/response pairs.
type RequestHandler interface {
	HandleRequest(req *http.Request, resp *http.Response) error
}

// ProxyServer is an HTTP/HTTPS forward proxy.
type ProxyServer struct {
	Addr           string
	Mode           ProxyMode
	CAManager      *ca.CAManager
	Handler        RequestHandler
	InjectHeaders  map[string]string // headers to inject into outgoing requests
	MaxBodySize    int64             // max response body size in bytes (0 = unlimited)
	server         *http.Server
}

// NewProxyServer creates a ProxyServer ready to be started.
func NewProxyServer(addr string, mode ProxyMode, cam *ca.CAManager, handler RequestHandler) *ProxyServer {
	return &ProxyServer{
		Addr:      addr,
		Mode:      mode,
		CAManager: cam,
		Handler:   handler,
	}
}

// Start listens for proxy connections. Blocks until context is cancelled.
func (p *ProxyServer) Start(ctx context.Context) error {
	p.server = &http.Server{
		Addr:    p.Addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				p.handleConnect(w, r)
				return
			}
			p.handleHTTP(w, r)
		}),
	}

	ln, err := net.Listen("tcp", p.Addr)
	if err != nil {
		return fmt.Errorf("proxy: listen on %s: %w", p.Addr, err)
	}

	fmt.Printf("etch proxy listening on %s (mode: %s)\n", p.Addr, p.Mode)

	go func() {
		<-ctx.Done()
		_ = p.Shutdown(context.Background())
	}()

	if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("proxy: serve: %w", err)
	}
	return nil
}

// Shutdown drains in-flight requests with a 30s timeout.
func (p *ProxyServer) Shutdown(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return p.server.Shutdown(shutdownCtx)
}

// handleHTTP forwards a plain HTTP request upstream and writes the response back.
func (p *ProxyServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// buffer request body so we can forward it and still pass it to the handler
	var reqBody []byte
	if r.Body != nil {
		reqBody, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	// Build the outgoing request.
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "proxy: bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	copyHeaders(outReq.Header, r.Header)
	// strip hop-by-hop headers
	outReq.Header.Del("Proxy-Connection")
	outReq.Header.Del("Proxy-Authenticate")
	outReq.Header.Del("Proxy-Authorization")

	// inject any configured headers (e.g. fresh auth tokens for CI)
	for k, v := range p.InjectHeaders {
		outReq.Header.Set(k, os.ExpandEnv(v))
	}

	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, "proxy: upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "proxy: read upstream body: "+err.Error(), http.StatusBadGateway)
		return
	}

	// decompress so snapshots contain readable text
	plainBody := decompressBody(body, resp.Header.Get("Content-Encoding"))
	if plainBody != nil {
		resp.Header.Del("Content-Encoding")
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(plainBody)))
	} else {
		plainBody = body
	}

	// truncate if over the size limit
	if p.MaxBodySize > 0 && int64(len(plainBody)) > p.MaxBodySize {
		log.Printf("proxy: response body truncated from %d to %d bytes for %s", len(plainBody), p.MaxBodySize, r.URL)
		plainBody = plainBody[:p.MaxBodySize]
	}

	if p.Handler != nil {
		// restore request body for the handler
		r.Body = io.NopCloser(bytes.NewReader(reqBody))
		handlerResp := *resp
		handlerResp.Body = io.NopCloser(strings.NewReader(string(plainBody)))
		if err := p.Handler.HandleRequest(r, &handlerResp); err != nil {
			log.Printf("proxy: handler error: %v", err)
		}
	}

	// Write the response back to the client.
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(plainBody)
}

// handleConnect handles HTTPS via CONNECT tunneling.
func (p *ProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !strings.Contains(host, ":") {
		host += ":443"
	}
	hostname := strings.Split(host, ":")[0]

	// Hijack the connection before writing any response.
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		log.Printf("proxy: hijack error: %v", err)
		return
	}
	defer clientConn.Close()

	_, _ = clientBuf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = clientBuf.Flush()

	tlsCert, err := p.CAManager.GenerateServerCert(hostname)
	if err != nil {
		log.Printf("proxy: generate cert for %s: %v", hostname, err)
		return
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
	}

	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("proxy: TLS handshake with client for %s: %v", hostname, err)
		return
	}
	defer tlsConn.Close()

	clientReader := bufio.NewReader(tlsConn)
	req, err := http.ReadRequest(clientReader)
	if err != nil {
		if err != io.EOF {
			log.Printf("proxy: read decrypted request for %s: %v", hostname, err)
		}
		return
	}

	req.URL.Scheme = "https"
	req.URL.Host = host
	req.RequestURI = ""

	p.forwardTLSRequest(tlsConn, req)
}

// forwardTLSRequest sends the decrypted request upstream and writes the response back.
func (p *ProxyServer) forwardTLSRequest(tlsConn net.Conn, req *http.Request) {
	// buffer request body for the handler
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
	}

	outReq, err := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("proxy: build upstream request: %v", err)
		writeHTTPError(tlsConn, http.StatusBadRequest)
		return
	}
	copyHeaders(outReq.Header, req.Header)

	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		log.Printf("proxy: upstream TLS error for %s: %v", req.URL.Host, err)
		writeHTTPError(tlsConn, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("proxy: read upstream TLS body: %v", err)
		writeHTTPError(tlsConn, http.StatusBadGateway)
		return
	}

	// decompress for readable snapshots
	plainBody := decompressBody(body, resp.Header.Get("Content-Encoding"))
	if plainBody != nil {
		resp.Header.Del("Content-Encoding")
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(plainBody)))
	} else {
		plainBody = body
	}

	if p.MaxBodySize > 0 && int64(len(plainBody)) > p.MaxBodySize {
		log.Printf("proxy: TLS response body truncated from %d to %d bytes for %s", len(plainBody), p.MaxBodySize, req.URL)
		plainBody = plainBody[:p.MaxBodySize]
	}

	if p.Handler != nil {
		// restore request body for the handler
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
		handlerResp := *resp
		handlerResp.Body = io.NopCloser(strings.NewReader(string(plainBody)))
		if err := p.Handler.HandleRequest(req, &handlerResp); err != nil {
			log.Printf("proxy: handler error: %v", err)
		}
	}

	resp.Body = io.NopCloser(strings.NewReader(string(plainBody)))
	raw, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Printf("proxy: dump response: %v", err)
		return
	}
	_, _ = tlsConn.Write(raw)
}

// decompressBody decompresses gzip or deflate bodies. Returns nil if not applicable.
func decompressBody(body []byte, encoding string) []byte {
	switch strings.ToLower(encoding) {
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil
		}
		decompressed, err := io.ReadAll(gr)
		gr.Close()
		if err != nil {
			return nil
		}
		return decompressed
	case "deflate":
		fr := flate.NewReader(bytes.NewReader(body))
		decompressed, err := io.ReadAll(fr)
		fr.Close()
		if err != nil {
			return nil
		}
		return decompressed
	default:
		return nil
	}
}

// writeHTTPError writes a minimal HTTP error response to a raw connection.
func writeHTTPError(conn net.Conn, statusCode int) {
	statusText := http.StatusText(statusCode)
	resp := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Length: %d\r\n\r\n%s",
		statusCode, statusText, len(statusText), statusText)
	_, _ = conn.Write([]byte(resp))
}

// copyHeaders copies all headers from src to dst.
func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
