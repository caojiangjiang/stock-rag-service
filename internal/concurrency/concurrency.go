package concurrency

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

var (
	// ErrQueueFull 队列已满错误
	ErrQueueFull = errors.New("queue is full")
	// ErrQueueClosed 队列已关闭错误
	ErrQueueClosed = errors.New("queue is closed")
	// ErrWaitTimeout 等待超时错误
	ErrWaitTimeout = errors.New("wait timeout")
)

// Response 表示一个LLM响应
type Response struct {
	Result string
	Error  error
}

// Request 表示一个LLM请求
type Request struct {
	ID         string
	Question   string // 用户问题/请求内容
	TaskType   string // 任务类型：agent, rag, stream 等
	Messages   []*schema.Message
	Priority   int
	CreatedAt  time.Time
	Timeout    time.Duration
	Ctx        context.Context
	ResponseCh chan Response
	OnChunk    func(string) error // 流式回调函数
}

// QueueManager 队列管理器
type QueueManager struct {
	// 等待队列
	pending   []*Request // 等待中的请求列表
	pendingMu sync.Mutex

	// 配置
	maxQueueSize   int
	maxConcurrency int
	maxWaitTime    time.Duration

	// 状态
	activeCount int
	closed      bool
	mu          sync.Mutex
	once        sync.Once

	// 事件触发
	dispatchCh chan struct{} // 用于触发 dispatch 的 channel
	doneCh     chan struct{} // 用于通知停止的 channel

	// 统计指标
	submittedTotal    int64 // 提交总数
	rejectedTotal     int64 // 拒绝总数（队列满）
	canceledTotal     int64 // 取消总数
	timeoutTotal      int64 // 超时总数
	completedTotal    int64 // 完成总数
	totalWaitMs       int64 // 总等待时长（毫秒）
	waitTimeoutTotal  int64 // 等待超时被丢弃总数
	networkErrorTotal int64 // 网络错误总数
	modelErrorTotal   int64 // 模型错误总数

	// 处理函数
	handler func(*Request) (string, error)
}

// NewQueueManager 创建队列管理器
func NewQueueManager(maxQueueSize, maxConcurrency int) *QueueManager {
	return NewQueueManagerWithMaxWait(maxQueueSize, maxConcurrency, 0)
}

// NewQueueManagerWithMaxWait 创建队列管理器，支持最大等待时长
func NewQueueManagerWithMaxWait(maxQueueSize, maxConcurrency int, maxWaitTime time.Duration) *QueueManager {
	return &QueueManager{
		pending:        make([]*Request, 0, maxQueueSize),
		maxQueueSize:   maxQueueSize,
		maxConcurrency: maxConcurrency,
		maxWaitTime:    maxWaitTime,
		dispatchCh:     make(chan struct{}, 1), // 缓冲 1，避免阻塞
		doneCh:         make(chan struct{}),
	}
}

// Submit 提交请求到队列
func (qm *QueueManager) Submit(req *Request) error {
	qm.mu.Lock()
	if qm.closed {
		qm.mu.Unlock()
		return ErrQueueClosed
	}
	qm.mu.Unlock()

	qm.pendingMu.Lock()
	if len(qm.pending) >= qm.maxQueueSize {
		qm.pendingMu.Unlock()
		qm.mu.Lock()
		qm.rejectedTotal++
		qm.mu.Unlock()
		return ErrQueueFull
	}

	req.CreatedAt = time.Now()
	qm.pending = append(qm.pending, req)
	qm.pendingMu.Unlock()

	qm.mu.Lock()
	qm.submittedTotal++
	qm.mu.Unlock()

	// 触发 dispatch：有新请求入队
	qm.triggerDispatch()

	return nil
}

// triggerDispatch 触发 dispatch
func (qm *QueueManager) triggerDispatch() {
	select {
	case qm.dispatchCh <- struct{}{}:
	default:
		// 如果 channel 已满，说明已经有待处理的 dispatch 事件，不需要重复触发
	}
}

// Start 启动队列管理器
func (qm *QueueManager) Start(handler func(*Request) (string, error)) {
	qm.handler = handler

	// 启动调度器
	go qm.scheduler()
	// 启动清理器
	go qm.cleanup()
}

// scheduler 调度器：事件驱动 dispatch
func (qm *QueueManager) scheduler() {
	for {
		select {
		case <-qm.doneCh:
			return
		case <-qm.dispatchCh:
			// 事件触发：尝试 dispatch
			qm.tryDispatch()
		}
	}
}

