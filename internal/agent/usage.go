package agent

import (
	"sync"
)

// Usage holds token consumption metrics for an agent run
type Usage struct {
	InputTokens      int     `json:"input_tokens" yaml:"input_tokens"`
	OutputTokens     int     `json:"output_tokens" yaml:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty" yaml:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty" yaml:"cache_write_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens" yaml:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty" yaml:"cost_usd,omitempty"`
}

// IsEmpty returns true if no usage data was recorded
func (u Usage) IsEmpty() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.TotalTokens == 0
}

// SessionUsage tracks aggregate usage across a session
type SessionUsage struct {
	mu      sync.Mutex
	ByAgent map[string]Usage // keyed by "provider/model"
	Total   Usage
}

// NewSessionUsage creates a new session usage tracker
func NewSessionUsage() *SessionUsage {
	return &SessionUsage{
		ByAgent: make(map[string]Usage),
	}
}

// Add records usage for an agent run
func (s *SessionUsage) Add(provider, model string, u Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build key
	key := provider
	if model != "" {
		key = provider + "/" + model
	}

	// Update per-agent totals
	existing := s.ByAgent[key]
	existing.InputTokens += u.InputTokens
	existing.OutputTokens += u.OutputTokens
	existing.CacheReadTokens += u.CacheReadTokens
	existing.CacheWriteTokens += u.CacheWriteTokens
	existing.TotalTokens += u.TotalTokens
	existing.CostUSD += u.CostUSD
	s.ByAgent[key] = existing

	// Update session totals
	s.Total.InputTokens += u.InputTokens
	s.Total.OutputTokens += u.OutputTokens
	s.Total.CacheReadTokens += u.CacheReadTokens
	s.Total.CacheWriteTokens += u.CacheWriteTokens
	s.Total.TotalTokens += u.TotalTokens
	s.Total.CostUSD += u.CostUSD
}

// Reset clears all accumulated usage
func (s *SessionUsage) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ByAgent = make(map[string]Usage)
	s.Total = Usage{}
}

// GetTotal returns the current session total (thread-safe copy)
func (s *SessionUsage) GetTotal() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Total
}

// GetByAgent returns a copy of the per-agent usage map
func (s *SessionUsage) GetByAgent() map[string]Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]Usage, len(s.ByAgent))
	for k, v := range s.ByAgent {
		result[k] = v
	}
	return result
}

// GlobalSession is the shared session usage tracker
var GlobalSession = NewSessionUsage()

// UsageProvider is an interface for agents that can report usage
type UsageProvider interface {
	LastUsage() Usage
}
