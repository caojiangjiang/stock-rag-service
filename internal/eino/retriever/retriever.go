package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"stock_rag/internal/embedding"
	appmodel "stock_rag/internal/model"
	"stock_rag/internal/utils"
	"stock_rag/internal/vectorstore"
)

// Retriever 描述当前链路依赖的最小检索契约。
type Retriever interface {
	Retrieve(ctx context.Context, req appmodel.RAGQueryRequest) ([]RetrievedChunk, error)
}

// DocumentStore 描述 retriever 对仓库层的最小读取依赖。
type DocumentStore interface {
	ListDocuments(ctx context.Context) ([]appmodel.Document, error)
}

// QueryOption 描述第一版检索过滤条件。
type QueryOption struct {
	StockCode string
	TimeRange string
	DocTypes  []string
	TopK      int
}

// QueryIntent 表示从自然语言问题中抽取出的结构化约束。
type QueryIntent struct {
	FiscalYear    int
	Metric        string
	MetricAliases []string
}

// HasConstraints 表示问题里是否包含显式年份/指标约束。
func (i QueryIntent) HasConstraints() bool {
	return i.FiscalYear > 0 || i.Metric != ""
}

// MatchesDocument 表示文档是否满足当前问题意图。
func (i QueryIntent) MatchesDocument(doc appmodel.Document) bool {
	if i.FiscalYear > 0 && documentFiscalYear(doc) != i.FiscalYear {
		return false
	}
	if i.Metric != "" && !i.matchesText(doc.Title, doc.Content, strings.Join(doc.Keywords, " ")) {
		return false
	}
	return true
}

// MatchesChunk 表示检索片段是否满足当前问题意图。
func (i QueryIntent) MatchesChunk(chunk RetrievedChunk) bool {
	if i.FiscalYear > 0 && fiscalYearFromTexts(chunk.Citation.Title, chunk.Content) != i.FiscalYear {
		return false
	}
	if i.Metric != "" && !i.matchesText(chunk.Citation.Title, chunk.Content) {
		return false
	}
	return true
}

func (i QueryIntent) matchesText(parts ...string) bool {
	if i.Metric == "" {
		return true
	}
	combined := strings.ToLower(strings.Join(parts, "\n"))
	for _, alias := range i.MetricAliases {
		if alias != "" && strings.Contains(combined, strings.ToLower(alias)) {
			return true
		}
	}
	return strings.Contains(combined, strings.ToLower(i.Metric))
}

// RetrievedChunk 表示当前阶段检索到的一个上下文片段。
type RetrievedChunk struct {
	Content  string
	Citation appmodel.Citation
}

// HybridRetrieverConfig 描述 hybrid retriever 的可选依赖。
type HybridRetrieverConfig struct {
	Store                    DocumentStore
	Embedder                 embedding.Embedder
	VectorStore              vectorstore.VectorStore
	LoadLocalSampleDocuments bool
}

// HybridRetriever 是当前阶段的可演进检索器实现。
//
// 优先级如下：
// 1. 若配置了 embedder + vector store，则先尝试向量检索；
// 2. 若未命中，则回退到导入文档 / testdata 的轻量关键词排序；
// 3. 再未命中，则回退到占位检索结果。
type HybridRetriever struct {
	store       DocumentStore
	embedder    embedding.Embedder
	vectorStore vectorstore.VectorStore
	docs        []appmodel.Document
}

// LocalSampleRetriever 是历史命名的兼容别名。
type LocalSampleRetriever = HybridRetriever

type scoredDocument struct {
	Doc   appmodel.Document
	Score int
}

// scoredSearchResult 表示带混合分数的搜索结果
type scoredSearchResult struct {
	Result         vectorstore.SearchResult
	HybridScore    float64
	VectorScore    float64
	TitleScore     float64
	FreshnessScore float64
	DocTypeScore   float64
	IntentScore    float64
}

type metricAliasGroup struct {
	Canonical string
	Aliases   []string
}

var (
	localDocsOnce sync.Once
	localDocs     []appmodel.Document
	localDocsErr  error
	fiscalYearRE  = regexp.MustCompile(`20[0-9]{2}`)
	metricAliases = []metricAliasGroup{
		{
			Canonical: "净利润",
			Aliases: []string{
				"归属于母公司所有者的净利润",
				"归母净利润",
				"净利润",
				"盈利",
				"利润",
				"earnings",
				"net profit",
				"profit",
			},
		},
		{
			Canonical: "营业收入",
			Aliases: []string{
				"营业总收入",
				"营业收入",
				"营收",
				"收入",
				"revenue",
			},
		},
		{
			Canonical: "现金流",
			Aliases: []string{
				"经营活动产生的现金流量净额",
				"现金流",
				"cash flow",
			},
		},
		{
			Canonical: "每股收益",
			Aliases: []string{
				"基本每股收益",
				"每股收益",
				"eps",
				"earnings per share",
			},
		},
	}
)

