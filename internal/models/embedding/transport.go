package embedding

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// sharedEmbeddingHTTPTransport keeps a single SSRF-safe connection pool for
// all embedding clients. Embedders are recreated as model configuration changes,
// but their outbound connections can be safely reused across client instances,
// so the transport (and its keep-alive pool) is built once at package load.
var sharedEmbeddingHTTPTransport = newSharedEmbeddingHTTPTransport()

func newSharedEmbeddingHTTPTransport() *http.Transport {
	transport := secutils.NewSSRFSafeTransport(secutils.DefaultSSRFSafeHTTPClientConfig())
	transport.DialContext = embeddingDialContext
	return transport
}

// validateEmbeddingBaseURL checks that a resolved embedding API base URL is safe
// for outbound requests. Empty URLs are allowed (callers apply provider defaults).
func validateEmbeddingBaseURL(baseURL string) error {
	if baseURL == "" {
		return nil
	}
	if err := secutils.ValidateURLForSSRF(baseURL); err != nil {
		// Transparent proxy fake-IP mode resolves public domains into RFC 2544.
		// The configured hostname remains the connection target, so this exact
		// case does not require a per-domain whitelist. Direct IP URLs and all
		// other restricted ranges continue through the central SSRF rejection.
		if shouldAllowEmbeddingProxyFakeIP(baseURL, err) {
			return nil
		}
		return fmt.Errorf("base URL SSRF check failed: %w", err)
	}
	return nil
}

func shouldAllowEmbeddingProxyFakeIP(baseURL string, err error) bool {
	if err == nil || !strings.Contains(err.Error(), "restricted range 198.18.0.0/15") {
		return false
	}
	parsed, parseErr := url.Parse(baseURL)
	if parseErr != nil || parsed.Hostname() == "" {
		return false
	}
	return net.ParseIP(parsed.Hostname()) == nil
}

var proxyFakeIPNet = &net.IPNet{
	IP:   net.ParseIP("198.18.0.0").To4(),
	Mask: net.CIDRMask(15, 32),
}

func embeddingDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) != nil {
		return secutils.SSRFSafeDialContext(ctx, network, addr)
	}
	ips, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if lookupErr == nil && len(ips) > 0 {
		allProxyFakeIPs := true
		for _, ipAddr := range ips {
			ip := ipAddr.IP.To4()
			if ip == nil || !proxyFakeIPNet.Contains(ip) {
				allProxyFakeIPs = false
				break
			}
		}
		if allProxyFakeIPs {
			dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			return dialer.DialContext(ctx, network, addr)
		}
	}
	return secutils.SSRFSafeDialContext(ctx, network, addr)
}

// newEmbeddingHTTPClient returns an HTTP client with connection-level SSRF
// protection and redirect validation, aligned with internal/models/chat/transport.go.
// All clients share sharedEmbeddingHTTPTransport so keep-alive connections are
// pooled globally, while each keeps its own timeout.
func newEmbeddingHTTPClient(timeout time.Duration) *http.Client {
	cfg := secutils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	return secutils.NewSSRFSafeHTTPClientWithTransport(cfg, sharedEmbeddingHTTPTransport)
}
