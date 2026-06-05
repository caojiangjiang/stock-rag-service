package decision

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// HistoryStore 每日决策快照持久化（P1 跟踪基础）。
type HistoryStore struct {
	mu   sync.Mutex
	root string
}

func NewHistoryStore(root string) *HistoryStore {
	if root == "" {
		root = "data/decision_history"
	}
	return &HistoryStore{root: root}
}

func (s *HistoryStore) Record(ctx context.Context, record DailyRecord) error {
	_ = ctx
	if record.UserID == "" || record.Date == "" {
		return fmt.Errorf("invalid record")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.root, sanitizeID(record.UserID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, record.Date+".json")
	data, err := yaml.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *HistoryStore) List(ctx context.Context, userID string) ([]DailyRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.root, sanitizeID(userID))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []DailyRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var rec DailyRecord
		if err := yaml.Unmarshal(data, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (s *HistoryStore) ExportCSV(ctx context.Context, userID string, w io.Writer) error {
	records, err := s.List(ctx, userID)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"date", "total_value", "total_cost", "unrealized_pnl_pct",
		"theme_id", "theme_name", "theme_market", "theme_state", "theme_avg_change_pct", "theme_exposure_pct",
	})
	for _, rec := range records {
		if len(rec.ThemeSnapshots) == 0 {
			_ = cw.Write([]string{
				rec.Date,
				fmt.Sprintf("%.2f", rec.TotalValue),
				fmt.Sprintf("%.2f", rec.TotalCost),
				fmt.Sprintf("%.2f", rec.UnrealizedPct),
				"", "", "", "", "", "",
			})
			continue
		}
		for _, th := range rec.ThemeSnapshots {
			_ = cw.Write([]string{
				rec.Date,
				fmt.Sprintf("%.2f", rec.TotalValue),
				fmt.Sprintf("%.2f", rec.TotalCost),
				fmt.Sprintf("%.2f", rec.UnrealizedPct),
				th.ThemeID,
				th.ThemeName,
				th.Market,
				th.State,
				fmt.Sprintf("%.2f", th.AvgChangePct),
				fmt.Sprintf("%.2f", th.ExposurePct),
			})
		}
	}
	cw.Flush()
	return cw.Error()
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "anonymous"
	}
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(id)
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}