// DefaultQueryOption 返回推荐默认值。
func DefaultQueryOption() QueryOption {
	return QueryOption{
		TopK:     5,
		DocTypes: []string{"announcement", "report", "news"},
	}
}

// SimilarityThreshold 相似度阈值，低于此值的结果将被过滤
const SimilarityThreshold = 0.6

// NewHybridRetriever 创建可演进的 hybrid retriever。
func NewHybridRetriever(cfg HybridRetrieverConfig) HybridRetriever {
	retriever := HybridRetriever{
		store:       cfg.Store,
		embedder:    cfg.Embedder,
		vectorStore: cfg.VectorStore,
	}
	if !cfg.LoadLocalSampleDocuments {
		return retriever
	}

	docs, err := loadLocalSampleDocuments()
	if err != nil {
		return retriever
	}
	retriever.docs = docs
	return retriever
}

// NewLocalSampleRetriever 创建本地样本检索器。
func NewLocalSampleRetriever(store DocumentStore) LocalSampleRetriever {
	return NewHybridRetriever(HybridRetrieverConfig{Store: store, LoadLocalSampleDocuments: true})
}

// AnalyzeQuery 从问题里抽取结构化年份/指标约束。
func AnalyzeQuery(req appmodel.RAGQueryRequest) QueryIntent {
	metric, aliases := normalizeMetric(strings.TrimSpace(req.Question))
	return QueryIntent{
		FiscalYear:    fiscalYearFromTexts(req.Question),
		Metric:        metric,
		MetricAliases: aliases,
	}
}

// ExtractYears 返回文本中出现的 4 位年份。
func ExtractYears(text string) []int {
	matches := fiscalYearRE.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	years := make([]int, 0, len(matches))
	seen := make(map[int]struct{}, len(matches))
	for _, match := range matches {
		year, err := strconv.Atoi(match)
		if err != nil {
			continue
		}
		if _, exists := seen[year]; exists {
			continue
		}
		seen[year] = struct{}{}
		years = append(years, year)
	}
	return years
}

// Retrieve 返回当前阶段的本地样本检索结果。
func (r HybridRetriever) Retrieve(ctx context.Context, req appmodel.RAGQueryRequest) ([]RetrievedChunk, error) {
	option := DefaultQueryOption()
	intent := AnalyzeQuery(req)
	topK := option.TopK
	if req.TopK > 0 {
		topK = req.TopK
	}

	docTypes := req.DocTypes
	if len(docTypes) == 0 {
		docTypes = option.DocTypes
	}

	stockCode := strings.ToUpper(strings.TrimSpace(req.StockCode))
	if stockCode == "" {
		stockCode = "GENERAL"
	}

	timeRange := strings.TrimSpace(req.TimeRange)
	if timeRange == "" {
		timeRange = "latest"
	}

	question := strings.TrimSpace(req.Question)
	if question == "" {
		question = "未提供问题"
	}

	utils.Info("开始检索", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
		Message:   fmt.Sprintf("timeRange=%s, question=%s, docTypes=%v, fiscal_year=%d, metric=%s, local_only=%t", timeRange, question, docTypes, intent.FiscalYear, intent.Metric, req.UseLocalOnly),
	})

	if req.UseLocalOnly {
		chunks, found := r.retrieveLocalOnly(stockCode, timeRange, question, docTypes, topK, intent)
		if found {
			return chunks, nil
		}
		fallback := fallbackChunks(stockCode, timeRange, question, docTypes, topK)
		utils.Info("local-only 模式未命中，返回空结果", utils.LogFields{
			StockCode:      stockCode,
			TopK:           topK,
			RetrievedCount: len(fallback),
		})
		return fallback, nil
	}

	// 第一次检索：使用指定的 docTypes
	chunks, found, err := r.performRetrieval(ctx, stockCode, timeRange, question, docTypes, topK, intent)
	if err != nil {
		return nil, err
	}

	// 如果检索命中，直接返回结果
	if found && len(chunks) > 0 {
		return chunks, nil
	}

	// 如果用户没有指定 docTypes，尝试动态降级
	if len(req.DocTypes) == 0 {
		// 获取可用的 docTypes
		availableDocTypes := r.getAvailableDocTypes(ctx, stockCode)
		utils.Info("动态降级：获取可用的 docTypes", utils.LogFields{
			StockCode: stockCode,
			Message:   fmt.Sprintf("available_types=%v, original_types=%v", availableDocTypes, docTypes),
		})

		// 如果只有 report 类型可用，使用 report 类型重新检索
		if len(availableDocTypes) == 1 && availableDocTypes[0] == "report" {
			utils.Info("动态降级：只找到 report 类型，使用 report 类型重新检索", utils.LogFields{
				StockCode: stockCode,
			})
			chunks, found, err = r.performRetrieval(ctx, stockCode, timeRange, question, []string{"report"}, topK, intent)
			if err != nil {
				return nil, err
			}
			if found && len(chunks) > 0 {
				return chunks, nil
			}
		} else if len(availableDocTypes) > 0 && !equalStringSlices(availableDocTypes, docTypes) {
			// 如果有其他可用类型，使用所有可用类型重新检索
			utils.Info("动态降级：使用所有可用类型重新检索", utils.LogFields{
				StockCode: stockCode,
				Message:   fmt.Sprintf("available_types=%v", availableDocTypes),
			})
			chunks, found, err = r.performRetrieval(ctx, stockCode, timeRange, question, availableDocTypes, topK, intent)
			if err != nil {
				return nil, err
			}
			if found && len(chunks) > 0 {
				return chunks, nil
			}
		}
	}

	// 返回回退结果
	fallback := fallbackChunks(stockCode, timeRange, question, docTypes, topK)
	utils.Info("返回回退结果", utils.LogFields{
		StockCode:      stockCode,
		TopK:           topK,
		RetrievedCount: len(fallback),
	})

	return fallback, nil
}