// tryDispatch 尝试 dispatch 请求
func (qm *QueueManager) tryDispatch() {
	for {
		qm.mu.Lock()
		// 检查是否有空闲槽位
		if qm.activeCount >= qm.maxConcurrency {
			qm.mu.Unlock()
			return // 槽位已满，停止 dispatch
		}
		qm.mu.Unlock()

		qm.pendingMu.Lock()
		// 检查队列是否为空
		if len(qm.pending) == 0 {
			qm.pendingMu.Unlock()
			return // 队列为空，停止 dispatch
		}

		// 获取队列中的第一个请求
		req := qm.pending[0]

		// 检查请求是否已超时
		if qm.maxWaitTime > 0 && time.Since(req.CreatedAt) > qm.maxWaitTime {
			// 请求已超时，移除并拒绝
			qm.pending = qm.pending[1:]
			qm.pendingMu.Unlock()

			req.ResponseCh <- Response{Result: "", Error: ErrWaitTimeout}
			close(req.ResponseCh)

			waitMs := time.Since(req.CreatedAt).Milliseconds()
			qm.mu.Lock()
			qm.waitTimeoutTotal++
			qm.totalWaitMs += waitMs
			qm.mu.Unlock()

			// 继续尝试 dispatch 下一个请求
			continue
		}

		// 检查请求是否已取消
		select {
		case <-req.Ctx.Done():
			// 请求已取消，移除
			qm.pending = qm.pending[1:]
			qm.pendingMu.Unlock()

			req.ResponseCh <- Response{Result: "", Error: errors.New("request canceled")}
			close(req.ResponseCh)

			waitMs := time.Since(req.CreatedAt).Milliseconds()
			qm.mu.Lock()
			qm.canceledTotal++
			qm.totalWaitMs += waitMs
			qm.mu.Unlock()

			// 继续尝试 dispatch 下一个请求
			continue
		default:
		}

		// 请求有效，从队列中移除
		qm.pending = qm.pending[1:]
		qm.pendingMu.Unlock()

		// 增加活跃计数
		qm.mu.Lock()
		qm.activeCount++
		qm.mu.Unlock()

		// 启动 worker 处理请求
		go qm.processRequest(req)
	}
}

// ErrorType 错误类型
type ErrorType string

const (
	// ErrorTypeNetwork 网络错误
	ErrorTypeNetwork ErrorType = "network"
	// ErrorTypeModel 模型错误
	ErrorTypeModel ErrorType = "model"
	// ErrorTypeBusiness 业务错误
	ErrorTypeBusiness ErrorType = "business"
	// ErrorTypeTimeout 超时错误
	ErrorTypeTimeout ErrorType = "timeout"
	// ErrorTypeCanceled 取消错误
	ErrorTypeCanceled ErrorType = "canceled"
	// ErrorTypeUnknown 未知错误
	ErrorTypeUnknown ErrorType = "unknown"
)

// EnhancedError 增强的错误结构
type EnhancedError struct {
	OriginalError error
	ErrorType     ErrorType
	Retryable     bool
	Message       string
}

func (e *EnhancedError) Error() string {
	return e.Message
}

// classifyError 分类错误
func classifyError(err error) *EnhancedError {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, context.Canceled):
		return &EnhancedError{
			OriginalError: err,
			ErrorType:     ErrorTypeCanceled,
			Retryable:     false,
			Message:       "Request canceled",
		}
	case errors.Is(err, context.DeadlineExceeded):
		return &EnhancedError{
			OriginalError: err,
			ErrorType:     ErrorTypeTimeout,
			Retryable:     true,
			Message:       "Request timeout",
		}
	case strings.Contains(err.Error(), "network") || strings.Contains(err.Error(), "connection"):
		return &EnhancedError{
			OriginalError: err,
			ErrorType:     ErrorTypeNetwork,
			Retryable:     true,
			Message:       "Network error: " + err.Error(),
		}
	case strings.Contains(err.Error(), "model") || strings.Contains(err.Error(), "LLM"):
		return &EnhancedError{
			OriginalError: err,
			ErrorType:     ErrorTypeModel,
			Retryable:     true,
			Message:       "Model error: " + err.Error(),
		}
	default:
		return &EnhancedError{
			OriginalError: err,
			ErrorType:     ErrorTypeUnknown,
			Retryable:     false,
			Message:       "Unknown error: " + err.Error(),
		}
	}
}

