package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	einoTool "github.com/cloudwego/eino/components/tool"
)

// ToolApprovalInfo 展示给人工审批的中断信息。
type ToolApprovalInfo struct {
	ToolName string                 `json:"tool_name"`
	Args     map[string]interface{} `json:"args,omitempty"`
	Message  string                 `json:"message"`
}

type toolApprovalState struct {
	ToolName string `json:"tool_name"`
}

var hitlApprovalTools = map[string]struct{}{
	"retrieve_evidence":    {},
	"generate_report":      {},
	"web_search":           {},
	"fetch_webpage":        {},
	"search_announcements": {},
}

// HITLEnabled 是否启用工具级人工审批（默认开启，设置 ENABLE_TOOL_HITL=false 关闭）。
func HITLEnabled() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_TOOL_HITL"))
	if v == "" {
		return true
	}
	return !strings.EqualFold(v, "false") && v != "0"
}

// RequiresHITLApproval 判断工具是否属于高风险/外部调用，需要人工确认。
func RequiresHITLApproval(toolName string) bool {
	_, ok := hitlApprovalTools[FormatToolName(toolName)]
	return ok
}

// GateToolInvocation 在真正执行工具前触发 Eino HITL 中断；恢复后根据 resume_data 决定是否继续。
func GateToolInvocation(ctx context.Context, toolName string, args map[string]interface{}) error {
	if !HITLEnabled() || !RequiresHITLApproval(toolName) {
		return nil
	}

	name := FormatToolName(toolName)
	wasInterrupted, _, _ := einoTool.GetInterruptState[toolApprovalState](ctx)
	if !wasInterrupted {
		info := ToolApprovalInfo{
			ToolName: name,
			Args:     args,
			Message:  fmt.Sprintf("即将执行工具「%s」，请确认是否继续（resume_data 传 approved:true 或「确认」）", name),
		}
		return einoTool.StatefulInterrupt(ctx, info, toolApprovalState{ToolName: name})
	}

	isTarget, hasData, data := einoTool.GetResumeContext[interface{}](ctx)
	if !isTarget {
		return einoTool.Interrupt(ctx, nil)
	}
	if hasData && !parseToolApproval(data) {
		return fmt.Errorf("用户已拒绝执行工具 %s", name)
	}
	return nil
}

func parseToolApproval(data interface{}) bool {
	if data == nil {
		return true
	}
	switch v := data.(type) {
	case bool:
		return v
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		return s != "deny" && s != "拒绝" && s != "false" && s != "no" && s != "cancel"
	case map[string]interface{}:
		if a, ok := v["approved"].(bool); ok {
			return a
		}
		if s, ok := v["approved"].(string); ok {
			return parseToolApproval(s)
		}
	case json.Number:
		return v.String() != "0"
	}
	return true
}