// performRetrieval 执行检索操作
func (r HybridRetriever) performRetrieval(ctx context.Context, stockCode, timeRange, question string, docTypes []string, topK int, intent QueryIntent) ([]RetrievedChunk, bool, error) {
	// 并行执行向量检索和BM25检索
	vectorChunksChan := make(chan []RetrievedChunk)
	bm25ChunksChan := make(chan []RetrievedChunk)
	errorChan := make(chan error)

	// 启动向量检索协程
	go func() {
		vectorChunks, found, err := r.retrieveByVectorSearch(ctx, stockCode, timeRange, question, docTypes, topK, intent) // 获取更多结果用于合并
		if err != nil {
			errorChan <- err
			return
		}
		if found {
			vectorChunksChan <- vectorChunks
		} else {
			vectorChunksChan <- nil
		}
	}()

	// 启动BM25检索协程
	go func() {
		bm25Chunks, found := r.retrieveByBM25(stockCode, timeRange, question, docTypes, topK*2, intent) // 获取更多结果用于合并
		if found {
			bm25ChunksChan <- bm25Chunks
		} else {
			bm25ChunksChan <- nil
		}
	}()

	// 等待检索结果
	var vectorChunks, bm25Chunks []RetrievedChunk
	var err error

	done := 0
	for done < 2 {
		select {
		case chunks := <-vectorChunksChan:
			vectorChunks = chunks
			done++
		case chunks := <-bm25ChunksChan:
			bm25Chunks = chunks
			done++
		case e := <-errorChan:
			err = e
			done++
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}

	if err != nil {
		utils.Error("检索错误", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
			Message:   err.Error(),
		})
		return nil, false, err
	}

	// 合并并重新排序结果
	mergedChunks := r.mergeAndRankChunks(vectorChunks, bm25Chunks, topK)
	if len(mergedChunks) > 0 {
		utils.Info("混合检索命中", utils.LogFields{
			StockCode:      stockCode,
			TopK:           topK,
			RetrievedCount: len(mergedChunks),
			Message:        fmt.Sprintf("向量检索:%d, BM25检索:%d", len(vectorChunks), len(bm25Chunks)),
		})
		return mergedChunks, true, nil
	}
	utils.Info("混合检索未命中", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
	})

	if fallbackSearcher, ok := r.vectorStore.(vectorstore.DocumentFallbackSearcher); ok {
		docs, err := fallbackSearcher.FallbackDocumentSearch(ctx, vectorstore.KeywordSearchRequest{
			QueryText: strings.TrimSpace(question),
			Terms:     buildKeywordSearchTerms(question, intent),
			TopK:      topK * 4,
			Filter: vectorstore.Filter{
				StockCode: stockCode,
				DocTypes:  append([]string(nil), docTypes...),
				TimeRange: timeRange,
			},
		})
		if err != nil {
			utils.Warning("数据库侧文档 fallback 搜索失败，回退到 ListDocuments 扫描", utils.LogFields{
				StockCode: stockCode,
				TopK:      topK,
				Message:   err.Error(),
			})
		} else if len(docs) > 0 {
			matched := r.matchDocuments(docs, stockCode, timeRange, question, docTypes, intent)
			if len(matched) > 0 {
				resultChunks := chunksFromDocuments(matched, topK)
				utils.Info("数据库侧文档 fallback 命中", utils.LogFields{
					StockCode:      stockCode,
					TopK:           topK,
					RetrievedCount: len(resultChunks),
				})
				return resultChunks, true, nil
			}
		}
	}

	// 尝试本地数据库搜索
	if r.store != nil {
		repoDocs, err := r.store.ListDocuments(ctx)
		if err != nil {
			utils.Error("数据库搜索错误", utils.LogFields{
				StockCode: stockCode,
				TopK:      topK,
				Message:   err.Error(),
			})
			return nil, false, err
		}
		utils.Info("从数据库获取到文档", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
			Message:   fmt.Sprintf("%d 个文档", len(repoDocs)),
		})

		matched := r.matchDocuments(repoDocs, stockCode, timeRange, question, docTypes, intent)
		if len(matched) > 0 {
			resultChunks := chunksFromDocuments(matched, topK)
			utils.Info("数据库搜索命中", utils.LogFields{
				StockCode:      stockCode,
				TopK:           topK,
				RetrievedCount: len(resultChunks),
			})
			return resultChunks, true, nil
		}
		utils.Info("数据库搜索未命中", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
		})
	} else {
		utils.Info("未配置 DocumentStore，跳过数据库搜索", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
		})
	}

	// 尝试本地样本文档搜索
	utils.Info("本地样本文档数量", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
		Message:   fmt.Sprintf("%d", len(r.docs)),
	})
	matched := r.matchDocuments(r.docs, stockCode, timeRange, question, docTypes, intent)
	if len(matched) > 0 {
		resultChunks := chunksFromDocuments(matched, topK)
		utils.Info("本地样本文档搜索命中", utils.LogFields{
			StockCode:      stockCode,
			TopK:           topK,
			RetrievedCount: len(resultChunks),
		})
		return resultChunks, true, nil
	}
	utils.Info("本地样本文档搜索未命中", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
	})

	return nil, false, nil
}

