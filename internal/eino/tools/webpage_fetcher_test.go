package tools

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTypedWebpageFetcherExtractsTitleAndTruncatesText(t *testing.T) {
	fetcher := NewTypedWebpageFetcher()
	fetcher.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body: io.NopCloser(bytes.NewBufferString(`<!doctype html>
<html>
  <head><title>测试标题</title><style>.hidden{display:none}</style></head>
  <body>
    <nav>导航内容</nav>
    <main>
      <h1>正文标题</h1>
      <p>贵州茅台营收增长，现金流稳定。</p>
    </main>
    <script>console.log("ignore")</script>
  </body>
</html>`)),
			Request: req,
		}, nil
	})

	resp, err := fetcher.Run(context.Background(), &FetchWebpageRequest{
		URL:       "https://example.com/report",
		MaxLength: 8,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.Title != "测试标题" {
		t.Fatalf("expected title, got %q", resp.Title)
	}
	if strings.Contains(resp.Content, "导航内容") || strings.Contains(resp.Content, "console.log") {
		t.Fatalf("content should exclude navigation/script text, got %q", resp.Content)
	}
	if !strings.HasSuffix(resp.Content, "...") {
		t.Fatalf("expected truncated content suffix, got %q", resp.Content)
	}
}

func TestTypedWebpageFetcherBlocksLocalAddresses(t *testing.T) {
	fetcher := NewTypedWebpageFetcher()
	blockedURLs := []string{
		"http://localhost/report",
		"http://127.0.0.1/report",
		"http://10.0.0.1/report",
		"http://[::1]/report",
	}
	for _, target := range blockedURLs {
		if _, err := fetcher.Run(context.Background(), &FetchWebpageRequest{URL: target}); err == nil {
			t.Fatalf("expected %s to be blocked", target)
		}
	}
}

func TestTruncateTextPreservesUTF8(t *testing.T) {
	got := truncateText("贵州茅台营收增长", 4)
	if got != "贵州茅台..." {
		t.Fatalf("unexpected truncate result: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
