package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"stock_rag/internal/eino/adapter"
	"stock_rag/internal/eino/tools"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type AgentBuilder struct {
	toolRegistry *tools.ToolRegistry
}

func NewAgentBuilder(toolRegistry *tools.ToolRegistry) *AgentBuilder {
	return &AgentBuilder{
		toolRegistry: toolRegistry,
	}
}

func (b *AgentBuilder) Build(ctx context.Context, profile *AgentProfile) (adk.Agent, error) {
	instruction := b.buildInstruction(profile)

	einoTools, err := b.getToolsForProfile(profile)
	if err != nil {
		return nil, fmt.Errorf("获取工具失败: %w", err)
	}

	// 使用 EinoModelAdapter 包装 llm.GetLLMClient()
	// 调用链路: Eino ADK -> EinoModelAdapter -> llm.GetLLMClient() -> provider
	modelAdapter := adapter.NewEinoModelAdapter()

	if len(einoTools) > 0 {
		return b.buildChatModelAgentWithTools(ctx, profile, instruction, einoTools, modelAdapter)
	}

	return b.buildChatModelAgent(ctx, profile, instruction, modelAdapter)
}

func (b *AgentBuilder) BuildToolsForProfile(profile *AgentProfile) ([]tool.BaseTool, error) {
	return b.getToolsForProfile(profile)
}

func (b *AgentBuilder) buildInstruction(profile *AgentProfile) string {
	instruction := fmt.Sprintf("%s\n\n%s", profile.Role, profile.RolePrompt)
	if len(profile.Constraints) > 0 {
		instruction += "\n\n约束：\n"
		for i, constraint := range profile.Constraints {
			instruction += fmt.Sprintf("%d. %s\n", i+1, constraint)
		}
	}
	return instruction
}

func (b *AgentBuilder) getToolsForProfile(profile *AgentProfile) ([]tool.BaseTool, error) {
	if b.toolRegistry == nil {
		return nil, nil
	}

	if len(profile.AvailableTools) == 0 {
		return nil, nil
	}

	einoTools := make([]tool.BaseTool, 0, len(profile.AvailableTools))
	for _, toolName := range profile.AvailableTools {
		toolInfo, err := b.toolRegistry.GetInfo(toolName)
		if err != nil {
			continue
		}

		if toolInfo.IsTyped {
			typedTool, err := b.toolRegistry.GetTypedTool(toolName)
			if err != nil {
				continue
			}
			einoTool := &typedToolAdapter{
				name:      typedTool.Name(),
				desc:      typedTool.Description(),
				schema:    typedTool.GetSchema(),
				typedTool: typedTool,
				registry:  b.toolRegistry,
			}
			einoTools = append(einoTools, einoTool)
		} else {
			einoTool := &registryToolAdapter{
				name:     toolInfo.Name,
				desc:     toolInfo.Description,
				tool:     toolInfo.Instance,
				registry: b.toolRegistry,
			}
			einoTools = append(einoTools, einoTool)
		}
	}

	return einoTools, nil
}

func (b *AgentBuilder) buildChatModelAgent(ctx context.Context, profile *AgentProfile, instruction string, modelAdapter *adapter.EinoModelAdapter) (adk.Agent, error) {
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        profile.Name,
		Description: profile.Role,
		Model:       modelAdapter,
		Instruction: instruction,
	})
}

func (b *AgentBuilder) buildChatModelAgentWithTools(ctx context.Context, profile *AgentProfile, instruction string, einoTools []tool.BaseTool, modelAdapter *adapter.EinoModelAdapter) (adk.Agent, error) {
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        profile.Name,
		Description: profile.Role,
		Model:       modelAdapter,
		Instruction: instruction,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: einoTools,
			},
		},
	})
}

type registryToolAdapter struct {
	name     string
	desc     string
	tool     tools.Tool
	registry *tools.ToolRegistry
}

func (t *registryToolAdapter) Name(ctx context.Context) (string, error) {
	return t.name, nil
}

func (t *registryToolAdapter) Desc(ctx context.Context) (string, error) {
	return t.desc, nil
}

func (t *registryToolAdapter) Invoke(ctx context.Context, query string) (string, error) {
	args := parseTypedArgs(query)
	if t.registry != nil {
		return t.registry.Invoke(ctx, t.name, args)
	}
	return t.tool.Run(ctx, args)
}

func (t *registryToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: t.name,
		Desc: t.desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {Type: "string", Desc: "查询参数", Required: true},
		}),
	}, nil
}

type typedToolAdapter struct {
	name      string
	desc      string
	schema    *schema.ToolInfo
	typedTool tools.TypedToolBase
	registry  *tools.ToolRegistry
}

func (t *typedToolAdapter) Name(ctx context.Context) (string, error) {
	return t.name, nil
}

func (t *typedToolAdapter) Desc(ctx context.Context) (string, error) {
	return t.desc, nil
}

func (t *typedToolAdapter) Invoke(ctx context.Context, query string) (string, error) {
	args := parseTypedArgs(query)
	if t.registry != nil {
		return t.registry.Invoke(ctx, t.name, args)
	}
	return t.typedTool.Invoke(ctx, args)
}

func (t *typedToolAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	if t.schema != nil {
		return t.schema, nil
	}
	return &schema.ToolInfo{
		Name: t.name,
		Desc: t.desc,
	}, nil
}

func parseTypedArgs(query string) map[string]interface{} {
	args := make(map[string]interface{})
	if query == "" {
		return args
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(query), &parsed); err == nil {
		if m, ok := parsed.(map[string]interface{}); ok {
			args = m
		} else {
			args["query"] = query
		}
	} else {
		args["query"] = query
	}
	return args
}