func (r HybridRetriever) retrieveLocalOnly(stockCode, timeRange, question string, docTypes []string, topK int, intent QueryIntent) ([]RetrievedChunk, bool) {
	utils.Info("启用 local-only 检索模式", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
		Message:   fmt.Sprintf("local_docs=%d", len(r.docs)),
	})
	matched := r.matchDocuments(r.docs, stockCode, timeRange, question, docTypes, intent)
	if len(matched) == 0 {
		return nil, false
	}
	resultChunks := chunksFromDocuments(matched, topK)
	utils.Info("local-only 检索命中", utils.LogFields{
		StockCode:      stockCode,
		TopK:           topK,
		RetrievedCount: len(resultChunks),
	})
	return resultChunks, true
}

// mergedChunk 表示合并后的检索结果
type mergedChunk struct {
	chunk  RetrievedChunk
	score  float64
	source string // "vector" 或 "bm25"
}

// mergeAndRankChunks 合并并重新排序检索结果
func (r HybridRetriever) mergeAndRankChunks(vectorChunks, bm25Chunks []RetrievedChunk, topK int) []RetrievedChunk {
	// 去重并计算综合分数
	chunkMap := make(map[string]*mergedChunk)

	// 添加向量检索结果
	for i, chunk := range vectorChunks {
		key := chunk.Citation.Title + "|" + chunk.Citation.SourceURL
		if _, exists := chunkMap[key]; !exists {
			// 向量检索分数：位置越靠前分数越高
			score := 1.0 - float64(i)/float64(len(vectorChunks))
			chunkMap[key] = &mergedChunk{
				chunk:  chunk,
				score:  score * 0.6, // 向量检索权重
				source: "vector",
			}
		} else {
			// 如果已经存在，增加分数
			chunkMap[key].score += (1.0 - float64(i)/float64(len(vectorChunks))) * 0.6
		}
	}

	// 添加BM25检索结果
	for i, chunk := range bm25Chunks {
		key := chunk.Citation.Title + "|" + chunk.Citation.SourceURL
		if _, exists := chunkMap[key]; !exists {
			// BM25检索分数：位置越靠前分数越高
			score := 1.0 - float64(i)/float64(len(bm25Chunks))
			chunkMap[key] = &mergedChunk{
				chunk:  chunk,
				score:  score * 0.4, // BM25检索权重
				source: "bm25",
			}
		} else {
			// 如果已经存在，增加分数
			chunkMap[key].score += (1.0 - float64(i)/float64(len(bm25Chunks))) * 0.4
		}
	}

	// 转换为切片并排序
	mergedList := make([]*mergedChunk, 0, len(chunkMap))
	for _, mc := range chunkMap {
		mergedList = append(mergedList, mc)
	}

	// 按分数降序排序
	sort.SliceStable(mergedList, func(i, j int) bool {
		return mergedList[i].score > mergedList[j].score
	})

	// 取前topK个结果
	result := make([]RetrievedChunk, 0, min(topK, len(mergedList)))
	for i, mc := range mergedList {
		if i >= topK {
			break
		}
		result = append(result, mc.chunk)
	}

	return result
}

