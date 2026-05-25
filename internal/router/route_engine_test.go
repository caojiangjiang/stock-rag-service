package router

import "testing"

func TestShouldApplyStickiness(t *testing.T) {
	baseInput := func(msg string, last RouteMode) *RouteInput {
		return &RouteInput{
			CurrentMessage: msg,
			LastRouteMode:  last,
			RecentMessages: []MessageContext{
				{Role: "user", Content: "分析茅台财报", RouteMode: ModeAgent},
			},
		}
	}

	tests := []struct {
		name string
		in   *RouteInput
		want bool
	}{
		{
			name: "no last mode",
			in:   baseInput("市盈率呢", ""),
			want: false,
		},
		{
			name: "follow-up short question",
			in:   baseInput("市盈率呢", ModeAgent),
			want: true,
		},
		{
			name: "mode switch blocks stickiness",
			in:   baseInput("然后帮我分析一下财报", ModeChat),
			want: false,
		},
		{
			name: "capability question blocks stickiness",
			in:   baseInput("你能帮我做什么", ModeAgent),
			want: false,
		},
		{
			name: "new topic without context",
			in: &RouteInput{
				CurrentMessage: "市盈率呢",
				LastRouteMode:  ModeAgent,
			},
			want: false,
		},
		{
			name: "continuation phrase",
			in:   baseInput("接着说", ModeAgent),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldApplyStickiness(tt.in); got != tt.want {
				t.Fatalf("shouldApplyStickiness() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasModeSwitchIntent(t *testing.T) {
	if !hasModeSwitchIntent("对比一下茅台和五粮液") {
		t.Fatal("expected mode switch for comparison")
	}
	if hasModeSwitchIntent("市盈率呢") {
		t.Fatal("follow-up should not be treated as mode switch")
	}
}
