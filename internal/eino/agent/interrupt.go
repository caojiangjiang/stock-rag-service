package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// TaskStatusAwaitingHuman 表示 Agent 已中断，等待人工输入后 Resume。
const TaskStatusAwaitingHuman TaskStatus = "awaiting_human"

// InterruptInfo 人机协同中断上下文。
type InterruptInfo struct {
	ID            string `json:"interrupt_id"`
	CheckPointID  string `json:"checkpoint_id"`
	Info          string `json:"info,omitempty"`
	Address       string `json:"address,omitempty"`
	PartialResult string `json:"partial_result,omitempty"`
}

// InterruptSession 恢复执行所需的业务元数据（与 Eino checkpoint 字节流配合）。
type InterruptSession struct {
	ConversationID  string `json:"conversation_id"`
	MessageID       string `json:"message_id"`
	UserID          string `json:"user_id"`
	CheckPointID    string `json:"checkpoint_id"`
	InterruptID     string `json:"interrupt_id"`
	CoordinatorType string `json:"coordinator_type"`
	StockCode       string `json:"stock_code,omitempty"`
	UserMessage     string `json:"user_message,omitempty"`
	InterruptInfo   string `json:"interrupt_info,omitempty"`
}

// AwaitingHumanError 表示执行成功暂停、等待人工，不应视为失败重试。
type AwaitingHumanError struct {
	Interrupt *InterruptInfo
}

func (e *AwaitingHumanError) Error() string {
	if e == nil || e.Interrupt == nil {
		return "awaiting human input"
	}
	return fmt.Sprintf("awaiting human input: %s", e.Interrupt.Info)
}

// AsAwaitingHuman 从 error 中提取中断信息。
func AsAwaitingHuman(err error) (*AwaitingHumanError, bool) {
	var target *AwaitingHumanError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// ADKProcessResult ProcessADKIterator 的处理结果。
type ADKProcessResult struct {
	Content   string
	Interrupt *InterruptInfo
}

// EnsureCheckPointID 为任务绑定 checkpoint；优先使用 conversation_id 便于跨请求恢复。
func EnsureCheckPointID(taskState *TaskState) string {
	if taskState == nil {
		return ""
	}
	if taskState.CheckPointID != "" {
		return taskState.CheckPointID
	}
	if taskState.ConversationID != "" {
		taskState.CheckPointID = taskState.ConversationID
	} else {
		taskState.GenerateCheckPointID()
	}
	return taskState.CheckPointID
}

// SaveInterruptSession 持久化 HITL 恢复所需的业务元数据。
func SaveInterruptSession(
	ctx context.Context,
	store InterruptSessionStore,
	coordType CoordinatorType,
	taskState *TaskState,
	interrupt *InterruptInfo,
) {
	if store == nil || interrupt == nil || taskState == nil {
		return
	}
	_ = store.Save(ctx, &InterruptSession{
		ConversationID:  taskState.ConversationID,
		MessageID:       taskState.MessageID,
		UserID:          taskState.UserID,
		CheckPointID:    interrupt.CheckPointID,
		InterruptID:     interrupt.ID,
		CoordinatorType: string(coordType),
		StockCode:       taskState.StockCode,
		UserMessage:     taskState.UserMessage,
		InterruptInfo:   interrupt.Info,
	})
}

// RunADKWithCheckpoint 通过 ADK Runner 执行 Agent，支持 checkpoint 与 HITL 中断。
func RunADKWithCheckpoint(
	ctx context.Context,
	rt *CoordinatorRuntime,
	taskState *TaskState,
	agent adk.Agent,
	messages []adk.Message,
	checkPointStore adk.CheckPointStore,
	sessionStore InterruptSessionStore,
	coordType CoordinatorType,
	enableStreaming bool,
) (*ADKProcessResult, error) {
	checkPointID := EnsureCheckPointID(taskState)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: enableStreaming,
		CheckPointStore: checkPointStore,
	})
	iter := runner.Run(ctx, messages, adk.WithCheckPointID(checkPointID))
	processResult, err := rt.ProcessADKIterator(ctx, taskState, iter)
	if err != nil {
		return processResult, err
	}
	if processResult != nil && processResult.Interrupt != nil {
		SaveInterruptSession(ctx, sessionStore, coordType, taskState, processResult.Interrupt)
		return processResult, &AwaitingHumanError{Interrupt: processResult.Interrupt}
	}
	return processResult, nil
}

// ResumeADKWithCheckpoint 从 HITL 中断点恢复 ADK Runner 执行。
func ResumeADKWithCheckpoint(
	ctx context.Context,
	rt *CoordinatorRuntime,
	taskState *TaskState,
	agent adk.Agent,
	checkPointStore adk.CheckPointStore,
	sessionStore InterruptSessionStore,
	coordType CoordinatorType,
	interruptID string,
	resumeData any,
	enableStreaming bool,
) (*ADKProcessResult, error) {
	checkPointID := EnsureCheckPointID(taskState)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: enableStreaming,
		CheckPointStore: checkPointStore,
	})
	params := &adk.ResumeParams{
		Targets: map[string]any{
			interruptID: resumeData,
		},
	}
	iter, err := runner.ResumeWithParams(ctx, checkPointID, params)
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("恢复执行失败: %v", err))
		return nil, err
	}
	taskState.UpdateStatus(TaskStatusRunning)
	processResult, err := rt.ProcessADKIterator(ctx, taskState, iter)
	if err != nil {
		return processResult, err
	}
	if processResult != nil && processResult.Interrupt != nil {
		SaveInterruptSession(ctx, sessionStore, coordType, taskState, processResult.Interrupt)
		return processResult, &AwaitingHumanError{Interrupt: processResult.Interrupt}
	}
	if sessionStore != nil {
		_ = sessionStore.Delete(ctx, checkPointID)
	}
	return processResult, nil
}

// UserMessagesFromTask 从 TaskState 构造 ADK 用户消息。
func UserMessagesFromTask(taskState *TaskState) []adk.Message {
	userContent := taskState.UserMessage
	if taskState.StockCode != "" {
		userContent += fmt.Sprintf("\n\n股票代码: %s", taskState.StockCode)
	}
	return []adk.Message{{
		Role:    schema.User,
		Content: userContent,
	}}
}

// MarshalInterruptSession JSON 序列化会话元数据。
func MarshalInterruptSession(s *InterruptSession) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil session")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
