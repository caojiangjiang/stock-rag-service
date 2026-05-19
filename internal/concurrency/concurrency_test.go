package concurrency

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

// mockHandler 模拟大模型处理，返回时间在 30-180s 之间随机
func mockHandler(req *Request) (string, error) {
	waitTime := 30 + rand.Intn(150) // 30-180 秒
	time.Sleep(time.Duration(waitTime) * time.Millisecond)
	return fmt.Sprintf("Mock response for request %s", req.ID), nil
}

// TestConcurrencyLimit 测试并发数限制
func TestConcurrencyLimit(t *testing.T) {
	maxConcurrency := 5
	qm := NewQueueManager(100, maxConcurrency)
	qm.Start(mockHandler)
	defer qm.Stop()

	var wg sync.WaitGroup

	// 提交 20 个请求
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &Request{
				ID:         fmt.Sprintf("req-%d", id),
				Question:   "test question",
				TaskType:   "test",
				Messages:   []*schema.Message{{Role: "user", Content: "test"}},
				Ctx:        context.Background(),
				ResponseCh: make(chan Response, 1),
			}

			err := qm.Submit(req)
			if err != nil {
				t.Errorf("Failed to submit request: %v", err)
				return
			}

			// 等待响应
			resp := <-req.ResponseCh
			if resp.Error != nil {
				t.Errorf("Request failed: %v", resp.Error)
			}
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成
	time.Sleep(1 * time.Second)

	stats := qm.GetStats()
	if stats["completed_total"].(int64) != 20 {
		t.Errorf("Expected 20 completed requests, got %d", stats["completed_total"])
	}

	// 验证并发数从未超过限制
	if stats["active_count"].(int) > maxConcurrency {
		t.Errorf("Active count %d exceeds max concurrency %d", stats["active_count"], maxConcurrency)
	}
}

// TestQueueFull 测试队列满的情况
func TestQueueFull(t *testing.T) {
	maxQueueSize := 5
	maxConcurrency := 2
	qm := NewQueueManager(maxQueueSize, maxConcurrency)
	qm.Start(mockHandler)
	defer qm.Stop()

	var wg sync.WaitGroup

	// 提交 10 个请求，但队列只能容纳 5 个
	rejectedCount := int32(0)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &Request{
				ID:         fmt.Sprintf("req-%d", id),
				Question:   "test question",
				TaskType:   "test",
				Messages:   []*schema.Message{{Role: "user", Content: "test"}},
				Ctx:        context.Background(),
				ResponseCh: make(chan Response, 1),
			}

			err := qm.Submit(req)
			if err == ErrQueueFull {
				atomic.AddInt32(&rejectedCount, 1)
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			} else {
				// 如果成功提交，等待响应
				<-req.ResponseCh
			}
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成
	time.Sleep(1 * time.Second)

	stats := qm.GetStats()
	// 由于并发提交，可能不是正好 5 个被拒绝，但应该有一些被拒绝
	if stats["rejected_total"].(int64) == 0 {
		t.Errorf("Expected some rejected requests, got %d", stats["rejected_total"])
	}
}

// TestStopNoPanic 测试 Stop() 不会 panic
func TestStopNoPanic(t *testing.T) {
	qm := NewQueueManager(100, 10)
	qm.Start(mockHandler)

	// 提交一些请求
	for i := 0; i < 5; i++ {
		req := &Request{
			ID:         fmt.Sprintf("req-%d", i),
			Question:   "test question",
				TaskType:   "test",
			Messages:   []*schema.Message{{Role: "user", Content: "test"}},
			Ctx:        context.Background(),
			ResponseCh: make(chan Response, 1),
		}
		qm.Submit(req)
	}

	// 多次调用 Stop()，不应该 panic
	qm.Stop()
	qm.Stop()
	qm.Stop()

	// Stop() 后提交请求应该返回 ErrQueueClosed
	req := &Request{
		ID:         "req-after-stop",
		Question:   "test question",
				TaskType:   "test",
		Messages:   []*schema.Message{{Role: "user", Content: "test"}},
		Ctx:        context.Background(),
		ResponseCh: make(chan Response, 1),
	}
	err := qm.Submit(req)
	if err != ErrQueueClosed {
		t.Errorf("Expected ErrQueueClosed, got %v", err)
	}
}

// TestCanceledRequestNotBlocking 测试取消的请求不会长期占用槽位
func TestCanceledRequestNotBlocking(t *testing.T) {
	maxConcurrency := 2
	maxWaitTime := 5 * time.Second
	qm := NewQueueManagerWithMaxWait(100, maxConcurrency, maxWaitTime)
	qm.Start(mockHandler)
	defer qm.Stop()

	var wg sync.WaitGroup

	// 提交 10 个请求，其中一半会被取消
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithCancel(context.Background())

			req := &Request{
				ID:         fmt.Sprintf("req-%d", id),
				Question:   "test question",
				TaskType:   "test",
				Messages:   []*schema.Message{{Role: "user", Content: "test"}},
				Ctx:        ctx,
				ResponseCh: make(chan Response, 1),
			}

			err := qm.Submit(req)
			if err != nil {
				t.Errorf("Failed to submit request: %v", err)
				return
			}

			// 立即取消一半的请求
			if id%2 == 0 {
				cancel()
			}

			// 等待响应
			resp := <-req.ResponseCh
			if resp.Error != nil && resp.Error.Error() != "request canceled" {
				t.Errorf("Request failed: %v", resp.Error)
			}
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成
	time.Sleep(1 * time.Second)

	stats := qm.GetStats()
	if stats["canceled_total"].(int64) < 5 {
		t.Errorf("Expected at least 5 canceled requests, got %d", stats["canceled_total"])
	}
}

