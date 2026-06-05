package theme

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

const (
	snapshotCacheKey    = "all"
	symbolIndexCacheKey = "all"
)

func parseSnapshotCacheTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("THEME_SNAPSHOT_TTL"))
	if raw == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 30*time.Second {
		return 5 * time.Minute
	}
	return d
}

func symbolIndexTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("THEME_SYMBOL_INDEX_TTL"))
	if raw == "" {
		return time.Hour
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Minute {
		return time.Hour
	}
	return d
}

func (s *Service) cacheGet(ctx context.Context) (*SnapshotResponse, time.Time, bool) {
	if s.snapshotStore == nil || !s.snapshotStore.Enabled() {
		return nil, time.Time{}, false
	}
	var resp SnapshotResponse
	at, ok, err := s.snapshotStore.Get(ctx, snapshotCacheKey, &resp)
	if err != nil {
		log.Printf("theme snapshot redis get: %v", err)
		return nil, time.Time{}, false
	}
	if !ok {
		return nil, time.Time{}, false
	}
	return &resp, at, true
}

func (s *Service) cacheSet(ctx context.Context, resp *SnapshotResponse) {
	if s.snapshotStore == nil || !s.snapshotStore.Enabled() || resp == nil {
		return
	}
	if err := s.snapshotStore.Set(ctx, snapshotCacheKey, resp, s.cacheTTL); err != nil {
		log.Printf("theme snapshot redis set: %v", err)
	}
}

func (s *Service) invalidateCache(ctx context.Context) {
	if s.snapshotStore == nil || !s.snapshotStore.Enabled() {
		return
	}
	if err := s.snapshotStore.Delete(ctx, snapshotCacheKey); err != nil {
		log.Printf("theme snapshot redis del: %v", err)
	}
}

func filterSnapshot(resp *SnapshotResponse, themeID, marketFilter string, cached bool, cachedAt time.Time) *SnapshotResponse {
	if resp == nil {
		return nil
	}
	themes := resp.Themes
	if themeID != "" {
		filtered := make([]ThemeSnapshot, 0, len(themes))
		for _, th := range themes {
			if strings.EqualFold(th.ID, themeID) {
				filtered = append(filtered, th)
			}
		}
		themes = filtered
	}
	if marketFilter != "" {
		mf := strings.ToLower(marketFilter)
		filtered := make([]ThemeSnapshot, 0, len(themes))
		for _, th := range themes {
			if strings.ToLower(th.Market) == mf {
				filtered = append(filtered, th)
			}
		}
		themes = filtered
	}

	out := &SnapshotResponse{
		AsOf:        resp.AsOf,
		Themes:      themes,
		Cached:      cached,
		CacheAgeSec: int(time.Since(cachedAt).Seconds()),
	}
	if out.CacheAgeSec < 0 {
		out.CacheAgeSec = 0
	}
	return out
}

// StartBackgroundRefresh 定时预热主题快照；Redis 已有缓存则跳过启动预热。
func (s *Service) StartBackgroundRefresh(ctx context.Context) {
	if s.cacheTTL <= 0 {
		return
	}
	go func() {
		startCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.snapshotStore != nil && s.snapshotStore.Enabled() {
			exists, err := s.snapshotStore.Exists(startCtx, snapshotCacheKey)
			if err != nil {
				log.Printf("theme snapshot redis exists: %v", err)
			} else if exists {
				log.Println("theme snapshot: redis cache hit, skip startup warm")
			} else {
				s.warmSnapshot(context.Background())
			}
		} else {
			s.warmSnapshot(context.Background())
		}

		ticker := time.NewTicker(s.cacheTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.warmSnapshot(context.Background())
			}
		}
	}()
}

func (s *Service) warmSnapshot(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	resp, err := s.buildSnapshotFresh(ctx)
	if err != nil {
		log.Printf("theme snapshot background refresh: %v", err)
		return
	}
	s.cacheSet(ctx, resp)
	log.Printf("theme snapshot refreshed: %d themes, ttl=%s", len(resp.Themes), s.cacheTTL)
}

// RedisEnabled 是否已接入 Redis 缓存。
func (s *Service) RedisEnabled() bool {
	return s.snapshotStore != nil && s.snapshotStore.Enabled()
}
