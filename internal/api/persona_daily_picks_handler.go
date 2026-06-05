package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"stock_rag/internal/persona/model"
	"stock_rag/internal/persona/service"
)

// DailyPicksHandler 每日选股 API Handler
type DailyPicksHandler struct {
	service *service.DailyPicksService
}

// NewDailyPicksHandler 创建每日选股 Handler
func NewDailyPicksHandler(service *service.DailyPicksService) *DailyPicksHandler {
	return &DailyPicksHandler{
		service: service,
	}
}

// GetDailyPicks 处理 GET /api/personas/daily-picks
func (h *DailyPicksHandler) GetDailyPicks(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	personaID := r.URL.Query().Get("persona_id")
	date := r.URL.Query().Get("date")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 如果指定了 persona_id，返回单个结果
	if personaID != "" {
		picks, err := h.service.GetDailyPicks(ctx, personaID, date)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		response := buildResponse(picks)
		json.NewEncoder(w).Encode(response)
		return
	}

	// 否则返回所有结果
	cache, err := h.service.GetAllDailyPicks(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 转换为响应格式
	responses := make([]*model.DailyPicksResponse, 0, len(cache.Picks))
	for _, picks := range cache.Picks {
		responses = append(responses, buildResponse(picks))
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"date":       cache.Date,
		"updated_at": cache.UpdatedAt,
		"count":      len(responses),
		"picks":      responses,
	})
}

// RunManualTrigger 处理 POST /api/personas/daily-picks/run
func (h *DailyPicksHandler) RunManualTrigger(w http.ResponseWriter, r *http.Request) {
	// 设置 CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result := h.service.RunManualTrigger(ctx)
	json.NewEncoder(w).Encode(result)
}

// buildResponse 构建 API 响应
func buildResponse(picks *model.DailyPicks) *model.DailyPicksResponse {
	if picks == nil {
		return nil
	}

	return &model.DailyPicksResponse{
		Persona: model.PersonaInfo{
			ID:          picks.PersonaID,
			Name:        picks.PersonaName,
			Market:      picks.Market,
			Description: getPersonaDescription(picks.PersonaID),
		},
		Picks:       picks.Stocks,
		GeneratedAt: picks.GeneratedAt,
		Disclaimer:  "基于公开资料生成，投资有风险，请结合自身情况独立决策。",
		Market:      picks.Market,
		TotalCount:  len(picks.Stocks),
	}
}

// getPersonaDescription 获取 Persona 描述
func getPersonaDescription(personaID string) string {
	descriptions := map[string]string{
		"us_growth_tech":         "美股成长科技风格，关注 AI、半导体、云服务等高成长领域",
		"us_value_recovery":      "美股价值恢复风格，关注低估值、金融、能源等价值板块",
		"us_momentum":            "美股动量趋势风格，关注强势股和趋势延续",
		"cn_dividend_defensive":  "A 股红利防御风格，关注高股息、公用事业等防御板块",
		"cn_growth":              "A 股成长风格，关注国产替代、新能源等成长领域",
		"cn_momentum":           "A 股动量风格，关注题材催化和热点轮动",
	}
	if desc, ok := descriptions[personaID]; ok {
		return desc
	}
	return ""
}
