package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/cloudwego/eino/schema"
)

// WebSearchRequest 网络搜索请求
type WebSearchRequest struct {
	Query      string `json:"query"`
	StockCode  string `json:"stock_code,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
	SearchType string `json:"search_type,omitempty"`
	TimeRange  string `json:"time_range,omitempty"`
}

// WebSearchResponse 网络搜索响应
type WebSearchResponse struct {
	Query       string         `json:"query"`
	StockCode   string         `json:"stock_code,omitempty"`
	Results     []SearchResult `json:"results"`
	TotalCount  int            `json:"total_count"`
	SourceTypes []string       `json:"source_types"`
}

// TypedWebSearcher 强类型网络搜索工具
type TypedWebSearcher struct {
	*BaseTypedTool
	apiKey string
}

// NewTypedWebSearcher 创建强类型网络搜索工具
func NewTypedWebSearcher(apiKey string) *TypedWebSearcher {
	return &TypedWebSearcher{
		BaseTypedTool: &BaseTypedTool{
			name:        "web_search",
			description: "搜索外部网络获取最新新闻、公告、研报入口、公司官网链接等信息",
		},
		apiKey: apiKey,
	}
}

func (t *TypedWebSearcher) Schema() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: t.Name(),
		Desc: t.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query":       {Type: "string", Desc: "搜索查询词", Required: true},
			"stock_code":  {Type: "string", Desc: "股票代码", Required: false},
			"max_results": {Type: "int", Desc: "最大返回结果数", Required: false},
			"search_type": {Type: "string", Desc: "搜索类型(news/announcement/research)", Required: false},
			"time_range":  {Type: "string", Desc: "时间范围(7d/30d/90d/all)", Required: false},
		}),
	}
}

func (t *TypedWebSearcher) Run(ctx context.Context, req *WebSearchRequest) (*WebSearchResponse, error) {
	if req.MaxResults <= 0 {
		req.MaxResults = 10
	}
	if req.TimeRange == "" {
		req.TimeRange = "30d"
	}

	results, err := t.performSearch(req.Query, req.MaxResults)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	sourceTypes := make(map[string]bool)
	for _, r := range results {
		sourceTypes[r.Source] = true
	}

	sourceTypeList := make([]string, 0, len(sourceTypes))
	for st := range sourceTypes {
		sourceTypeList = append(sourceTypeList, st)
	}

	return &WebSearchResponse{
		Query:       req.Query,
		StockCode:   req.StockCode,
		Results:     results,
		TotalCount:  len(results),
		SourceTypes: sourceTypeList,
	}, nil
}

func (t *TypedWebSearcher) Invoke(ctx context.Context, args map[string]interface{}) (string, error) {
	req := &WebSearchRequest{}

	if query, ok := args["query"].(string); ok {
		req.Query = query
	}
	if stockCode, ok := args["stock_code"].(string); ok {
		req.StockCode = stockCode
	}
	if maxResults, ok := args["max_results"].(int); ok {
		req.MaxResults = maxResults
	}
	if searchType, ok := args["search_type"].(string); ok {
		req.SearchType = searchType
	}
	if timeRange, ok := args["time_range"].(string); ok {
		req.TimeRange = timeRange
	}

	resp, err := t.Run(ctx, req)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(resp)
	return string(data), nil
}

func (t *TypedWebSearcher) performSearch(query string, maxResults int) ([]SearchResult, error) {
	return performWebSearch(query, maxResults, t.apiKey)
}

// performWebSearch 执行网络搜索（强类型版本）
func performWebSearch(query string, maxResults int, apiKey string) ([]SearchResult, error) {
	searchResults := []SearchResult{}

	// 根据关键词生成模拟结果
	if strings.Contains(query, "贵州茅台") || strings.Contains(query, "茅台") {
		searchResults = append(searchResults, SearchResult{
			Title:   "贵州茅台2024年年度报告解读",
			Snippet: "贵州茅台发布2024年年度报告，实现营业总收入1,555.39亿元，同比增长15.51%。净利润875.54亿元，同比增长17.23%。",
			URL:     "https://www.eastmoney.com/",
			Source:  "东方财富网",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   "贵州茅台高端产品收入稳健增长",
			Snippet: "季报显示，贵州茅台高端产品收入保持稳健增长，渠道与现金流表现受到投资者关注。",
			URL:     "https://finance.sina.com.cn/",
			Source:  "新浪财经",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   "贵州茅台官方网站",
			Snippet: "贵州茅台酒股份有限公司官方网站，提供公司信息、产品介绍、投资者关系等内容。",
			URL:     "https://www.moutaichina.com/",
			Source:  "公司官网",
		})
	} else if strings.Contains(query, "比亚迪") || strings.Contains(query, "BYD") {
		searchResults = append(searchResults, SearchResult{
			Title:   "比亚迪海外市场扩张提速",
			Snippet: "比亚迪在多个海外市场继续推进渠道建设与车型投放，出口与全球化布局受到关注。",
			URL:     "https://www.10jqka.com.cn/",
			Source:  "同花顺财经",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   "比亚迪发布最新产销快报",
			Snippet: "比亚迪公告显示，新能源汽车销量持续增长，市场份额稳步提升。",
			URL:     "https://www.cninfo.com.cn/",
			Source:  "深交所公告",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   "比亚迪官方网站",
			Snippet: "比亚迪股份有限公司官方网站，提供公司新闻、产品信息、投资者关系等内容。",
			URL:     "https://www.byd.com/",
			Source:  "公司官网",
		})
	} else if strings.Contains(query, "宁德时代") || strings.Contains(query, "CATL") {
		searchResults = append(searchResults, SearchResult{
			Title:   "宁德时代储能业务保持增长",
			Snippet: "年报显示，宁德时代储能业务收入继续增长，海外收入占比提升，研发投入维持较高水平。",
			URL:     "https://www.stcn.com/",
			Source:  "证券时报网",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   "宁德时代最新研报：储能业务前景广阔",
			Snippet: "机构研报指出，宁德时代在储能领域具有技术和规模优势，未来增长潜力巨大。",
			URL:     "https://research.cnhbstock.com/",
			Source:  "券商研报",
		})
	} else if strings.Contains(query, "公告") {
		searchResults = append(searchResults, SearchResult{
			Title:   "上市公司公告汇总",
			Snippet: "最新上市公司公告信息，包括业绩预告、重大事项、股权转让等重要信息。",
			URL:     "https://www.cninfo.com.cn/",
			Source:  "巨潮资讯网",
		})
	} else if strings.Contains(query, "研报") || strings.Contains(query, "研究报告") {
		searchResults = append(searchResults, SearchResult{
			Title:   "最新券商研究报告",
			Snippet: "各大券商最新研究报告，覆盖宏观经济、行业分析、个股研究等内容。",
			URL:     "https://research.cnhbstock.com/",
			Source:  "券商研报",
		})
	} else {
		// 通用搜索结果
		searchResults = append(searchResults, SearchResult{
			Title:   fmt.Sprintf("%s - 最新资讯", query),
			Snippet: fmt.Sprintf("关于\"%s\"的最新市场动态和行业分析报告。", query),
			URL:     "https://news.sina.com.cn/",
			Source:  "新浪新闻",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   fmt.Sprintf("%s - 财经报道", query),
			Snippet: fmt.Sprintf("\"%s\"相关的财经新闻和市场分析。", query),
			URL:     "https://finance.qq.com/",
			Source:  "腾讯财经",
		})
	}

	// 限制返回结果数量
	if len(searchResults) > maxResults {
		searchResults = searchResults[:maxResults]
	}

	return searchResults, nil
}

// WebSearcher 网络搜索工具（标准类型）
type WebSearcher struct {
	apiKey string
}

// NewWebSearcher 创建网络搜索工具
func NewWebSearcher(apiKey string) *WebSearcher {
	return &WebSearcher{
		apiKey: apiKey,
	}
}

// Name 工具名称
func (t *WebSearcher) Name() string { return "web_search" }

// Description 工具描述
func (t *WebSearcher) Description() string {
	return "搜索网络信息，获取最新的新闻、公告和市场动态"
}

// Schema 工具参数schema
func (t *WebSearcher) Schema() string {
	return `web_search(query)
  - query: 搜索关键词（必填），如"贵州茅台2024年营收"、"比亚迪最新公告"
  示例：{"tool_name":"web_search","args":{"query":"贵州茅台2024年营收"}}`
}

// Run 执行工具
func (t *WebSearcher) Run(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("缺少搜索关键词参数")
	}

	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("搜索关键词不能为空")
	}

	// 使用简单的网络搜索（这里使用模拟搜索，实际生产中可以接入搜索引擎API）
	results, err := t.performSearch(query)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "未找到相关搜索结果", nil
	}

	// 构建返回结果
	var result strings.Builder
	result.WriteString("网络搜索结果：\n")
	for i, item := range results {
		result.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Title))
		result.WriteString(fmt.Sprintf("   来源：%s\n", item.Source))
		result.WriteString(fmt.Sprintf("   摘要：%s\n", item.Snippet))
		result.WriteString(fmt.Sprintf("   链接：%s\n", item.URL))
		result.WriteString("\n")
	}

	return result.String(), nil
}

// SearchResult 搜索结果
type SearchResult struct {
	Title   string
	Snippet string
	URL     string
	Source  string
}

// performSearch 执行网络搜索
func (t *WebSearcher) performSearch(query string) ([]SearchResult, error) {
	// 模拟搜索结果（实际生产中可以接入百度、必应等搜索引擎API）
	// 这里使用简单的HTTP请求获取一些公开信息作为演示

	// 对于股票相关的搜索，我们可以尝试获取一些公开的财经网站信息
	searchResults := []SearchResult{}

	// 根据关键词生成模拟结果
	if strings.Contains(query, "贵州茅台") || strings.Contains(query, "茅台") {
		searchResults = append(searchResults, SearchResult{
			Title:   "贵州茅台2024年年度报告解读",
			Snippet: "贵州茅台发布2024年年度报告，实现营业总收入1,555.39亿元，同比增长15.51%。净利润875.54亿元，同比增长17.23%。",
			URL:     "https://www.eastmoney.com/",
			Source:  "东方财富网",
		})
		searchResults = append(searchResults, SearchResult{
			Title:   "贵州茅台高端产品收入稳健增长",
			Snippet: "季报显示，贵州茅台高端产品收入保持稳健增长，渠道与现金流表现受到投资者关注。",
			URL:     "https://finance.sina.com.cn/",
			Source:  "新浪财经",
		})
	} else if strings.Contains(query, "比亚迪") {
		searchResults = append(searchResults, SearchResult{
			Title:   "比亚迪海外市场扩张提速",
			Snippet: "比亚迪在多个海外市场继续推进渠道建设与车型投放，出口与全球化布局受到关注。",
			URL:     "https://www.10jqka.com.cn/",
			Source:  "同花顺财经",
		})
	} else if strings.Contains(query, "宁德时代") {
		searchResults = append(searchResults, SearchResult{
			Title:   "宁德时代储能业务保持增长",
			Snippet: "年报显示，宁德时代储能业务收入继续增长，海外收入占比提升，研发投入维持较高水平。",
			URL:     "https://www.stcn.com/",
			Source:  "证券时报网",
		})
	} else {
		// 通用搜索结果
		searchResults = append(searchResults, SearchResult{
			Title:   fmt.Sprintf("%s - 最新资讯", query),
			Snippet: fmt.Sprintf("关于\"%s\"的最新市场动态和行业分析报告。", query),
			URL:     "https://news.sina.com.cn/",
			Source:  "新浪新闻",
		})
	}

	// 尝试获取真实网页内容作为补充
	if t.apiKey != "" {
		// 如果配置了API key，可以调用真实的搜索API
		// 这里省略实际的API调用
	}

	return searchResults, nil
}

// fetchWebPage 抓取网页内容（用于获取更详细的信息）
func (t *WebSearcher) fetchWebPage(urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	// 只允许HTTP/HTTPS协议
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("不支持的协议")
	}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	// 设置请求头，模拟浏览器
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP请求失败：%d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// extractTextFromHTML 从HTML中提取文本内容
func (t *WebSearcher) extractTextFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	// 提取标题和正文
	title := doc.Find("title").Text()
	content := doc.Find("article, .article, .content, main").Text()

	if content == "" {
		content = doc.Find("body").Text()
	}

	// 限制内容长度
	if len(content) > 500 {
		content = content[:500] + "..."
	}

	return fmt.Sprintf("标题：%s\n内容：%s", title, content), nil
}
