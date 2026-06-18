package providers

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/EngineerProjects/nexus-engine/internal/web/searxng"
)

func searxngProvider(transport roundTripFunc) *SearXNGProvider {
	return &SearXNGProvider{
		client: searxng.NewClientForTest("http://localhost:8080", transport),
	}
}

// TestSearXNGRejectsNonJSONBody verifies that a JSON-decode failure on an HTML
// body produces an error when SEARXNG_HTML_FALLBACK is not enabled.
func TestSearXNGRejectsNonJSONBody(t *testing.T) {
	p := searxngProvider(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("<html>Captcha</html>")),
			Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		}, nil
	})
	_, err := p.Search(SearchInput{Query: "test"})
	if err == nil {
		t.Fatal("expected error for non-JSON response")
	}
	// Error message must tell the operator what to do.
	if !strings.Contains(err.Error(), "non-JSON") && !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("expected non-JSON error, got: %v", err)
	}
}

func TestSearXNGReturns5xxError(t *testing.T) {
	p := searxngProvider(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})
	_, err := p.Search(SearchInput{Query: "test"})
	if err == nil {
		t.Fatal("expected error for 5xx")
	}
}

func TestSearXNGParsesResults(t *testing.T) {
	body := `{"results":[{"title":"A","url":"https://a.com","content":"desc","engine":"google"}]}`
	p := searxngProvider(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": {"application/json"}},
		}, nil
	})
	out, err := p.Search(SearchInput{Query: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	if out.Hits[0].Title != "A" || out.Hits[0].URL != "https://a.com" {
		t.Fatalf("unexpected hit: %+v", out.Hits[0])
	}
}

func TestSearXNGNotConfigured(t *testing.T) {
	p := NewSearXNGProviderWithBaseURL("")
	_, err := p.Search(SearchInput{Query: "test"})
	if err == nil {
		t.Fatal("expected error when not configured")
	}
}
