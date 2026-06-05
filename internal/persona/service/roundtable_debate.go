package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	einoagent "stock_rag/internal/eino/agent"
	"stock_rag/internal/observability"
	"stock_rag/internal/persona/model"
)

type personaDebateGenerator struct {
	service   *ConfigPersonaService
	personas  []*model.PersonaProfile
	question  string
	timeRange string
	useRAG    bool
	requestID string

	// 每位角色最后一轮发言（用于组装 PersonaAnswer）
	finalArguments map[string]string
}

func (g *personaDebateGenerator) GenerateArgument(
	ctx context.Context,
	profile *einoagent.AgentProfile,
	taskState *einoagent.TaskState,
	peerArguments []einoagent.DebatePeerArgument,
	round int,
) (string, error) {
	personaID := profile.Name
	persona := g.personaByID(personaID)
	if persona == nil {
		return "", fmt.Errorf("persona %s not found", personaID)
	}

	promptBuilder := NewPersonaPromptBuilder(persona)
	userMsg := buildDebateUserMessage(g.question, round, peerArguments)

	var text string
	var err error
	if g.useRAG && g.service.queryService != nil {
		useRAG := true
		chatReq := &model.PersonaChatRequest{
			PersonaID: personaID,
			Message:   userMsg,
			TimeRange: g.timeRange,
			UseRAG:    &useRAG,
		}
		ans, chatErr := g.service.Chat(ctx, chatReq)
		if chatErr != nil {
			err = chatErr
		} else if ans != nil {
			text = strings.TrimSpace(ans.Summary)
			if text == "" {
				text = strings.TrimSpace(ans.Stance)
			}
		}
	} else {
		systemPrompt := promptBuilder.BuildPersonaSystemPrompt() +
			"\n你正在参加投资圆桌辩论：请坚持本角色投资框架，用中文发言；可支持或反驳其他参与者，但不要编造具体财报数据；不要给出买卖建议。"
		text, err = generatePersonaDirectAnswer(ctx, systemPrompt, userMsg)
	}

	if err != nil || strings.TrimSpace(text) == "" {
		observability.L().WarnCtx(ctx, "Persona debate argument fallback",
			"feature", "persona",
			"mode", "roundtable_debate",
			"persona_id", personaID,
			"round", round,
			"error", fmt.Sprintf("%v", err))
		text = debateFallbackArgument(persona, g.question, round, peerArguments)
	}

	g.finalArguments[personaID] = text
	return text, nil
}

func (g *personaDebateGenerator) personaByID(id string) *model.PersonaProfile {
	for _, p := range g.personas {
		if p.PersonaID == id {
			return p
		}
	}
	return nil
}

func buildDebateUserMessage(question string, round int, peers []einoagent.DebatePeerArgument) string {
	var b strings.Builder
	b.WriteString("圆桌议题：")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\n\n")
	if round <= 1 {
		b.WriteString("这是第 1 轮发言：请亮明你对该议题的核心立场与 2～3 条关键论点。")
	} else {
		b.WriteString(fmt.Sprintf("这是第 %d 轮发言：请回应其他参与者观点，可补充论据、支持或反驳。", round))
	}
	if len(peers) > 0 {
		b.WriteString("\n\n其他参与者已发言：\n")
		for _, p := range peers {
			role := p.Role
			if role == "" {
				role = p.Name
			}
			b.WriteString(fmt.Sprintf("- %s：%s\n", role, truncateForDebate(p.Argument, 500)))
		}
	}
	b.WriteString("\n请直接输出观点正文（约 150～350 字），不要寒暄，不要 JSON。")
	return b.String()
}

func truncateForDebate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func debateFallbackArgument(persona *model.PersonaProfile, question string, round int, peers []einoagent.DebatePeerArgument) string {
	if round <= 1 {
		return fmt.Sprintf("作为%s，针对「%s」我倾向于结合%s框架审慎评估，需关注宏观与估值匹配。",
			persona.Name, question, strings.Join(persona.StyleTags, "/"))
	}
	if len(peers) > 0 {
		return fmt.Sprintf("作为%s，我注意到其他观点强调了不同风险收益权衡；我仍坚持%s，但会关注分歧中的估值与流动性因素。",
			persona.Name, persona.InvestmentBelief)
	}
	return fmt.Sprintf("作为%s，该议题仍需更多数据验证。", persona.Name)
}

func personaProfilesToAgentProfiles(personas []*model.PersonaProfile) []*einoagent.AgentProfile {
	out := make([]*einoagent.AgentProfile, len(personas))
	for i, p := range personas {
		pb := NewPersonaPromptBuilder(p)
		out[i] = &einoagent.AgentProfile{
			Name:       p.PersonaID,
			Role:       p.Name,
			RolePrompt: pb.BuildPersonaSystemPrompt(),
		}
	}
	return out
}

func personaDebateMaxRounds() int {
	v := strings.TrimSpace(os.Getenv("PERSONA_ROUNDTABLE_DEBATE_ROUNDS"))
	if v == "" {
		return 2
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 2
	}
	if n > 3 {
		return 3
	}
	return n
}

func roundtableUseRAG() bool {
	v := strings.TrimSpace(os.Getenv("PERSONA_ROUNDTABLE_USE_RAG"))
	if v == "" {
		return false
	}
	return !strings.EqualFold(v, "false") && v != "0"
}