// retrieveByBM25 使用BM25算法进行检索
func (r HybridRetriever) retrieveByBM25(stockCode, timeRange, question string, docTypes []string, topK int, intent QueryIntent) ([]RetrievedChunk, bool) {
	if keywordSearcher, ok := r.vectorStore.(vectorstore.KeywordSearcher); ok {
		results, err := keywordSearcher.KeywordSearch(context.Background(), vectorstore.KeywordSearchRequest{
			QueryText: strings.TrimSpace(question),
			Terms:     buildKeywordSearchTerms(question, intent),
			TopK:      topK * 2,
			Filter: vectorstore.Filter{
				StockCode: stockCode,
				DocTypes:  append([]string(nil), docTypes...),
				TimeRange: timeRange,
			},
		})
		if err == nil && len(results) > 0 {
			filteredResults := filterSearchResultsByIntent(results, intent)
			if len(filteredResults) > 0 {
				return chunksFromSearchResults(filteredResults, topK), true
			}
		}
	}

	// 准备文档集合
	docs := r.docs
	if r.store != nil {
		// 从数据库获取文档
		ctx := context.Background()
		repoDocs, err := r.store.ListDocuments(ctx)
		if err == nil && len(repoDocs) > 0 {
			docs = append(docs, repoDocs...)
		}
	}

	if len(docs) == 0 {
		return nil, false
	}

	// 过滤文档
	filteredDocs := make([]appmodel.Document, 0)
	allowedDocTypes := make(map[string]struct{}, len(docTypes))
	for _, docType := range docTypes {
		allowedDocTypes[strings.ToLower(strings.TrimSpace(docType))] = struct{}{}
	}

	for _, doc := range docs {
		if stockCode != "GENERAL" && !strings.EqualFold(doc.StockCode, stockCode) {
			continue
		}
		if _, ok := allowedDocTypes[strings.ToLower(doc.DocType)]; !ok {
			continue
		}
		if !matchesTimeRange(doc.Published, timeRange) {
			continue
		}
		if intent.HasConstraints() && !intent.MatchesDocument(doc) {
			continue
		}
		filteredDocs = append(filteredDocs, doc)
	}

	if len(filteredDocs) == 0 {
		return nil, false
	}

	// 创建BM25实例并搜索
	bm25 := NewBM25(filteredDocs)
	rankedDocs := bm25.Search(question, topK*2) // 获取更多结果

	if len(rankedDocs) == 0 {
		return nil, false
	}

	// 转换为RetrievedChunk
	chunks := chunksFromDocuments(rankedDocs, topK)
	return chunks, true
}

func buildKeywordSearchTerms(question string, intent QueryIntent) []string {
	question = strings.TrimSpace(question)
	seen := make(map[string]struct{})
	terms := make([]string, 0, 8)
	addTerm := func(term string) {
		term = normalizeKeywordTerm(term)
		if term == "" {
			return
		}
		if _, exists := seen[term]; exists {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}

	addTerm(question)
	for _, alias := range intent.MetricAliases {
		addTerm(alias)
	}
	for _, year := range ExtractYears(question) {
		addTerm(strconv.Itoa(year))
		addTerm(fmt.Sprintf("%d年", year))
	}

	replacer := strings.NewReplacer(
		"？", " ", "?", " ", "，", " ", ",", " ", "。", " ", ".", " ",
		"、", " ", "：", " ", ":", " ", "；", " ", ";", " ", "（", " ", "）", " ",
		"(", " ", ")", " ", "和", " ", "及", " ",
	)
	for _, part := range strings.Fields(replacer.Replace(question)) {
		addTerm(part)
	}

	return terms
}

func normalizeKeywordTerm(term string) string {
	term = strings.TrimSpace(term)
	if term == "" {
		return ""
	}
	noiseReplacer := strings.NewReplacer(
		"是多少", "", "怎么样", "", "如何", "", "情况", "", "进展", "", "表现", "", "最近", "",
		"请问", "", "一下", "", "有关", "", "相关", "", "？", "", "?", "",
	)
	term = strings.TrimSpace(noiseReplacer.Replace(term))
	if utf8.RuneCountInString(term) < 2 && !containsASCIIAlphaNum(term) {
		return ""
	}
	return term
}

func containsASCIIAlphaNum(text string) bool {
	for _, ch := range text {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			return true
		}
	}
	return false
}

