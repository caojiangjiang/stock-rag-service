package agent

import (
	"context"
	"fmt"

	"stock_rag/internal/eino/adapter"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type DeepCoordinator struct {
	*BaseCoordinator
	supervisorProfile *AgentProfile
	maxIterations     int
	agentBuilder      *AgentBuilder
}

func NewDeepCoordinator(profileRegistry *ProfileRegistry, agentBuilder *AgentBuilder) *DeepCoordinator {
	base := NewBaseCoordinator("deep", profileRegistry, agentBuilder)
	return &DeepCoordinator{
		BaseCoordinator:   base,
		supervisorProfile: TaskPlannerProfile,
		maxIterations:     3,
		agentBuilder:      agentBuilder,
	}
}

func (c *DeepCoordinator) SetMaxIterations(max int) {
	c.maxIterations = max
}

func (c *DeepCoordinator) SetAgentBuilder(builder *AgentBuilder) {
	c.agentBuilder = builder
}

func (c *DeepCoordinator) Execute(ctx context.Context, taskState *TaskState) (string, error) {
	profiles := c.GetAgentProfiles()
	if len(profiles) == 0 {
		return "没有配置任何 Agent", nil
	}

	taskState.UpdateStatus(TaskStatusRunning)

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	// 调用链路: Eino ADK -> EinoModelAdapter -> llm.GetLLMClient() -> provider
	modelAdapter := adapter.NewEinoModelAdapter()

	deepAgent, err := deep.New(ctx, &deep.Config{
		Name:        "deep_coordinator",
		Description: "深度思考协调器，用于多轮反思和迭代优化分析",
		ChatModel:   modelAdapter,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: c.buildTools(profiles),
			},
		},
		MaxIteration: c.maxIterations,
		Instruction: `你是一个深度思考专家。你的工作方式：

1. 初始分析：对问题进行初步分析
2. 深度反思：检查初始分析是否有遗漏或错误
3. 证据补充：根据反思结果补充必要证据
4. 综合分析：整合所有分析形成最终结论

请按照以下步骤进行深度思考，并在每轮反思中质疑和改进之前的结论。`,
	})
	if err != nil {
		taskState.UpdateStatus(TaskStatusFailed)
		taskState.AddError(fmt.Sprintf("创建 Deep Agent 失败: %v", err))
		return "", err
	}

	input := &adk.AgentInput{
		Messages: []adk.Message{
			{
				Role:    schema.User,
				Content: taskState.UserMessage,
			},
		},
		EnableStreaming: false,
	}

	iterator := deepAgent.Run(ctx, input)

	var finalResult string
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			taskState.UpdateStatus(TaskStatusFailed)
			taskState.AddError(fmt.Sprintf("Deep 执行错误: %v", event.Err))
			return "", event.Err
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			finalResult += msg.Content
		}
	}

	taskState.UpdateStatus(TaskStatusCompleted)
	taskState.Summary = finalResult

	return finalResult, nil
}

func (c *DeepCoordinator) buildTools(profiles []*AgentProfile) []tool.BaseTool {
	if c.agentBuilder == nil {
		return nil
	}

	var allTools []tool.BaseTool
	for _, profile := range profiles {
		tools, err := c.agentBuilder.BuildToolsForProfile(profile)
		if err != nil || len(tools) == 0 {
			continue
		}
		allTools = append(allTools, tools...)
	}

	return allTools
}
