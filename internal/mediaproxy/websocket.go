package mediaproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (e *Executor) serveWebSocket(w http.ResponseWriter, r *http.Request, target Target) error {
	upstreamURL, err := target.URLForRequest(r.URL)
	if err != nil {
		return err
	}
	conn, throughHTTPProxy, err := e.dialTarget(r.Context(), target)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	requestHost := upstreamURL.Host
	if e.cfg.PreserveHost {
		requestHost = r.Host
	}
	request := &http.Request{Method: http.MethodGet, URL: upstreamURL, Host: requestHost, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: websocketHeaders(r.Header, target, e.cfg.PreserveHost)}
	writeRequest := request.Write
	if throughHTTPProxy {
		writeRequest = request.WriteProxy
	}
	if err := writeRequest(conn); err != nil {
		_ = conn.Close()
		return err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = conn.Close()
		return err
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		if isWebSocketClientRejection(response.StatusCode) {
			defer conn.Close()
			writeWebSocketClientRejection(w, response)
			return nil
		}
		_ = response.Body.Close()
		_ = conn.Close()
		return fmt.Errorf("upstream websocket status %d", response.StatusCode)
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = response.Body.Close()
		_ = conn.Close()
		return fmt.Errorf("websocket hijack unavailable")
	}
	client, clientReader, err := hijacker.Hijack()
	if err != nil {
		_ = conn.Close()
		return err
	}
	if err := response.Write(client); err != nil {
		_ = client.Close()
		_ = conn.Close()
		return err
	}
	flushBuffered(client, reader)
	flushBuffered(conn, clientReader.Reader)
	_ = conn.SetDeadline(time.Time{})
	go copyClose(conn, client)
	go copyClose(client, conn)
	return nil
}

func isWebSocketClientRejection(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	default:
		return false
	}
}

func writeWebSocketClientRejection(w http.ResponseWriter, response *http.Response) {
	defer response.Body.Close()
	for _, name := range []string{"Cache-Control", "Content-Language", "Content-Type", "Expires", "Pragma", "Retry-After", "Vary", "WWW-Authenticate"} {
		for _, value := range response.Header.Values(name) {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
}

func (e *Executor) dialTarget(ctx context.Context, target Target) (net.Conn, bool, error) {
	proxyURL, err := e.proxyURL(target)
	if err != nil {
		return nil, false, err
	}
	if proxyURL != nil {
		conn, err := e.dialProxy(ctx, proxyURL)
		if err != nil {
			return nil, false, err
		}
		if target.Scheme == "https" {
			if err := sendConnectRequest(conn, target); err != nil {
				_ = conn.Close()
				return nil, false, err
			}
			conn, err = e.wrapTLS(ctx, conn, target)
			if err != nil {
				return nil, false, err
			}
			return conn, false, nil
		}
		return conn, true, nil
	}
	resolved, ok := resolvedTargetFromContext(ctx)
	if !ok {
		return nil, false, ErrUpstreamUnavailable
	}
	conn, err := e.transport.DialContext(ctx, "tcp", net.JoinHostPort(resolved.host, strconv.Itoa(resolved.port)))
	if err != nil {
		return nil, false, err
	}
	if target.Scheme == "https" {
		conn, err = e.wrapTLS(ctx, conn, target)
		if err != nil {
			return nil, false, err
		}
		return conn, false, nil
	}
	return conn, false, nil
}

func (e *Executor) proxyURL(target Target) (*url.URL, error) {
	if e == nil || e.transport == nil || e.transport.Proxy == nil {
		return nil, nil
	}
	requestURL := &url.URL{Scheme: target.Scheme, Host: net.JoinHostPort(target.Host, strconv.Itoa(target.Port))}
	return e.transport.Proxy(&http.Request{URL: requestURL})
}

func (e *Executor) dialProxy(ctx context.Context, proxyURL *url.URL) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(proxyURL.Hostname(), proxyPort(proxyURL)))
}

func proxyPort(proxyURL *url.URL) string {
	if port := proxyURL.Port(); port != "" {
		return port
	}
	if strings.EqualFold(proxyURL.Scheme, "https") {
		return "443"
	}
	return "80"
}

func sendConnectRequest(conn net.Conn, target Target) error {
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: net.JoinHostPort(target.Host, strconv.Itoa(target.Port))}, Host: net.JoinHostPort(target.Host, strconv.Itoa(target.Port)), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
	if err := connect.Write(conn); err != nil {
		return err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), connect)
	if err != nil {
		return err
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("proxy connect failed")
	}
	return nil
}

func (e *Executor) wrapTLS(ctx context.Context, conn net.Conn, target Target) (net.Conn, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.Host}
	if e != nil && e.cfg.TLSConfig != nil {
		tlsConfig = e.cfg.TLSConfig.Clone()
		tlsConfig.ServerName = target.Host
	}
	tlsConn := tls.Client(conn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func flushBuffered(conn net.Conn, reader *bufio.Reader) {
	if reader == nil || reader.Buffered() == 0 {
		return
	}
	_, _ = io.CopyN(conn, reader, int64(reader.Buffered()))
}

func copyClose(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}
