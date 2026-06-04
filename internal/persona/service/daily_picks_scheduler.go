package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// DailyPicksScheduler 每日选股定时调度器
type DailyPicksScheduler struct {
	service *DailyPicksService
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewDailyPicksScheduler 创建定时调度器
func NewDailyPicksScheduler(service *DailyPicksService) *DailyPicksScheduler {
	return &DailyPicksScheduler{
		service: service,
		stopCh:  make(chan struct{}),
	}
}

// Start 启动调度器
func (s *DailyPicksScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	// 启动时检查是否需要补跑今天的榜单
	go s.checkAndRunOnStart(ctx)

	// 启动定时任务
	s.wg.Add(1)
	go s.runDailyTask(ctx)

	log.Println("[DailyPicksScheduler] Started")
	return nil
}

// Stop 停止调度器
func (s *DailyPicksScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	s.wg.Wait()
	log.Println("[DailyPicksScheduler] Stopped")
}

// checkAndRunOnStart 启动时检查（不自动生成，仅初始化缓存）
func (s *DailyPicksScheduler) checkAndRunOnStart(ctx context.Context) {
	log.Printf("[DailyPicksScheduler] Initialized - picks will be generated on first manual request or scheduled time")
}

// runDailyTask 运行每日定时任务
func (s *DailyPicksScheduler) runDailyTask(ctx context.Context) {
	defer s.wg.Done()

	// 计算距离明天 08:00 的时间
	now := time.Now()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 8, 0, 0, 0, now.Location())
	firstRun := tomorrow.Sub(now)

	log.Printf("[DailyPicksScheduler] First daily run scheduled at %s (in %v)", tomorrow.Format("2006-01-02 15:04:05"), firstRun)

	// 第一次等待
	select {
	case <-s.stopCh:
		return
	case <-time.After(firstRun):
		s.runOnce(ctx)
	}

	// 之后每天 08:00 运行
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

// runOnce 执行一次选股
func (s *DailyPicksScheduler) runOnce(ctx context.Context) {
	log.Printf("[DailyPicksScheduler] Running daily picks generation at %s", time.Now().Format("2006-01-02 15:04:05"))

	start := time.Now()
	if err := s.service.GenerateAllPicks(ctx); err != nil {
		log.Printf("[DailyPicksScheduler] Failed to generate picks: %v", err)
	} else {
		log.Printf("[DailyPicksScheduler] Successfully generated picks in %v", time.Since(start))
	}
}

// RunNow 手动立即执行
func (s *DailyPicksScheduler) RunNow(ctx context.Context) {
	go s.runOnce(ctx)
}
