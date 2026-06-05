package agent

import (
	"os"
	"strings"

	einoagent "stock_rag/internal/eino/agent"
)

// ExplicitCoordinatorFromEnv 读取 COORDINATOR_TYPE 环境变量（显式覆盖自动选择）。
func ExplicitCoordinatorFromEnv() einoagent.CoordinatorType {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("COORDINATOR_TYPE"))) {
	case "plan":
		return einoagent.CoordinatorTypePlan
	case "pipeline":
		return einoagent.CoordinatorTypePipeline
	case "workflow":
		return einoagent.CoordinatorTypeWorkflow
	case "multi_agent":
		return einoagent.CoordinatorTypeMultiAgent
	case "peer":
		return einoagent.CoordinatorTypePeer
	case "debate":
		return einoagent.CoordinatorTypeDebate
	case "committee":
		return einoagent.CoordinatorTypeCommittee
	case "deep":
		return einoagent.CoordinatorTypeDeep
	case "supervisor":
		return einoagent.CoordinatorTypeSupervisor
	case "":
		return ""
	default:
		return einoagent.CoordinatorType(os.Getenv("COORDINATOR_TYPE"))
	}
}
