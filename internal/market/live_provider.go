package market

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// LiveProvider 优先拉取真实行情（股票/基金），mock 兜底。
type LiveProvider struct {
	fallback *DefaultProvider
	cache    sync.Map // key -> cachedQuote
	ttl      time.Duration
	fundTTL  time.Duration
}

type cachedQuote struct {
	quote Quote
	at    time.Time
}

func NewLiveProvider() *LiveProvider {
	return &LiveProvider{
		fallback: NewDefaultProvider(),
		ttl:      2 * time.Minute,
		fundTTL:  10 * time.Minute,
	}
}

func (p *LiveProvider) GetQuote(code string) Quote {
	key := cacheKey(code, inferMarket(code))
	if q, ok := p.loadCacheKey(key, p.ttl); ok {
		return q
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	mkt := inferMarket(code)
	q, err := FetchStockQuoteFromEastMoney(ctx, code, mkt)
	if err != nil {
		log.Printf("stock quote fetch %s: %v, fallback mock", code, err)
		if mock := p.fallback.GetQuote(code); mock.HasQuote {
			mock.Source = "mock"
			return mock
		}
		return Quote{StockCode: code, HasQuote: false}
	}
	p.storeCacheKey(key, q)
	return q
}

func (p *LiveProvider) GetFundNAV(code string) Quote {
	code = normalizeFundCode(code)
	if code == "" {
		return Quote{HasQuote: false}
	}
	if q, ok := p.loadCacheKey(cacheKey(code, "fund"), p.fundTTL); ok {
		return q
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	q, err := FetchFundNAVFromEastMoney(ctx, code)
	if err != nil {
		log.Printf("fund nav fetch %s: %v, fallback mock", code, err)
		if mock := p.fallback.GetQuote(code); mock.HasQuote {
			mock.Source = "mock"
			return mock
		}
		return Quote{StockCode: code, HasQuote: false}
	}

	p.storeCacheKey(cacheKey(code, "fund"), q)
	return q
}

func cacheKey(code, kind string) string {
	return strings.ToUpper(strings.TrimSpace(kind)) + ":" + strings.ToUpper(strings.TrimSpace(code))
}

func (p *LiveProvider) loadCacheKey(key string, ttl time.Duration) (Quote, bool) {
	if v, ok := p.cache.Load(key); ok {
		c := v.(cachedQuote)
		if time.Since(c.at) < ttl {
			return c.quote, true
		}
		p.cache.Delete(key)
	}
	return Quote{}, false
}

func (p *LiveProvider) storeCacheKey(key string, q Quote) {
	p.cache.Store(key, cachedQuote{quote: q, at: time.Now()})
}

// LookupFundNAV 供 API 使用，返回错误或有效净值。
func LookupFundNAV(code string) (Quote, error) {
	if !isLikelyFundCode(code) {
		return Quote{}, fmt.Errorf("invalid fund code: %s", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return FetchFundNAVFromEastMoney(ctx, normalizeFundCode(code))
}

// LookupStockQuote 供 API 拉取股票/指数行情。
func LookupStockQuote(code, market string) (Quote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return FetchStockQuoteFromEastMoney(ctx, code, market)
}
