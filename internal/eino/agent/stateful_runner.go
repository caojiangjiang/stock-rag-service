package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type CheckPointStore interface {
	SaveState(ctx context.Context, checkPointID string, state map[string]any) error
	LoadState(ctx context.Context, checkPointID string) (map[string]any, error)
	ClearState(ctx context.Context, checkPointID string) error
}

type StatefulRunner struct {
	runner          *adk.Runner
	checkPointStore CheckPointStore
	taskState       *TaskState
}

func NewStatefulRunner(agent adk.Agent, checkPointStore CheckPointStore) *StatefulRunner {
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{
		EnableStreaming: true,
		Agent:           agent,
	})

	return &StatefulRunner{
		runner:          runner,
		checkPointStore: checkPointStore,
	}
}

func (r *StatefulRunner) Run(ctx context.Context, taskState *TaskState) (string, error) {
	r.taskState = taskState
	taskState.UpdateStatus(TaskStatusRunning)

	checkPointID := taskState.GenerateCheckPointID()

	messages := []adk.Message{
		{
			Role:    schema.User,
			Content: taskState.UserMessage,
		},
	}

	iter := r.runner.Run(ctx, messages, adk.WithCheckPointID(checkPointID))

	return r.processIterator(ctx, iter, taskState, checkPointID)
}

func (r *StatefulRunner) processIterator(ctx context.Context, iter *adk.AsyncIterator[*adk.AgentEvent], taskState *TaskState, checkPointID string) (string, error) {
	var finalResult string

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			taskState.AddError(fmt.Sprintf("执行错误: %v", event.Err))
			taskState.UpdateStatus(TaskStatusFailed)
			return "", event.Err
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			taskState.UpdateStatus(TaskStatusRunning)
			taskState.CreateCheckpoint(event.Action.Interrupted.InterruptContexts[0].ID)

			if err := r.saveState(checkPointID, taskState); err != nil {
				taskState.AddError(fmt.Sprintf("保存 Checkpoint 失败: %v", err))
			}
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			if msg != nil {
				finalResult = msg.Content
				taskState.Summary = finalResult
			}
		}
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	return finalResult, nil
}

func (r *StatefulRunner) Resume(ctx context.Context, taskState *TaskState, interruptID string, resumeData any) (string, error) {
	r.taskState = taskState
	taskState.UpdateStatus(TaskStatusRunning)

	checkPointID := taskState.CheckPointID
	if checkPointID == "" {
		checkPointID = taskState.GenerateCheckPointID()
	}

	params := &adk.ResumeParams{
		Targets: map[string]any{
			interruptID: resumeData,
		},
	}

	iter, err := r.runner.ResumeWithParams(ctx, checkPointID, params)
	if err != nil {
		taskState.AddError(fmt.Sprintf("恢复执行失败: %v", err))
		taskState.UpdateStatus(TaskStatusFailed)
		return "", err
	}

	return r.processIterator(ctx, iter, taskState, checkPointID)
}

func (r *StatefulRunner) GetTaskState() *TaskState {
	return r.taskState
}

func (r *StatefulRunner) saveState(checkPointID string, taskState *TaskState) error {
	if r.checkPointStore == nil {
		return nil
	}

	stateData := taskState.ToJSON()

	return r.checkPointStore.SaveState(context.Background(), checkPointID, map[string]any{
		"task_state": stateData,
	})
}

func (r *StatefulRunner) LoadState(checkPointID string) (*TaskState, error) {
	if r.checkPointStore == nil {
		return nil, fmt.Errorf("CheckPointStore 未初始化")
	}

	stateMap, err := r.checkPointStore.LoadState(context.Background(), checkPointID)
	if err != nil {
		return nil, fmt.Errorf("加载状态失败: %w", err)
	}

	stateData, ok := stateMap["task_state"].(string)
	if !ok {
		return nil, fmt.Errorf("状态数据格式错误")
	}

	taskState := &TaskState{}
	if err := taskState.FromJSON(stateData); err != nil {
		return nil, fmt.Errorf("反序列化 TaskState 失败: %w", err)
	}

	r.taskState = taskState
	return taskState, nil
}

func (r *StatefulRunner) ClearState(checkPointID string) error {
	if r.checkPointStore == nil {
		return nil
	}
	return r.checkPointStore.ClearState(context.Background(), checkPointID)
}

type InMemoryCheckPointStore struct {
	states map[string]map[string]any
}

func NewInMemoryCheckPointStore() *InMemoryCheckPointStore {
	return &InMemoryCheckPointStore{
		states: make(map[string]map[string]any),
	}
}

func (s *InMemoryCheckPointStore) SaveState(ctx context.Context, checkPointID string, state map[string]any) error {
	s.states[checkPointID] = state
	return nil
}

func (s *InMemoryCheckPointStore) LoadState(ctx context.Context, checkPointID string) (map[string]any, error) {
	state, ok := s.states[checkPointID]
	if !ok {
		return nil, fmt.Errorf("checkpoint %s not found", checkPointID)
	}
	return state, nil
}

func (s *InMemoryCheckPointStore) ClearState(ctx context.Context, checkPointID string) error {
	delete(s.states, checkPointID)
	return nil
}
