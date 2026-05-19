# Agent 体系迁移指南

## 概述

`agent.go` 和 `agent_modes.go` 已标记为废弃（deprecated），建议迁移到新的 Coordinator 体系或 Eino 原生 ADK 抽象。

## 体系对比

| 旧体系（废弃） | 新体系（推荐） |
|---------------|---------------|
| `Agent` struct | `Coordinator` interface |
| `Tool` interface | `ToolRegistry` + tools |
| `Runner` interface | `Coordinator.Execute()` |
| `AgentMode` | `CoordinatorType` |
| `ReactAgent` | `PlanCoordinator` |
| `PlanExecuteAgent` | `PlanCoordinator` |
| `MultiAgent` | `SupervisorCoordinator` |
| `PeerAgent` | `SupervisorCoordinator` |
| `HybridAgent` | `PipelineCoordinator` |

## 迁移步骤

### 1. 创建 Coordinator 和相关组件

```go
// 旧方式
config := AgentConfig{
    LLMClient: llmClient,
    Tools:     []Tool{tool1, tool2},
    MaxSteps:  10,
}
agent := NewReactAgent(config)

// 新方式
toolRegistry := einotools.NewToolRegistry()
toolRegistry.RegisterStandardTools(queryService)

profileRegistry := einoagent.NewProfileRegistry()

agentBuilder := einoagent.NewAgentBuilder(toolRegistry)

coordinatorFactory := einoagent.NewCoordinatorFactory(profileRegistry, agentBuilder, toolRegistry)

coordinator, _ := coordinatorFactory.Create(einoagent.CoordinatorTypePlan)
```

### 2. 执行任务

```go
// 旧方式
result, err := agent.Run(ctx, task)

// 新方式
taskState := einoagent.NewTaskState(conversationID, messageID, userID, task)
result, err := coordinator.Execute(ctx, taskState)
```

### 3. 多 Agent 协调

```go
// 旧方式
multiAgent := NewMultiAgent(config)
result, err := multiAgent.Run(ctx, task)

// 新方式
coordinator, _ := coordinatorFactory.Create(einoagent.CoordinatorTypeSupervisor)
coordinator.SetAgentProfiles([]*einoagent.AgentProfile{
    einoagent.EvidenceCollectorProfile,
    einoagent.MetricExtractorProfile,
    einoagent.AnalystWriterProfile,
})
result, err := coordinator.Execute(ctx, taskState)
```

## 推荐的 Coordinator 类型

| 场景 | 推荐 Coordinator |
|------|-----------------|
| 简单任务执行 | `CoordinatorTypePlan` |
| 多 Agent 协作 | `CoordinatorTypeSupervisor` |
| 流水线处理 | `CoordinatorTypePipeline` |

## 代码路径更新

- `internal/eino/agent/agent.go` → `internal/eino/agent/coordinator.go`
- `internal/eino/agent/agent_modes.go` → `internal/eino/agent/supervisor_coordinator.go`
- `internal/eino/agent/skill_adapters.go` → `internal/eino/tools/` 目录下的工具文件

## 保留作为 Legacy 的情况

以下情况可以继续使用旧体系（但不推荐）：

1. 需要保持向后兼容性
2. 正在进行增量迁移
3. 需要特定的自定义行为

## 完整示例

```go
func createTaskAgent(queryService *service.QueryService) service.SupervisorAgent {
    // 创建 ToolRegistry
    toolRegistry := einotools.NewToolRegistry()
    toolRegistry.RegisterStandardTools(queryService)
    
    // 创建 ProfileRegistry
    profileRegistry := einoagent.NewProfileRegistry()
    
    // 创建 AgentBuilder
    agentBuilder := einoagent.NewAgentBuilder(toolRegistry)
    
    // 创建 Coordinator
    factory := einoagent.NewCoordinatorFactory(profileRegistry, agentBuilder)
    coordinator, _ := factory.Create(einoagent.CoordinatorTypeSupervisor)
    
    // 设置子 Agent Profiles
    coordinator.SetAgentProfiles([]*einoagent.AgentProfile{
        einoagent.EvidenceCollectorProfile,
        einoagent.MetricExtractorProfile,
        einoagent.AnalystWriterProfile,
    })
    
    // 返回适配器
    return einoagent.NewCoordinatorSupervisorAdapter(coordinator)
}
```

## 废弃时间表

- **v1.0**: 标记为废弃
- **v1.1**: 停止新增功能
- **v1.2**: 计划移除（如有必要）
