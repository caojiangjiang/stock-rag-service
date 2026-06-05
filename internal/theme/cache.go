package theme

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

type snapshotCacheEntry struct {
	resp *SnapshotResponse
	at   time.Time
}

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

func (s *Service) cacheGet() (*snapshotCacheEntry, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	if s.cache == nil {
		return nil, false
	}
	entry, ok := s.cache["all"]
	if !ok || entry.resp == nil {
		return nil, false
	}
	if time.Since(entry.at) >= s.cacheTTL {
		return nil, false
	}
	return &entry, true
}

func (s *Service) cacheSet(resp *SnapshotResponse) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cache == nil {
		s.cache = make(map[string]snapshotCacheEntry)
	}
	s.cache["all"] = snapshotCacheEntry{resp: resp, at: time.Now()}
}

func (s *Service) invalidateCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.cache, "all")
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

// StartBackgroundRefresh 定时预热主题快照，避免用户点击时触发东财请求。
func (s *Service) StartBackgroundRefresh(ctx context.Context) {
	if s.cacheTTL <= 0 {
		return
	}
	go func() {
		s.warmSnapshot(context.Background())
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
	s.cacheSet(resp)
	log.Printf("theme snapshot refreshed: %d themes, ttl=%s", len(resp.Themes), s.cacheTTL)
}
