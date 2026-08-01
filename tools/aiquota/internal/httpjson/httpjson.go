// Package httpjson is a tiny read-only JSON-over-HTTP client shared by the
// network providers, mirroring QuotaList's JSONHTTPClient.swift: centralize
// timeout, status handling and JSON parsing, leave each provider to only map
// the result onto a quota.Snapshot.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
)

// Kind classifies why a fetch did not yield JSON, so providers can map it onto
// user-facing text the way ClaudeProvider/ZaiProvider do (e.g. 401 → "token
// invalid").
type Kind int

const (
	KindSuccess   Kind = iota
	KindStatus         // non-2xx response
	KindNotJSON        // body wasn't valid JSON
	KindTransport      // request failed / no HTTP response
)

// Result is the outcome of one request.
type Result struct {
	Kind       Kind
	StatusCode int
	Body       any // decoded JSON on KindSuccess
	RetryAfter *time.Time
	Err        error
}

// Client performs GET/POST requests and decodes JSON bodies.
type Client struct {
	HTTP    *http.Client
	Timeout time.Duration
}

// New builds a client using `proxy` when `useProxy` is set and the proxy is
// fully configured, otherwise the default transport.
func New(proxy config.Proxy, useProxy bool, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	hc := &http.Client{Timeout: timeout}
	if useProxy && proxy.Usable() {
		if transport, err := proxyTransport(proxy); err == nil {
			hc.Transport = transport
		}
	}
	return &Client{HTTP: hc, Timeout: timeout}
}

func proxyTransport(p config.Proxy) (*http.Transport, error) {
	scheme := "http"
	if p.Type == config.ProxySOCKS5 {
		scheme = "socks5"
	}
	u, err := url.Parse(fmt.Sprintf("%s://%s:%d", scheme, p.Host, p.Port))
	if err != nil {
		return nil, err
	}
	return &http.Transport{Proxy: http.ProxyURL(u)}, nil
}

// Fetch issues one request and decodes a JSON body.
func (c *Client) Fetch(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) Result {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return Result{Kind: KindTransport, Err: err}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{Kind: KindTransport, Err: err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{Kind: KindTransport, Err: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		res := Result{Kind: KindStatus, StatusCode: resp.StatusCode}
		if resp.StatusCode == http.StatusTooManyRequests {
			res.RetryAfter = retryAfter(resp.Header.Get("Retry-After"))
		}
		return res
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return Result{Kind: KindNotJSON, StatusCode: resp.StatusCode}
	}
	return Result{Kind: KindSuccess, StatusCode: resp.StatusCode, Body: decoded}
}

func retryAfter(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs >= 0 {
		t := time.Now().Add(time.Duration(secs) * time.Second)
		return &t
	}
	if t, err := http.ParseTime(raw); err == nil {
		return &t
	}
	return nil
}