func newDefaultCoordinatorFactory() *einoagent.CoordinatorFactory {
	registry := einoagent.NewProfileRegistry()
	builder := einoagent.NewAgentBuilder(nil)
	return einoagent.NewCoordinatorFactory(registry, builder, nil, nil)
}

func (s *ConfigPersonaService) runRoundtableDebate(
	ctx context.Context,
	req *model.RoundtableRequest,
	validProfiles []*model.PersonaProfile,
	requestID string,
) ([]model.PersonaAnswer, string, int, error) {
	factory := s.coordinatorFactory
	if factory == nil {
		factory = newDefaultCoordinatorFactory()
	}

	coord, err := factory.Create(einoagent.CoordinatorTypeDebate)
	if err != nil {
		return nil, "", 0, fmt.Errorf("create multi-agent coordinator: %w", err)
	}
	multi, ok := coord.(*einoagent.MultiAgentCoordinator)
	if !ok {
		return nil, "", 0, fmt.Errorf("coordinator is not MultiAgentCoordinator")
	}

	agentProfiles := personaProfilesToAgentProfiles(validProfiles)
	multi.SetAgentProfiles(agentProfiles)
	multi.SetMaxRounds(personaDebateMaxRounds())

	gen := &personaDebateGenerator{
		service:        s,
		personas:       validProfiles,
		question:       req.Question,
		timeRange:      req.TimeRange,
		useRAG:         roundtableUseRAG(),
		requestID:      requestID,
		finalArguments: make(map[string]string, len(validProfiles)),
	}
	multi.SetArgumentGenerator(gen)

	taskState := einoagent.NewTaskState("persona-roundtable", requestID, "", req.Question)
	taskState.TimeRange = req.TimeRange

	summary, execErr := multi.Execute(ctx, taskState)
	if execErr != nil {
		return nil, summary, len(taskState.StepTraces), execErr
	}

	answers := make([]model.PersonaAnswer, len(validProfiles))
	for i, p := range validProfiles {
		arg := gen.finalArguments[p.PersonaID]
		if arg == "" {
			arg = debateFallbackArgument(p, req.Question, 1, nil)
		}
		answers[i] = s.personaAnswerFromDebateArgument(p, req.Question, arg, requestID)
	}
	return answers, summary, len(taskState.StepTraces), nil
}

// roundtableFanoutFallback 在 MultiAgentCoordinator 失败时降级为并行独立 Chat。
func (s *ConfigPersonaService) roundtableFanoutFallback(
	ctx context.Context,
	req *model.RoundtableRequest,
	validProfiles []*model.PersonaProfile,
	requestID string,
) ([]model.PersonaAnswer, []string) {
	answers := make([]model.PersonaAnswer, len(validProfiles))
	failedPersonas := []string{}

	chatTask := func(ctx context.Context, idx int, profile *model.PersonaProfile) (*model.PersonaAnswer, error) {
		chatReq := &model.PersonaChatRequest{
			PersonaID: profile.PersonaID,
			Message:   req.Question,
			TimeRange: req.TimeRange,
		}
		return s.Chat(ctx, chatReq)
	}

	results := einoagent.Fanout(
		ctx,
		validProfiles,
		chatTask,
		einoagent.WithTimeout(60*time.Second),
		einoagent.WithTracing("persona.service.roundtable.fanout_fallback"),
		einoagent.WithMaxConcurrency(len(validProfiles)),
	)

	for _, result := range results {
		if result.Error != nil {
			failedPersonas = append(failedPersonas, validProfiles[result.Index].PersonaID)
			promptBuilder := NewPersonaPromptBuilder(validProfiles[result.Index])
			answers[result.Index] = model.PersonaAnswer{
				PersonaID:   validProfiles[result.Index].PersonaID,
				PersonaName: validProfiles[result.Index].Name,
				Summary:     "该角色暂时无法完成分析，请稍后重试。",
				Stance:      promptBuilder.BuildStance(req.Question, ""),
				Thesis:      promptBuilder.BuildThesis(""),
				Risks:       promptBuilder.BuildRisks(),
				Citations:   []model.EvidenceItem{},
				Disclaimer:  promptBuilder.BuildDisclaimer(),
				RequestID:   fmt.Sprintf("roundtable-%s-%d", validProfiles[result.Index].PersonaID, time.Now().UnixNano()),
			}
		} else {
			answers[result.Index] = *result.Result
		}
	}
	return answers, failedPersonas
}

func (s *ConfigPersonaService) personaAnswerFromDebateArgument(
	profile *model.PersonaProfile,
	question, argument, requestID string,
) model.PersonaAnswer {
	promptBuilder := NewPersonaPromptBuilder(profile)
	summary := strings.TrimSpace(argument)
	answer := model.PersonaAnswer{
		PersonaID:   profile.PersonaID,
		PersonaName: profile.Name,
		Summary:     summary,
		Stance:      promptBuilder.BuildStance(question, argument),
		Thesis:      promptBuilder.BuildThesis(argument),
		Risks:       promptBuilder.BuildRisks(),
		Citations:   []model.EvidenceItem{},
		Disclaimer:  promptBuilder.BuildDisclaimer(),
		RequestID:   fmt.Sprintf("%s-%s-%d", requestID, profile.PersonaID, time.Now().UnixNano()),
	}
	answer.Thesis = dedupeThesisAgainstSummary(summary, answer.Thesis)
	if len(answer.Thesis) == 0 {
		answer.Thesis = pickFrameworkThesis(profile.DecisionFramework, 3)
	}
	return answer
}
