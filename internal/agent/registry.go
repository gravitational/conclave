package agent

import (
	"context"
	"sync"
)

// RunningAgent tracks a running agent instance
type RunningAgent struct {
	ID       int
	Name     string
	Provider string
	Model    string
	Cancel   context.CancelFunc
	Killed   bool
}

// Registry tracks all running agents for control purposes
type Registry struct {
	mu     sync.RWMutex
	agents map[int]*RunningAgent
}

// NewRegistry creates a new agent registry
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[int]*RunningAgent),
	}
}

// Register adds an agent to the registry
func (r *Registry) Register(id int, name, provider, model string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[id] = &RunningAgent{
		ID:       id,
		Name:     name,
		Provider: provider,
		Model:    model,
		Cancel:   cancel,
	}
}

// Unregister removes an agent from the registry
func (r *Registry) Unregister(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, id)
}

// Kill cancels an agent's context, terminating its execution
func (r *Registry) Kill(id int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agent, ok := r.agents[id]; ok {
		if !agent.Killed {
			agent.Cancel()
			agent.Killed = true
			return true
		}
	}
	return false
}

// KillAll cancels all running agents
func (r *Registry) KillAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, agent := range r.agents {
		if !agent.Killed {
			agent.Cancel()
			agent.Killed = true
			count++
		}
	}
	return count
}

// Get returns an agent by ID
func (r *Registry) Get(id int) (*RunningAgent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[id]
	return agent, ok
}

// List returns all registered agents
func (r *Registry) List() []*RunningAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*RunningAgent, 0, len(r.agents))
	for _, agent := range r.agents {
		result = append(result, agent)
	}
	return result
}

// Clear removes all agents from the registry
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = make(map[int]*RunningAgent)
}