func filterSearchResultsByIntent(results []vectorstore.SearchResult, intent QueryIntent) []vectorstore.SearchResult {
	if !intent.HasConstraints() {
		return results
	}
	filtered := make([]vectorstore.SearchResult, 0, len(results))
	for _, result := range results {
		doc := appmodel.Document{
			Title:     result.Citation.Title,
			DocType:   result.Citation.DocType,
			SourceURL: result.Citation.SourceURL,
			Published: result.Citation.Published,
			Content:   result.Content,
		}
		if intent.MatchesDocument(doc) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

// getAvailableDocTypes 获取当前股票/当前库里可用的 docTypes
func (r HybridRetriever) getAvailableDocTypes(ctx context.Context, stockCode string) []string {
	availableTypes := make(map[string]bool)
	if docTypeLister, ok := r.vectorStore.(vectorstore.AvailableDocTypeLister); ok {
		docTypes, err := docTypeLister.ListAvailableDocTypes(ctx, stockCode)
		if err == nil {
			for _, docType := range docTypes {
				docType = strings.ToLower(strings.TrimSpace(docType))
				if docType != "" {
					availableTypes[docType] = true
				}
			}
		}
	}

	// 从数据库获取可用的 docTypes
	if r.store != nil {
		repoDocs, err := r.store.ListDocuments(ctx)
		if err == nil {
			for _, doc := range repoDocs {
				if stockCode == "GENERAL" || strings.EqualFold(doc.StockCode, stockCode) {
					docType := strings.ToLower(strings.TrimSpace(doc.DocType))
					if docType != "" {
						availableTypes[docType] = true
					}
				}
			}
		}
	}

	// 从本地样本文档获取可用的 docTypes
	for _, doc := range r.docs {
		if stockCode == "GENERAL" || strings.EqualFold(doc.StockCode, stockCode) {
			docType := strings.ToLower(strings.TrimSpace(doc.DocType))
			if docType != "" {
				availableTypes[docType] = true
			}
		}
	}

	// 转换为切片并返回
	result := make([]string, 0, len(availableTypes))
	for docType := range availableTypes {
		result = append(result, docType)
	}
	return result
}

// equalStringSlices 比较两个字符串切片是否相等
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// 创建映射以比较元素
	mapA := make(map[string]bool)
	for _, item := range a {
		mapA[item] = true
	}

	for _, item := range b {
		if !mapA[item] {
			return false
		}
	}

	return true
}

func (r HybridRetriever) retrieveByVectorSearch(ctx context.Context, stockCode, timeRange, question string, docTypes []string, topK int, intent QueryIntent) ([]RetrievedChunk, bool, error) {
	if r.embedder == nil || r.vectorStore == nil {
		utils.Info("跳过向量检索", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
			Message:   "embedder或vectorStore未配置",
		})
		return nil, false, nil
	}

	utils.Info("开始生成查询嵌入向量", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
	})
	start := time.Now()
	queryVector, err := r.embedder.EmbedQuery(ctx, question)
	elapsed := time.Since(start)
	if err != nil {
		utils.Error("嵌入生成错误", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
			Elapsed:   elapsed,
			Message:   err.Error(),
		})
		return nil, false, nil
	}
	utils.Info("嵌入向量生成完成", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
		Elapsed:   elapsed,
		Message:   fmt.Sprintf("维度: %d", len(queryVector)),
	})

	utils.Info("开始向量搜索", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
	})

	// 打印向量搜索输入参数
	inputParams := fmt.Sprintf("问题: %s, 股票代码: %s, TopK: %d, 文档类型: %v, 时间范围: %s, 向量维度: %d",
		question, stockCode, topK, docTypes, timeRange, len(queryVector))
	utils.Info("向量搜索输入参数", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
		Message:   inputParams,
	})

	start = time.Now()
	results, err := r.vectorStore.Search(ctx, vectorstore.SearchRequest{
		QueryText:           question,
		QueryVector:         queryVector,
		TopK:                topK,
		SimilarityThreshold: SimilarityThreshold,
		Filter: vectorstore.Filter{
			StockCode: stockCode,
			DocTypes:  append([]string(nil), docTypes...),
			TimeRange: timeRange,
		},
	})
	elapsed = time.Since(start)
	if err != nil {
		utils.Error("向量存储搜索错误", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
			Elapsed:   elapsed,
			Message:   err.Error(),
		})
		return nil, false, nil
	}
	utils.Info("向量搜索完成", utils.LogFields{
		StockCode:      stockCode,
		TopK:           topK,
		Elapsed:        elapsed,
		RetrievedCount: len(results),
	})

	// 输出向量搜索结果到日志
	if len(results) > 0 {
		var resultSummary strings.Builder
		for i, result := range results {
			if i > 4 { // 只打印前5个结果
				resultSummary.WriteString(fmt.Sprintf("\n... 还有 %d 个结果", len(results)-5))
				break
			}
			content := result.Content
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			resultSummary.WriteString(fmt.Sprintf("\n[%d] 标题: %s, 文档类型: %s, 相似度: %.4f\n内容预览: %s",
				i+1, result.Citation.Title, result.Citation.DocType, result.Score, content))
		}
		utils.Info("向量搜索输出结果", utils.LogFields{
			StockCode:      stockCode,
			TopK:           topK,
			RetrievedCount: len(results),
			Message:        resultSummary.String(),
		})
	}

	if len(results) == 0 {
		utils.Info("向量搜索未命中结果", utils.LogFields{
			StockCode: stockCode,
			TopK:      topK,
		})
		return nil, false, nil
	}

	// 计算混合分
	utils.Info("开始计算混合分", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
	})
	start = time.Now()
	questionLower := strings.ToLower(question)
	scoredResults := make([]scoredSearchResult, 0, len(results))

	for _, result := range results {
		candidate := RetrievedChunk{Content: result.Content, Citation: result.Citation}
		if intent.HasConstraints() && !intent.MatchesChunk(candidate) {
			continue
		}

		// 基础向量相似度分数
		vectorScore := result.Score

		// 标题/章节关键词命中分数
		titleScore := 0.0
		if strings.Contains(questionLower, strings.ToLower(result.Citation.Title)) {
			titleScore = 0.3
		}

		// 文档新鲜度分数（假设published格式为YYYY-MM-DD或类似格式）
		freshnessScore := 0.0
		if result.Citation.Published != "" {
			// 简单计算：越新的文档分数越高
			// 这里使用一个简单的启发式方法，假设最近一年的文档分数为0.2，每早一年减0.05
			currentYear := time.Now().Year()
			if len(result.Citation.Published) >= 4 {
				docYear := 0
				fmt.Sscanf(result.Citation.Published[:4], "%d", &docYear)
				if docYear > 0 {
					yearDiff := currentYear - docYear
					if yearDiff < 0 {
						yearDiff = 0
					}
					freshnessScore = math.Max(0, 0.2-0.05*float64(yearDiff))
				}
			}
		}

		// 文档类型权重
		docTypeScore := 0.0
		switch strings.ToLower(result.Citation.DocType) {
		case "report": // 年报
			docTypeScore = 0.2
		case "announcement": // 公告
			docTypeScore = 0.15
		case "news": // 新闻
			docTypeScore = 0.1
		default:
			docTypeScore = 0.05
		}

		intentScore := 0.0
		if intent.FiscalYear > 0 {
			intentScore += 0.35
		}
		if intent.Metric != "" {
			intentScore += 0.25
		}

		// 计算混合分数
		hybridScore := vectorScore + titleScore + freshnessScore + docTypeScore + intentScore

		scoredResults = append(scoredResults, scoredSearchResult{
			Result:         result,
			HybridScore:    hybridScore,
			VectorScore:    vectorScore,
			TitleScore:     titleScore,
			FreshnessScore: freshnessScore,
			DocTypeScore:   docTypeScore,
			IntentScore:    intentScore,
		})
	}

	if len(scoredResults) == 0 {
		return nil, false, nil
	}

	// 按混合分数排序
	sort.SliceStable(scoredResults, func(i, j int) bool {
		return scoredResults[i].HybridScore > scoredResults[j].HybridScore
	})
	elapsed = time.Since(start)
	utils.Info("混合分计算完成", utils.LogFields{
		StockCode: stockCode,
		TopK:      topK,
		Elapsed:   elapsed,
	})

	// 过滤相似度低于阈值的结果
	filteredResults := make([]scoredSearchResult, 0)
	for _, scoredResult := range scoredResults {
		if scoredResult.HybridScore >= SimilarityThreshold {
			filteredResults = append(filteredResults, scoredResult)
		}
	}
	utils.Info("相似度过滤完成", utils.LogFields{
		StockCode: stockCode,
		TopK:      len(filteredResults),
	})

	// 取前topK个结果
	topResults := make([]vectorstore.SearchResult, 0, min(topK, len(filteredResults)))
	for i, scoredResult := range filteredResults {
		if i >= topK {
			break
		}
		topResults = append(topResults, scoredResult.Result)
	}

	// 打印前几个结果的详细信息
	utils.Info("前几个检索结果", utils.LogFields{
		StockCode:      stockCode,
		TopK:           topK,
		RetrievedCount: len(topResults),
	})

	return chunksFromSearchResults(topResults, topK), true, nil
}

