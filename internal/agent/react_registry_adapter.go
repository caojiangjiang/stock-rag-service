package agent

import (
	"context"
	"encoding/json"
	"fmt"

	einotools "stock_rag/internal/eino/tools"
)

// ToolsFromRegistry 将 Eino ToolRegistry 中的工具适配为 ReAct 可用的 Tool 列表。
func ToolsFromRegistry(registry *einotools.ToolRegistry) ([]Tool, error) {
	if registry == nil {
		return nil, fmt.Errorf("tool registry is nil")
	}
	names := registry.List()
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		info, err := registry.GetInfo(name)
		if err != nil {
			return nil, err
		}
		tools = append(tools, &registryToolAdapter{
			registry: registry,
			name:     info.Name,
			desc:     info.Description,
			schema:   info.Schema,
		})
	}
	return tools, nil
}

type registryToolAdapter struct {
	registry *einotools.ToolRegistry
	name     string
	desc     string
	schema   string
}

func (t *registryToolAdapter) Name() string {
	return t.name
}

func (t *registryToolAdapter) Description() string {
	return t.desc
}

func (t *registryToolAdapter) Parameters() []ToolParameter {
	if t.schema == "" {
		return nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(t.schema), &raw); err != nil {
		return nil
	}
	props, _ := raw["properties"].(map[string]interface{})
	if props == nil {
		return nil
	}
	requiredSet := make(map[string]bool)
	if reqList, ok := raw["required"].([]interface{}); ok {
		for _, r := range reqList {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}
	params := make([]ToolParameter, 0, len(props))
	for name, def := range props {
		defMap, _ := def.(map[string]interface{})
		paramType := "string"
		if t, ok := defMap["type"].(string); ok && t != "" {
			paramType = t
		}
		desc := ""
		if d, ok := defMap["description"].(string); ok {
			desc = d
		}
		params = append(params, ToolParameter{
			Name:        name,
			Type:        paramType,
			Description: desc,
			Required:    requiredSet[name],
		})
	}
	return params
}

func (t *registryToolAdapter) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	result, err := t.registry.Invoke(ctx, t.name, args)
	if err != nil {
		return map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		}, err
	}
	return parseToolResult(result), nil
}

func parseToolResult(result string) interface{} {
	var parsed interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err == nil {
		return parsed
	}
	return result
}
