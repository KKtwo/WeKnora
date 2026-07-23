package embedding

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewEmbeddingHTTPClient_ReusesTransport(t *testing.T) {
	firstTimeout := 15 * time.Second
	secondTimeout := 45 * time.Second
	first := newEmbeddingHTTPClient(firstTimeout)
	second := newEmbeddingHTTPClient(secondTimeout)

	if first == second {
		t.Fatal("expected distinct HTTP clients")
	}
	if first.Transport != second.Transport {
		t.Fatal("expected embedding HTTP clients to share a transport")
	}
	if first.Transport != sharedEmbeddingHTTPTransport {
		t.Fatal("expected embedding HTTP client to use the shared transport")
	}
	if first.Timeout != firstTimeout {
		t.Fatalf("unexpected first client timeout: got %v, want %v", first.Timeout, firstTimeout)
	}
	if second.Timeout != secondTimeout {
		t.Fatalf("unexpected second client timeout: got %v, want %v", second.Timeout, secondTimeout)
	}
}

func TestValidateEmbeddingBaseURL_RejectsLoopback(t *testing.T) {
	err := validateEmbeddingBaseURL("http://169.254.169.254/latest/meta-data")
	if err == nil {
		t.Fatal("expected SSRF error for link-local metadata URL")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmbeddingBaseURL_AllowsEmpty(t *testing.T) {
	if err := validateEmbeddingBaseURL(""); err != nil {
		t.Fatalf("empty base URL should be allowed: %v", err)
	}
}

func TestEmbeddingProxyFakeIPDoesNotRequireWhitelist(t *testing.T) {
	fakeIPError := fmt.Errorf(
		"SSRF validation failed: hostname ark.cn-beijing.volces.com resolves to restricted IP 198.18.0.68: restricted range 198.18.0.0/15",
	)
	if !shouldAllowEmbeddingProxyFakeIP("https://ark.cn-beijing.volces.com/api/v3", fakeIPError) {
		t.Fatal("expected a hostname resolved through RFC 2544 fake-IP mode to be allowed")
	}
	if shouldAllowEmbeddingProxyFakeIP("https://198.18.0.68/api/v3", fakeIPError) {
		t.Fatal("direct RFC 2544 IP URLs must remain blocked")
	}
	privateError := fmt.Errorf(
		"SSRF validation failed: hostname embedding.internal resolves to restricted IP 10.0.0.2: private IP address",
	)
	if shouldAllowEmbeddingProxyFakeIP("https://embedding.internal/v1", privateError) {
		t.Fatal("private network resolutions must remain blocked")
	}
}

func TestNewOpenAIEmbedder_RejectsPrivateBaseURL(t *testing.T) {
	_, err := NewOpenAIEmbedder(
		"test-key",
		"http://169.254.169.254/latest/meta-data",
		"text-embedding-3-small",
		511,
		256,
		"model-id",
		nil,
	)
	if err == nil {
		t.Fatal("expected SSRF rejection for link-local metadata URL")
	}
}
