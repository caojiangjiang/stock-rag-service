package adapter

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

var (
	functionCallBlockRe = regexp.MustCompile(`(?s)<\|FunctionCallBegin\|>.*?<\|FunctionCallEnd\|>`)
)

type doubaoFunctionCall struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

// normalizeModelMessage 将豆包等模型以文本输出的 function call 转为 schema.ToolCalls。
func normalizeModelMessage(msg *schema.Message) *schema.Message {
	if msg == nil {
		return nil
	}
	if len(msg.ToolCalls) > 0 {
		msg.Content = stripFunctionCallBlocks(msg.Content)
		return msg
	}

	toolCalls, cleaned := extractDoubaoFunctionCalls(msg.Content)
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		msg.Content = strings.TrimSpace(cleaned)
	}
	return msg
}

func stripFunctionCallBlocks(content string) string {
	return strings.TrimSpace(functionCallBlockRe.ReplaceAllString(content, ""))
}

func extractDoubaoFunctionCalls(content string) ([]schema.ToolCall, string) {
	cleaned := content
	matches := functionCallBlockRe.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil, cleaned
	}

	var toolCalls []schema.ToolCall
	for _, block := range matches {
		inner := strings.TrimPrefix(block, "<|FunctionCallBegin|>")
		inner = strings.TrimSuffix(inner, "<|FunctionCallEnd|>")
		inner = strings.TrimSpace(inner)
		if inner == "" {
			continue
		}

		var calls []doubaoFunctionCall
		if err := json.Unmarshal([]byte(inner), &calls); err != nil {
			var single doubaoFunctionCall
			if err2 := json.Unmarshal([]byte(inner), &single); err2 != nil {
				continue
			}
			calls = []doubaoFunctionCall{single}
		}

		for _, call := range calls {
			if call.Name == "" {
				continue
			}
			argsJSON, err := json.Marshal(call.Parameters)
			if err != nil {
				continue
			}
			toolCalls = append(toolCalls, schema.ToolCall{
				ID:   uuid.NewString(),
				Type: "function",
				Function: schema.FunctionCall{
					Name:      call.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	cleaned = stripFunctionCallBlocks(content)
	return toolCalls, cleaned
}
