package einomodel

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	arkmodel "github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"

	appmodel "stock_rag/internal/model"
	"stock_rag/internal/pkgctx"
)

// ChatConfig 描述后续接入 Eino ChatModel 所需的核心配置。
type ChatConfig struct {
	Provider  string
	ModelName string
	ModelEnv  string
	APIKeyEnv string
	BaseURL   string
}

// ChatModel 是当前项目的聊天模型封装。
//
// 当 ARK_API_KEY 和 ARK_MODEL 可用时，走真实 Ark 模型；
// 否则回退到 skeleton 响应，保证本地开发和单测稳定。
type ChatModel struct {
	Config   ChatConfig
	ark      *arkmodel.ChatModel
	fallback SkeletonChatModel
}

// SkeletonChatModel 是没有真实 provider 配置时的占位实现。
type SkeletonChatModel struct {
	Config ChatConfig
}

// DefaultChatConfig 从 ModelConfig 创建 ChatConfig。
func DefaultChatConfig(modelConfig pkgctx.ModelConfig) ChatConfig {
	return ChatConfig{
		ModelName: modelConfig.Name,
		Provider:  modelConfig.Provider,
		ModelEnv:  modelConfig.ModelEnv,
		APIKeyEnv: modelConfig.APIKeyEnv,
		BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
	}
}

// DefaultChatConfigWithDefaults 返回默认的 ChatConfig。
func DefaultChatConfigWithDefaults() ChatConfig {
	return ChatConfig{
		Provider:  "ark",
		ModelEnv:  "ARK_MODEL",
		APIKeyEnv: "ARK_API_KEY",
		BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
	}
}

// NewChatModel 创建带 Ark provider 能力的聊天模型。
func NewChatModel(ctx context.Context, cfg ChatConfig) (*ChatModel, error) {
	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" && cfg.ModelEnv != "" {
		modelName = strings.TrimSpace(os.Getenv(cfg.ModelEnv))
	}

	apiKey := ""
	if cfg.APIKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	}
	fallback := NewSkeletonChatModel(cfg)

	client := &ChatModel{
		Config:   cfg,
		fallback: fallback,
	}

	if apiKey == "" || modelName == "" {
		return client, nil
	}

	timeout := 2 * time.Minute
	temperature := float32(0.2)
	maxTokens := 1024

	arkChatModel, err := arkmodel.NewChatModel(ctx, &arkmodel.ChatModelConfig{
		APIKey:      apiKey,
		Model:       modelName,
		BaseURL:     cfg.BaseURL,
		Timeout:     &timeout,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	})
	if err != nil {
		return nil, err
	}

	client.Config.ModelName = modelName
	client.ark = arkChatModel
	return client, nil
}

// Enabled 表示当前是否启用了真实 Ark 模型。
func (m *ChatModel) Enabled() bool {
	return m != nil && m.ark != nil
}

// Generate 生成当前阶段的占位回答。
func (m *ChatModel) Generate(ctx context.Context, req appmodel.RAGQueryRequest, messages []*schema.Message, citations []appmodel.Citation) (string, error) {
	if m != nil && m.ark != nil {
		out, err := m.ark.Generate(ctx, messages)
		if err != nil {
			return "", err
		}

		if out != nil && strings.TrimSpace(out.Content) != "" {
			return strings.TrimSpace(out.Content), nil
		}
		return "", nil
	}

	if m == nil {
		return "", fmt.Errorf("chat model is nil")
	}

	if m.ark == nil {
		answer := m.fallback.Generate(req, messages, citations)
		return answer, nil
	}

	return "", nil
}

// StreamGenerate 以流式方式生成回答，并通过回调输出增量 chunk。
func (m *ChatModel) StreamGenerate(ctx context.Context, req appmodel.RAGQueryRequest, messages []*schema.Message, citations []appmodel.Citation, onChunk func(string) error) (string, error) {
	if m != nil && m.ark != nil {
		stream, err := m.ark.Stream(ctx, messages)
		if err != nil {
			return "", err
		}
		defer stream.Close()

		var full strings.Builder
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return full.String(), err
			}
			if msg == nil {
				continue
			}

			delta := strings.TrimSpace(msg.Content)
			if delta == "" {
				continue
			}

			full.WriteString(delta)
			if onChunk != nil {
				if err := onChunk(delta); err != nil {
					return full.String(), err
				}
			}
		}

		return full.String(), nil
	}

	answer := m.fallback.Generate(req, messages, citations)
	if onChunk != nil {
		for _, chunk := range splitForStreaming(answer, 24) {
			if err := onChunk(chunk); err != nil {
				return answer, err
			}
		}
	}

	return answer, nil
}

// NewSkeletonChatModel 创建最小聊天模型占位。
func NewSkeletonChatModel(cfg ChatConfig) SkeletonChatModel {
	return SkeletonChatModel{Config: cfg}
}

// Generate 生成当前阶段的占位回答。
func (m SkeletonChatModel) Generate(req appmodel.RAGQueryRequest, messages []*schema.Message, citations []appmodel.Citation) string {
	_ = messages

	titles := make([]string, 0, 2)
	for i, citation := range citations {
		if i >= 2 {
			break
		}
		titles = append(titles, citation.Title)
	}

	sourceHint := ""
	if len(titles) > 0 {
		sourceHint = " 参考来源：" + strings.Join(titles, "；") + "。"
	}

	return fmt.Sprintf(
		"【Eino skeleton】已通过 compose 链路完成检索与 Prompt 组装；当前问题\"%s\"将在配置 %s 与 %s 后切换到真实 %s ChatModel。%s",
		strings.TrimSpace(req.Question),
		m.Config.APIKeyEnv,
		m.Config.ModelEnv,
		m.Config.Provider,
		sourceHint,
	)
}

// splitForStreaming 将字符串分割为流式输出的小块
func splitForStreaming(s string, chunkSize int) []string {
	var chunks []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}
