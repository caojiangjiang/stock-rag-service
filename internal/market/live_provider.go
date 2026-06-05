package market

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// LiveProvider 优先拉取真实基金净值，其余走 mock 兜底。
type LiveProvider struct {
	fallback *DefaultProvider
	cache    sync.Map // code -> cachedQuote
	ttl      time.Duration
}

type cachedQuote struct {
	quote Quote
	at    time.Time
}

func NewLiveProvider() *LiveProvider {
	return &LiveProvider{
		fallback: NewDefaultProvider(),
		ttl:      10 * time.Minute,
	}
}

func (p *LiveProvider) GetQuote(code string) Quote {
	return p.fallback.GetQuote(code)
}

func (p *LiveProvider) GetFundNAV(code string) Quote {
	code = normalizeFundCode(code)
	if code == "" {
		return Quote{HasQuote: false}
	}
	if q, ok := p.loadCache(code); ok {
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

	p.storeCache(code, q)
	return q
}

func (p *LiveProvider) loadCache(code string) (Quote, bool) {
	if v, ok := p.cache.Load(strings.ToUpper(code)); ok {
		c := v.(cachedQuote)
		if time.Since(c.at) < p.ttl {
			return c.quote, true
		}
		p.cache.Delete(strings.ToUpper(code))
	}
	return Quote{}, false
}

func (p *LiveProvider) storeCache(code string, q Quote) {
	p.cache.Store(strings.ToUpper(code), cachedQuote{quote: q, at: time.Now()})
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
