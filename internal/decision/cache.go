package decision

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

func parseBriefCacheTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DECISION_BRIEF_TTL"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("THEME_SNAPSHOT_TTL"))
	}
	if raw == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 30*time.Second {
		return 5 * time.Minute
	}
	return d
}

func (s *Service) briefCacheGet(ctx context.Context, userID string) (*DailyBrief, time.Time, bool) {
	if s.briefStore == nil || !s.briefStore.Enabled() || userID == "" {
		return nil, time.Time{}, false
	}
	var brief DailyBrief
	at, ok, err := s.briefStore.Get(ctx, userID, &brief)
	if err != nil {
		log.Printf("decision brief redis get: %v", err)
		return nil, time.Time{}, false
	}
	if !ok {
		return nil, time.Time{}, false
	}
	return &brief, at, true
}

func (s *Service) briefCacheSet(ctx context.Context, userID string, brief *DailyBrief) {
	if s.briefStore == nil || !s.briefStore.Enabled() || userID == "" || brief == nil {
		return
	}
	if err := s.briefStore.Set(ctx, userID, brief, s.briefTTL); err != nil {
		log.Printf("decision brief redis set: %v", err)
	}
}

func (s *Service) invalidateBriefCache(ctx context.Context, userID string) {
	if s.briefStore == nil || !s.briefStore.Enabled() {
		return
	}
	if userID == "" {
		return
	}
	if err := s.briefStore.Delete(ctx, userID); err != nil {
		log.Printf("decision brief redis del: %v", err)
	}
}

func annotateBriefCached(brief *DailyBrief, cached bool, cachedAt time.Time) *DailyBrief {
	if brief == nil {
		return nil
	}
	out := *brief
	out.Cached = cached
	out.CacheAgeSec = int(time.Since(cachedAt).Seconds())
	if out.CacheAgeSec < 0 {
		out.CacheAgeSec = 0
	}
	if cached {
		note := "简报来自 Redis 缓存，重启服务不会重复请求东财"
		if out.DataTrust.Note != "" {
			out.DataTrust.Note = note + "；" + out.DataTrust.Note
		} else {
			out.DataTrust.Note = note
		}
	}
	return &out
}

// InvalidateUserCache 持仓变更后可调用，使简报缓存失效。
func (s *Service) InvalidateUserCache(userID string) {
	s.invalidateBriefCache(context.Background(), userID)
}
