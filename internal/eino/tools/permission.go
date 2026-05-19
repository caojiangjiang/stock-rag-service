package tools

import (
	"fmt"
	"sync"
)

type ToolPermission struct {
	AllowedProfiles []string
	DenyProfiles    []string
	MaxCallsPerHour int
	RequireAuth     bool
}

type ToolPermissionRegistry struct {
	permissions map[string]*ToolPermission
	mu          sync.RWMutex
	callCounts  map[string]map[string]int
}

func NewToolPermissionRegistry() *ToolPermissionRegistry {
	return &ToolPermissionRegistry{
		permissions: make(map[string]*ToolPermission),
		callCounts: make(map[string]map[string]int),
	}
}

func (r *ToolPermissionRegistry) RegisterPermission(toolName string, perm *ToolPermission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissions[toolName] = perm
}

func (r *ToolPermissionRegistry) IsAllowed(toolName, profile string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	perm, ok := r.permissions[toolName]
	if !ok {
		return false
	}

	for _, deny := range perm.DenyProfiles {
		if deny == profile || deny == "*" {
			return false
		}
	}

	if len(perm.AllowedProfiles) == 0 {
		return true
	}

	for _, allowed := range perm.AllowedProfiles {
		if allowed == profile || allowed == "*" {
			return true
		}
	}

	return false
}

func (r *ToolPermissionRegistry) RecordCall(toolName, profile string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.callCounts[toolName]; !ok {
		r.callCounts[toolName] = make(map[string]int)
	}
	r.callCounts[toolName][profile]++
}

func (r *ToolPermissionRegistry) GetCallCount(toolName, profile string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if counts, ok := r.callCounts[toolName]; ok {
		return counts[profile]
	}
	return 0
}

func (r *ToolPermissionRegistry) CheckRateLimit(toolName, profile string) error {
	r.mu.RLock()
	perm, ok := r.permissions[toolName]
	if !ok {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()

	if perm.MaxCallsPerHour <= 0 {
		return nil
	}

	count := r.GetCallCount(toolName, profile)
	if count >= perm.MaxCallsPerHour {
		return fmt.Errorf("工具 %s 被 profile %s 超过速率限制: %d/小时",
			toolName, profile, perm.MaxCallsPerHour)
	}

	return nil
}

func (r *ToolPermissionRegistry) ValidateCall(toolName, profile string) error {
	if !r.IsAllowed(toolName, profile) {
		return fmt.Errorf("profile %s 没有权限调用工具 %s", profile, toolName)
	}

	if err := r.CheckRateLimit(toolName, profile); err != nil {
		return err
	}

	r.RecordCall(toolName, profile)
	return nil
}

type ToolCallRecord struct {
	ToolName    string
	Profile     string
	RequestID   string
	Args        map[string]interface{}
	Result      string
	DurationMs  int64
	Success     bool
	Error       string
}

type ToolAuditLog struct {
	records    []ToolCallRecord
	mu         sync.Mutex
	maxRecords int
}

func NewToolAuditLog(maxRecords int) *ToolAuditLog {
	if maxRecords <= 0 {
		maxRecords = 10000
	}
	return &ToolAuditLog{
		records:    make([]ToolCallRecord, 0, maxRecords),
		maxRecords: maxRecords,
	}
}

func (l *ToolAuditLog) Log(record ToolCallRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.records = append(l.records, record)

	if len(l.records) > l.maxRecords {
		l.records = l.records[len(l.records)-l.maxRecords:]
	}
}

func (l *ToolAuditLog) GetRecords() []ToolCallRecord {
	l.mu.Lock()
	defer l.mu.Unlock()

	result := make([]ToolCallRecord, len(l.records))
	copy(result, l.records)
	return result
}

func (l *ToolAuditLog) GetRecordsByTool(toolName string) []ToolCallRecord {
	l.mu.Lock()
	defer l.mu.Unlock()

	var result []ToolCallRecord
	for _, record := range l.records {
		if record.ToolName == toolName {
			result = append(result, record)
		}
	}
	return result
}

func (l *ToolAuditLog) GetRecordsByProfile(profile string) []ToolCallRecord {
	l.mu.Lock()
	defer l.mu.Unlock()

	var result []ToolCallRecord
	for _, record := range l.records {
		if record.Profile == profile {
			result = append(result, record)
		}
	}
	return result
}

var (
	DefaultPermissionRegistry = NewToolPermissionRegistry()
	DefaultAuditLog          = NewToolAuditLog(10000)
)

func RegisterDefaultPermissions() {
	DefaultPermissionRegistry.RegisterPermission("retrieve_evidence", &ToolPermission{
		AllowedProfiles: []string{"evidence_collector", "analyst_writer", "*"},
		DenyProfiles:    []string{},
		MaxCallsPerHour: 1000,
		RequireAuth:     false,
	})

	DefaultPermissionRegistry.RegisterPermission("extract_metrics", &ToolPermission{
		AllowedProfiles: []string{"metric_extractor", "analyst_writer", "*"},
		DenyProfiles:    []string{},
		MaxCallsPerHour: 1000,
		RequireAuth:     false,
	})

	DefaultPermissionRegistry.RegisterPermission("generate_report", &ToolPermission{
		AllowedProfiles: []string{"analyst_writer", "*"},
		DenyProfiles:    []string{},
		MaxCallsPerHour: 500,
		RequireAuth:     false,
	})
}