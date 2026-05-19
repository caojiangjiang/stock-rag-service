package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cloudwego/eino/schema"
)

type FetchWebpageRequest struct {
	URL         string `json:"url"`
	MaxLength   int    `json:"max_length,omitempty"`
	ExtractMode string `json:"extract_mode,omitempty"`
}

type FetchWebpageResponse struct {
	URL         string            `json:"url"`
	Title       string            `json:"title"`
	Content     string            `json:"content"`
	ContentType string            `json:"content_type"`
	Links       []string          `json:"links,omitempty"`
	Images      []string          `json:"images,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	Length      int               `json:"length"`
	FetchTime   string            `json:"fetch_time"`
}

type TypedWebpageFetcher struct {
	*BaseTypedTool
	client *http.Client
}

func NewTypedWebpageFetcher() *TypedWebpageFetcher {
	return &TypedWebpageFetcher{
		BaseTypedTool: &BaseTypedTool{
			name:        "fetch_webpage",
			description: "抓取网页内容，提取纯文本或markdown格式",
		},
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *TypedWebpageFetcher) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url":          {Type: "string", Desc: "网页URL", Required: true},
			"max_length":   {Type: "int", Desc: "最大内容长度", Required: false},
			"extract_mode": {Type: "string", Desc: "提取模式(text/markdown/html)", Required: false},
		}),
	}
}

func (t *TypedWebpageFetcher) Run(ctx context.Context, req *FetchWebpageRequest) (*FetchWebpageResponse, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("URL不能为空")
	}

	if req.MaxLength <= 0 {
		req.MaxLength = 5000
	}
	if req.ExtractMode == "" {
		req.ExtractMode = "text"
	}

	content, err := t.fetchURL(ctx, req.URL)
	if err != nil {
		return nil, fmt.Errorf("抓取网页失败: %w", err)
	}

	title := extractTitle(content)

	if len(content) > req.MaxLength {
		content = content[:req.MaxLength] + "..."
	}

	return &FetchWebpageResponse{
		URL:         req.URL,
		Title:       title,
		Content:     content,
		ContentType: req.ExtractMode,
		Length:      len(content),
		FetchTime:   time.Now().Format(time.RFC3339),
	}, nil
}

func (t *TypedWebpageFetcher) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &FetchWebpageRequest{}

	if urlStr, ok := args["url"].(string); ok {
		req.URL = urlStr
	}
	if maxLength, ok := args["max_length"].(int); ok {
		req.MaxLength = maxLength
	}
	if extractMode, ok := args["extract_mode"].(string); ok {
		req.ExtractMode = extractMode
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedWebpageFetcher) fetchURL(ctx context.Context, urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("无效的URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("不支持的协议: %s", parsedURL.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP请求失败: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "gb2312") || strings.Contains(contentType, "gbk") {
		body, _ = decodeGBK(body)
	}

	return extractTextFromHTML(string(body)), nil
}

func extractTitle(html string) string {
	re := regexp.MustCompile(`<title[^>]*>([^<]+)</title>`)
	match := re.FindStringSubmatch(html)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractTextFromHTML(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return stripHTML(html)
	}

	doc.Find("script, style, nav, header, footer, aside, .advertisement, .ad").Each(func(i int, s *goquery.Selection) {
		s.Remove()
	})

	content := doc.Find("article").First()
	if content.Length() == 0 {
		content = doc.Find("main").First()
	}
	if content.Length() == 0 {
		content = doc.Find(".content, .article, .post, .entry, #content").First()
	}
	if content.Length() == 0 {
		content = doc.Find("body")
	}

	text := content.Text()

	lines := strings.Split(text, "\n")
	var cleanedLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 0 {
			cleanedLines = append(cleanedLines, line)
		}
	}

	return strings.Join(cleanedLines, "\n")
}

func stripHTML(html string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	text := re.ReplaceAllString(html, " ")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	return strings.Join(strings.Fields(text), " ")
}

func decodeGBK(data []byte) ([]byte, error) {
	reader := bytes.NewReader(data)
	var result bytes.Buffer
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n == 0 {
			break
		}
		result.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return result.Bytes(), nil
}

type WebpageFetcher struct {
	client *http.Client
}

func NewWebpageFetcher() *WebpageFetcher {
	return &WebpageFetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *WebpageFetcher) Name() string { return "fetch_webpage" }
func (t *WebpageFetcher) Description() string {
	return "抓取网页内容，提取纯文本或markdown格式"
}

func (t *WebpageFetcher) Schema() string {
	return `fetch_webpage(url, max_length, extract_mode)
  - url: 网页URL（必填）
  - max_length: 最大内容长度（可选，默认5000）
  - extract_mode: 提取模式(text/markdown/html，可选）
  示例：{"tool_name":"fetch_webpage","args":{"url":"https://example.com/news"}}`
}

func (t *WebpageFetcher) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	urlStr, ok := args["url"].(string)
	if !ok || urlStr == "" {
		return "", fmt.Errorf("URL不能为空")
	}

	maxLength := 5000
	if ml, ok := args["max_length"].(int); ok {
		maxLength = ml
	}

	content, err := t.fetchURL(ctx, urlStr)
	if err != nil {
		return "", err
	}

	if len(content) > maxLength {
		content = content[:maxLength] + "..."
	}

	result := map[string]interface{}{
		"url":     urlStr,
		"content": content,
		"length":  len(content),
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}

func (t *WebpageFetcher) fetchURL(ctx context.Context, urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("无效的URL: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("不支持的协议: %s", parsedURL.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP请求失败: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "gb2312") || strings.Contains(contentType, "gbk") {
		body, _ = decodeGBK(body)
	}

	return extractTextFromHTML(string(body)), nil
}
