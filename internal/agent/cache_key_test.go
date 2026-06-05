package agent

import "testing"

func TestBuildExactCacheKeyIgnoresRouteMode(t *testing.T) {
	key1 := buildExactCacheKey("question", "600519", "report", "latest")
	key2 := buildExactCacheKey("question", "600519", "report", "latest")
	if key1 != key2 {
		t.Fatalf("expected stable cache key, got %q vs %q", key1, key2)
	}

	key3 := buildExactCacheKey("other", "600519", "report", "latest")
	if key1 == key3 {
		t.Fatal("expected different cache keys for different messages")
	}
}

func TestIsCacheableChatResponse(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect bool
	}{
		{"empty", "", false},
		{"function_call", `<|FunctionCallBegin|>[{"name":"transfer_to_agent"}]<|FunctionCallEnd|>`, false},
		{"error_prefix", "执行失败: timeout", false},
		{"good", "这是基于持仓数据的风险分析，当前组合集中度偏高，建议关注以下三点……", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCacheableChatResponse(tc.input); got != tc.expect {
				t.Fatalf("isCacheableChatResponse(%q) = %v, want %v", tc.input, got, tc.expect)
			}
		})
	}
}