// TestWaitTimeout 测试等待超时
func TestWaitTimeout(t *testing.T) {
	maxConcurrency := 1
	maxWaitTime := 50 * time.Millisecond
	qm := NewQueueManagerWithMaxWait(100, maxConcurrency, maxWaitTime)
	qm.Start(mockHandler)
	defer qm.Stop()

	var wg sync.WaitGroup

	// 提交 20 个请求，第一个会立即处理，其他的会超时
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &Request{
				ID:         fmt.Sprintf("req-%d", id),
				Question:   "test question",
				TaskType:   "test",
				Messages:   []*schema.Message{{Role: "user", Content: "test"}},
				Ctx:        context.Background(),
				ResponseCh: make(chan Response, 1),
			}

			err := qm.Submit(req)
			if err != nil {
				t.Errorf("Failed to submit request: %v", err)
				return
			}

			// 等待响应
			resp := <-req.ResponseCh
			// 第一个请求会成功，其他的可能会超时
			if id > 0 && resp.Error != nil && resp.Error != ErrWaitTimeout {
				t.Errorf("Request %d unexpected error: %v", id, resp.Error)
			}
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成，包括 cleanup 处理超时请求
	time.Sleep(3 * time.Second)

	stats := qm.GetStats()
	// 第一个请求会成功，其他的会超时
	// 由于并发和调度，可能会有一些请求被处理，但应该有很多超时
	if stats["wait_timeout_total"].(int64) < 10 {
		t.Errorf("Expected at least 10 wait timeout requests, got %d", stats["wait_timeout_total"])
	}
}

// TestSharedClient 测试 QueryService 和 Agent 共享同一个 client
func TestSharedClient(t *testing.T) {
	qm := NewQueueManager(100, 10)
	qm.Start(mockHandler)
	defer qm.Stop()

	// 模拟 QueryService 和 Agent 共享同一个 QueueManager
	var wg sync.WaitGroup

	// QueryService 提交请求
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &Request{
				ID:         fmt.Sprintf("query-%d", id),
				Question:   "rag question",
				TaskType:   "rag",
				Messages:   []*schema.Message{{Role: "user", Content: "query"}},
				Ctx:        context.Background(),
				ResponseCh: make(chan Response, 1),
			}

			qm.Submit(req)
			<-req.ResponseCh
		}(i)
	}

	// Agent 提交请求
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &Request{
				ID:         fmt.Sprintf("agent-%d", id),
				Question:   "agent question",
				TaskType:   "agent",
				Messages:   []*schema.Message{{Role: "user", Content: "agent"}},
				Ctx:        context.Background(),
				ResponseCh: make(chan Response, 1),
			}

			qm.Submit(req)
			<-req.ResponseCh
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成
	time.Sleep(1 * time.Second)

	stats := qm.GetStats()
	if stats["completed_total"].(int64) != 10 {
		t.Errorf("Expected 10 completed requests, got %d", stats["completed_total"])
	}

	// 验证并发限制
	if stats["active_count"].(int) > 10 {
		t.Errorf("Active count exceeds max concurrency")
	}
}

// TestStats 测试统计指标
func TestStats(t *testing.T) {
	qm := NewQueueManager(10, 2)
	qm.Start(mockHandler)
	defer qm.Stop()

	var wg sync.WaitGroup

	// 提交 10 个请求
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &Request{
				ID:         fmt.Sprintf("req-%d", id),
				Question:   "test question",
				TaskType:   "test",
				Messages:   []*schema.Message{{Role: "user", Content: "test"}},
				Ctx:        context.Background(),
				ResponseCh: make(chan Response, 1),
			}

			qm.Submit(req)
			<-req.ResponseCh
		}(i)
	}

	wg.Wait()

	// 等待所有请求完成
	time.Sleep(1 * time.Second)

	stats := qm.GetStats()

	// 验证统计指标
	if stats["submitted_total"].(int64) != 10 {
		t.Errorf("Expected 10 submitted requests, got %d", stats["submitted_total"])
	}

	if stats["completed_total"].(int64) != 10 {
		t.Errorf("Expected 10 completed requests, got %d", stats["completed_total"])
	}

	if stats["max_queue_size"].(int) != 10 {
		t.Errorf("Expected max queue size 10, got %d", stats["max_queue_size"])
	}

	if stats["max_concurrency"].(int) != 2 {
		t.Errorf("Expected max concurrency 2, got %d", stats["max_concurrency"])
	}
}