func (r HybridRetriever) matchDocuments(docs []appmodel.Document, stockCode, timeRange, question string, docTypes []string, intent QueryIntent) []appmodel.Document {
	allowedDocTypes := make(map[string]struct{}, len(docTypes))
	for _, docType := range docTypes {
		allowedDocTypes[strings.ToLower(strings.TrimSpace(docType))] = struct{}{}
	}

	questionLower := strings.ToLower(question)
	scored := make([]scoredDocument, 0, len(docs))
	for _, doc := range docs {
		if stockCode != "GENERAL" && !strings.EqualFold(doc.StockCode, stockCode) {
			continue
		}
		if _, ok := allowedDocTypes[strings.ToLower(doc.DocType)]; !ok {
			continue
		}
		if !matchesTimeRange(doc.Published, timeRange) {
			continue
		}
		if intent.HasConstraints() && !intent.MatchesDocument(doc) {
			continue
		}

		score := 1
		if intent.FiscalYear > 0 && documentFiscalYear(doc) == intent.FiscalYear {
			score += 6
		}
		if intent.Metric != "" && intent.matchesText(doc.Title, doc.Content, strings.Join(doc.Keywords, " ")) {
			score += 4
		}
		for _, keyword := range doc.Keywords {
			if keyword != "" && strings.Contains(questionLower, strings.ToLower(keyword)) {
				score += 3
			}
		}
		if strings.Contains(questionLower, strings.ToLower(doc.Title)) {
			score += 2
		}

		scored = append(scored, scoredDocument{Doc: doc, Score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Doc.Published > scored[j].Doc.Published
	})

	matched := make([]appmodel.Document, 0, len(scored))
	for _, item := range scored {
		matched = append(matched, item.Doc)
	}
	return matched
}

func normalizeMetric(question string) (string, []string) {
	questionLower := strings.ToLower(strings.TrimSpace(question))
	for _, group := range metricAliases {
		for _, alias := range group.Aliases {
			if alias != "" && strings.Contains(questionLower, strings.ToLower(alias)) {
				return group.Canonical, append([]string(nil), group.Aliases...)
			}
		}
	}
	return "", nil
}

func fiscalYearFromTexts(texts ...string) int {
	for _, text := range texts {
		years := ExtractYears(text)
		if len(years) > 0 {
			return years[0]
		}
	}
	return 0
}

func documentFiscalYear(doc appmodel.Document) int {
	return fiscalYearFromTexts(doc.Title, doc.Content)
}

func loadLocalSampleDocuments() ([]appmodel.Document, error) {
	localDocsOnce.Do(func() {
		path, err := localSampleDocumentsPath()
		if err != nil {
			localDocsErr = err
			return
		}

		data, err := os.ReadFile(path)
		if err != nil {
			localDocsErr = err
			return
		}

		if err := json.Unmarshal(data, &localDocs); err != nil {
			localDocsErr = err
		}
	})

	return localDocs, localDocsErr
}

func chunksFromDocuments(docs []appmodel.Document, topK int) []RetrievedChunk {
	chunks := make([]RetrievedChunk, 0, min(topK, len(docs)))
	for i, doc := range docs {
		if i >= topK {
			break
		}

		chunks = append(chunks, RetrievedChunk{
			Content: doc.Content,
			Citation: appmodel.Citation{
				Title:     doc.Title,
				DocType:   doc.DocType,
				SourceURL: doc.SourceURL,
				Published: doc.Published,
			},
		})
	}

	return chunks
}

func chunksFromSearchResults(results []vectorstore.SearchResult, topK int) []RetrievedChunk {
	chunks := make([]RetrievedChunk, 0, min(topK, len(results)))
	for i, result := range results {
		if i >= topK {
			break
		}

		chunks = append(chunks, RetrievedChunk{
			Content:  result.Content,
			Citation: result.Citation,
		})
	}

	return chunks
}

func localSampleDocumentsPath() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate retriever source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../testdata/local_documents.json")), nil
}

func matchesTimeRange(published, timeRange string) bool {
	rangeText := strings.TrimSpace(strings.ToLower(timeRange))
	if rangeText == "" || rangeText == "latest" {
		return true
	}

	publishedAt, err := time.Parse("2006-01-02", strings.TrimSpace(published))
	if err != nil {
		return true
	}

	// 将发布时间设置为当天的结束时间，避免边界条件问题
	// 例如：2026-03-08 变为 2026-03-08 23:59:59
	publishedAt = publishedAt.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	var window time.Duration
	switch rangeText {
	case "7d":
		window = 7 * 24 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
	case "90d":
		window = 90 * 24 * time.Hour
	case "180d":
		window = 180 * 24 * time.Hour
	case "365d", "1y":
		window = 365 * 24 * time.Hour
	default:
		return true
	}

	return time.Since(publishedAt) <= window
}

func fallbackChunks(stockCode, timeRange, question string, docTypes []string, topK int) []RetrievedChunk {
	// 对于正式模式，返回空结果，避免构造占位型结果
	// 这样在面试时可以明确说明：没有证据就返回空 citations
	return []RetrievedChunk{}
}