// processRequest 处理请求（支持实时中断和增强错误处理）
func (qm *QueueManager) processRequest(req *Request) {
	// 计算等待时长
	waitMs := time.Since(req.CreatedAt).Milliseconds()
	resultCh := make(chan Response, 1)

	defer func() {
		// 减少活跃计数
		qm.mu.Lock()
		qm.activeCount--
		qm.mu.Unlock()
		close(req.ResponseCh)

		// 触发 dispatch：请求执行完成，槽位空出来了
		qm.triggerDispatch()
	}()

	// 在 goroutine 中执行 handler，支持实时中断
	go func() {
		// 处理请求，最多重试3次
		var result string
		var err error
		maxRetries := 3

		for i := 0; i < maxRetries; i++ {
			// 创建一个 channel 来接收 handler 执行结果
			handlerResultCh := make(chan struct {
				result string
				err    error
			}, 1)

			// 在子 goroutine 中执行 handler
			go func() {
				r, e := qm.handler(req)
				handlerResultCh <- struct {
					result string
					err    error
				}{r, e}
			}()

			// 同时监听 context 取消和 handler 完成
			select {
			case <-req.Ctx.Done():
				err = errors.New("request canceled")
				goto finish
			case handlerResp := <-handlerResultCh:
				result, err = handlerResp.result, handlerResp.err
			}

			// 分类错误
			enhancedErr := classifyError(err)
			if enhancedErr == nil {
				// 成功
				goto finish
			}

			// 检查是否可重试
			if !enhancedErr.Retryable || i == maxRetries-1 {
				err = enhancedErr
				goto finish
			}

			// 重试前短暂延迟
			time.Sleep(time.Duration(i*100) * time.Millisecond)
		}

	finish:
		resultCh <- Response{Result: result, Error: err}
	}()

	// 同时监听 context 取消和 handler 完成
	select {
	case <-req.Ctx.Done():
		err := errors.New("request canceled")
		req.ResponseCh <- Response{Result: "", Error: err}
		qm.mu.Lock()
		qm.canceledTotal++
		qm.completedTotal++
		qm.totalWaitMs += waitMs
		qm.mu.Unlock()
		return
	case resp := <-resultCh:
		req.ResponseCh <- resp
		qm.mu.Lock()
		if resp.Error != nil {
			if errors.Is(resp.Error, context.DeadlineExceeded) {
				qm.timeoutTotal++
			} else if enhancedErr, ok := resp.Error.(*EnhancedError); ok {
				switch enhancedErr.ErrorType {
				case ErrorTypeNetwork:
					qm.networkErrorTotal++
				case ErrorTypeModel:
					qm.modelErrorTotal++
				case ErrorTypeTimeout:
					qm.timeoutTotal++
				case ErrorTypeCanceled:
					qm.canceledTotal++
				}
			}
		}
		qm.completedTotal++
		qm.totalWaitMs += waitMs
		qm.mu.Unlock()
	}
}

// cleanup 清理器：定期扫描 pending，移除超时请求
func (qm *QueueManager) cleanup() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-qm.doneCh:
			return
		case <-ticker.C:
			qm.pendingMu.Lock()
			now := time.Now()
			var newPending []*Request
			hasTimeout := false

			for _, req := range qm.pending {
				if qm.maxWaitTime > 0 && now.Sub(req.CreatedAt) > qm.maxWaitTime {
					req.ResponseCh <- Response{Result: "", Error: ErrWaitTimeout}
					close(req.ResponseCh)

					waitMs := time.Since(req.CreatedAt).Milliseconds()
					qm.mu.Lock()
					qm.waitTimeoutTotal++
					qm.totalWaitMs += waitMs
					qm.mu.Unlock()

					hasTimeout = true
				} else {
					newPending = append(newPending, req)
				}
			}

			qm.pending = newPending
			qm.pendingMu.Unlock()

			// 如果有请求被清理，触发 dispatch
			if hasTimeout {
				qm.triggerDispatch()
			}
		}
	}
}

// Stop 停止队列管理器
func (qm *QueueManager) Stop() {
	qm.once.Do(func() {
		qm.mu.Lock()
		qm.closed = true
		qm.mu.Unlock()

		close(qm.doneCh)
	})
}

// GetStats 获取队列状态
func (qm *QueueManager) GetStats() map[string]interface{} {
	qm.mu.Lock()
	qm.pendingMu.Lock()
	defer qm.mu.Unlock()
	defer qm.pendingMu.Unlock()

	avgWaitMs := int64(0)
	if qm.completedTotal > 0 {
		avgWaitMs = qm.totalWaitMs / qm.completedTotal
	}

	return map[string]interface{}{
		"queue_size":         len(qm.pending),
		"max_queue_size":     qm.maxQueueSize,
		"active_count":       qm.activeCount,
		"max_concurrency":    qm.maxConcurrency,
		"max_wait_time_ms":   qm.maxWaitTime.Milliseconds(),
		"submitted_total":    qm.submittedTotal,
		"rejected_total":     qm.rejectedTotal,
		"canceled_total":     qm.canceledTotal,
		"timeout_total":      qm.timeoutTotal,
		"wait_timeout_total": qm.waitTimeoutTotal,
		"completed_total":    qm.completedTotal,
		"avg_wait_ms":        avgWaitMs,
	}
}
