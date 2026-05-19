package retriever

import (
	"math"
	"strings"
	"sync"

	appmodel "stock_rag/internal/model"
)

// BM25 实现BM25检索算法
type BM25 struct {
	docs        []appmodel.Document
	avgDocLen   float64
	idf         map[string]float64
	docTermFreq [][]map[string]int
	bm25Params  BM25Params
	mu          float64 // 平滑参数
	sigma       float64 // BM25L参数
	initialized bool
	muMutex     sync.RWMutex
}

// BM25Params BM25算法参数
type BM25Params struct {
	K1 float64 // 词频饱和参数
	B  float64 // 文档长度归一化参数
}

// DefaultBM25Params 返回默认的BM25参数
func DefaultBM25Params() BM25Params {
	return BM25Params{
		K1: 1.2, // 标准值
		B:  0.75, // 标准值
	}
}

// NewBM25 创建BM25实例
func NewBM25(docs []appmodel.Document) *BM25 {
	bm25 := &BM25{
		docs:       docs,
		idf:        make(map[string]float64),
		bm25Params: DefaultBM25Params(),
		mu:         2000, // BM25+参数
		sigma:      0.25, // BM25L参数
	}
	bm25.initialize()
	return bm25
}

// initialize 初始化BM25所需的统计信息
func (b *BM25) initialize() {
	b.muMutex.Lock()
	defer b.muMutex.Unlock()

	if b.initialized {
		return
	}

	// 计算平均文档长度
	totalLen := 0
	for _, doc := range b.docs {
		totalLen += len(strings.Fields(doc.Content))
	}
	if len(b.docs) > 0 {
		b.avgDocLen = float64(totalLen) / float64(len(b.docs))
	} else {
		b.avgDocLen = 0
	}

	// 计算词频和文档频率
	b.docTermFreq = make([][]map[string]int, len(b.docs))
	df := make(map[string]int)

	for i, doc := range b.docs {
		terms := strings.Fields(strings.ToLower(doc.Content))
		termFreq := make(map[string]int)
		seenTerms := make(map[string]bool)

		for _, term := range terms {
			termFreq[term]++
			if !seenTerms[term] {
				df[term]++
				seenTerms[term] = true
			}
		}

		b.docTermFreq[i] = append(b.docTermFreq[i], termFreq)
	}

	// 计算IDF
	for term, freq := range df {
		numerator := float64(len(b.docs)) - float64(freq) + 0.5
		denominator := float64(freq) + 0.5
		b.idf[term] = math.Log(numerator / denominator)
	}

	b.initialized = true
}

// Search 执行BM25搜索
func (b *BM25) Search(query string, topK int) []appmodel.Document {
	b.muMutex.RLock()
	defer b.muMutex.RUnlock()

	if !b.initialized || len(b.docs) == 0 {
		return []appmodel.Document{}
	}

	queryTerms := strings.Fields(strings.ToLower(query))
	scores := make([]float64, len(b.docs))

	for i, doc := range b.docs {
		score := b.scoreDocument(doc, queryTerms, i)
		scores[i] = score
	}

	// 排序并返回前topK个结果
	rankedDocs := make([]appmodel.Document, 0, len(b.docs))
	rankedIndices := make([]int, len(b.docs))
	for i := range rankedIndices {
		rankedIndices[i] = i
	}

	// 按分数降序排序
	for i := 0; i < len(rankedIndices); i++ {
		for j := i + 1; j < len(rankedIndices); j++ {
			if scores[rankedIndices[j]] > scores[rankedIndices[i]] {
				rankedIndices[i], rankedIndices[j] = rankedIndices[j], rankedIndices[i]
			}
		}
	}

	// 取前topK个结果
	for i := 0; i < min(topK, len(rankedIndices)); i++ {
		rankedDocs = append(rankedDocs, b.docs[rankedIndices[i]])
	}

	return rankedDocs
}

// scoreDocument 计算文档与查询的BM25分数
func (b *BM25) scoreDocument(doc appmodel.Document, queryTerms []string, docIndex int) float64 {
	score := 0.0
	docLen := float64(len(strings.Fields(doc.Content)))

	// BM25+ 变体，添加了mu平滑
	for _, term := range queryTerms {
		if idf, ok := b.idf[term]; ok {
			// 计算词在文档中的频率
			tf := 0
			if len(b.docTermFreq[docIndex]) > 0 {
				if freq, ok := b.docTermFreq[docIndex][0][term]; ok {
					tf = freq
				}
			}

			// BM25+ 公式
			numerator := float64(tf) * (b.bm25Params.K1 + 1)
			denominator := float64(tf) + b.bm25Params.K1*(1-b.bm25Params.B+b.bm25Params.B*(docLen/b.avgDocLen))
			score += idf * (numerator / denominator)
		}
	}

	return score
}
