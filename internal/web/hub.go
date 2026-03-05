package web

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// MessageType identifies the type of WebSocket message
type MessageType string

const (
	MsgAgentStatus    MessageType = "agent_status"
	MsgAgentLog       MessageType = "agent_log"
	MsgPhaseChange    MessageType = "phase_change"
	MsgPhaseComplete  MessageType = "phase_complete"
	MsgError          MessageType = "error"
	MsgCommand        MessageType = "command"
	MsgCommandAck     MessageType = "command_ack"
	MsgPipelineStart  MessageType = "pipeline_start"
	MsgFindingUpdate  MessageType = "finding_update"
	MsgSessionUsage   MessageType = "session_usage"
)

// CommandData holds a control command from the frontend
type CommandData struct {
	Action  string `json:"action"`  // kill, kill_all
	AgentID int    `json:"agentId"` // for kill action
}

// CommandAckData acknowledges a command
type CommandAckData struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// AgentKiller is a function that kills an agent by ID
type AgentKiller func(id int) bool

// AllAgentKiller is a function that kills all agents
type AllAgentKiller func() int

// Message is a WebSocket message
type Message struct {
	Type      MessageType `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// UsageData holds token usage metrics for web display
type UsageData struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUsd,omitempty"`
}

// AgentStatusData holds agent status info
type AgentStatusData struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model,omitempty"`
	State     string     `json:"state"` // waiting, running, done, error
	Activity  string     `json:"activity"`
	Lines     int        `json:"lines"`
	StartTime time.Time  `json:"startTime"`
	EndTime   *time.Time `json:"endTime,omitempty"`
	Error     string     `json:"error,omitempty"`
	Usage     *UsageData `json:"usage,omitempty"`
}

// SessionUsageData holds aggregate session usage for broadcasting
type SessionUsageData struct {
	ByAgent map[string]UsageData `json:"byAgent"`
	Total   UsageData            `json:"total"`
}

// AgentLogData holds a log line from an agent
type AgentLogData struct {
	AgentID int    `json:"agentId"`
	Line    string `json:"line"`
}

// PhaseData holds phase change info
type PhaseData struct {
	Phase       string `json:"phase"` // plan, assess, debate, synthesize
	Description string `json:"description"`
}

// FindingState holds the current state of a finding in pipeline mode
type FindingState struct {
	FindingIdx   int              `json:"findingIdx"`
	Label        string           `json:"label"`
	CurrentPhase string           `json:"currentPhase"` // "steelman", "critique", "judge", "done"
	Verdict      string           `json:"verdict,omitempty"` // "RAISE" or "DISMISS" when done
	Agent        *AgentStatusData `json:"agent,omitempty"`
}

// PipelineStartData holds initialization data for pipeline mode
type PipelineStartData struct {
	FindingLabels []string `json:"findingLabels"`
}

// FindingUpdateData holds per-finding phase/agent updates
type FindingUpdateData struct {
	FindingIdx   int              `json:"findingIdx"`
	Phase        string           `json:"phase"`
	Verdict      string           `json:"verdict,omitempty"`
	Agent        *AgentStatusData `json:"agent,omitempty"`
}

// PhaseCompleteData holds complete phase data for timeline
type PhaseCompleteData struct {
	Phase       string               `json:"phase"`
	Description string               `json:"description"`
	StartTime   time.Time            `json:"startTime"`
	EndTime     time.Time            `json:"endTime"`
	Agents      []AgentCompleteData  `json:"agents"`
}

// AgentCompleteData holds agent data with full output
type AgentCompleteData struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model,omitempty"`
	State     string     `json:"state"`
	Lines     int        `json:"lines"`
	Output    string     `json:"output"`
	StartTime time.Time  `json:"startTime"`
	EndTime   *time.Time `json:"endTime,omitempty"`
}

// Hub manages WebSocket connections and broadcasts
type Hub struct {
	mu          sync.RWMutex
	clients     map[*Client]bool
	broadcast   chan []byte
	register    chan *Client
	unregister  chan *Client
	commands    chan clientCommand

	// Current state for new clients
	agents      map[int]*AgentStatusData
	phase       string
	phaseDesc   string
	logs        map[int][]string // agentID -> last N log lines
	maxLogLines int

	// Phase history for timeline
	phaseStartTime time.Time
	agentOutput    map[int]*strings.Builder // Full accumulated output per agent
	phaseHistory   []PhaseCompleteData      // Completed phases for timeline

	// Pipeline mode state
	pipelineMode   bool
	findingLabels  []string
	findingStates  map[int]*FindingState

	// Control functions
	killAgent AgentKiller
	killAll   AllAgentKiller
}

// clientCommand wraps a command with its source client for response
type clientCommand struct {
	client *Client
	cmd    CommandData
}

// Client represents a WebSocket client
type Client struct {
	hub  *Hub
	conn WebSocketConn
	send chan []byte
}

// WebSocketConn interface for testing
type WebSocketConn interface {
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	Close() error
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:      make(map[*Client]bool),
		broadcast:    make(chan []byte, 256),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		commands:     make(chan clientCommand, 16),
		agents:       make(map[int]*AgentStatusData),
		logs:         make(map[int][]string),
		maxLogLines:  500,
		agentOutput:  make(map[int]*strings.Builder),
		phaseHistory: make([]PhaseCompleteData, 0),
	}
}

// SetControllers sets the agent control functions
func (h *Hub) SetControllers(killAgent AgentKiller, killAll AllAgentKiller) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killAgent = killAgent
	h.killAll = killAll
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			// Send current state to new client
			h.sendCurrentState(client)
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()

		case cc := <-h.commands:
			h.handleCommand(cc.client, cc.cmd)
		}
	}
}

// handleCommand processes a control command
func (h *Hub) handleCommand(client *Client, cmd CommandData) {
	var ack CommandAckData
	ack.Action = cmd.Action

	h.mu.RLock()
	killAgent := h.killAgent
	killAll := h.killAll
	h.mu.RUnlock()

	switch cmd.Action {
	case "kill":
		if killAgent != nil {
			if killAgent(cmd.AgentID) {
				ack.Success = true
				ack.Message = "Agent killed"
			} else {
				ack.Success = false
				ack.Message = "Agent not found or already killed"
			}
		} else {
			ack.Success = false
			ack.Message = "Kill not available"
		}

	case "kill_all":
		if killAll != nil {
			count := killAll()
			ack.Success = true
			ack.Message = "Killed " + string(rune('0'+count)) + " agents"
			if count > 9 {
				ack.Message = "Killed multiple agents"
			}
		} else {
			ack.Success = false
			ack.Message = "Kill not available"
		}

	default:
		ack.Success = false
		ack.Message = "Unknown command"
	}

	// Send acknowledgment to the requesting client
	msg := Message{
		Type:      MsgCommandAck,
		Timestamp: time.Now(),
		Data:      ack,
	}
	if data, err := json.Marshal(msg); err == nil {
		select {
		case client.send <- data:
		default:
		}
	}
}

// SendCommand queues a command from a client
func (h *Hub) SendCommand(client *Client, cmd CommandData) {
	h.commands <- clientCommand{client: client, cmd: cmd}
}

func (h *Hub) sendCurrentState(client *Client) {
	// Send phase history first (for timeline)
	for _, phase := range h.phaseHistory {
		msg := Message{
			Type:      MsgPhaseComplete,
			Timestamp: time.Now(),
			Data:      phase,
		}
		if data, err := json.Marshal(msg); err == nil {
			client.send <- data
		}
	}

	// If in pipeline mode, send pipeline state
	if h.pipelineMode {
		// Send pipeline start message
		msg := Message{
			Type:      MsgPipelineStart,
			Timestamp: time.Now(),
			Data:      PipelineStartData{FindingLabels: h.findingLabels},
		}
		if data, err := json.Marshal(msg); err == nil {
			client.send <- data
		}

		// Send each finding's current state
		for _, state := range h.findingStates {
			msg := Message{
				Type:      MsgFindingUpdate,
				Timestamp: time.Now(),
				Data: FindingUpdateData{
					FindingIdx: state.FindingIdx,
					Phase:      state.CurrentPhase,
					Verdict:    state.Verdict,
					Agent:      state.Agent,
				},
			}
			if data, err := json.Marshal(msg); err == nil {
				client.send <- data
			}
		}
	}

	// Send current phase
	if h.phase != "" {
		msg := Message{
			Type:      MsgPhaseChange,
			Timestamp: time.Now(),
			Data:      PhaseData{Phase: h.phase, Description: h.phaseDesc},
		}
		if data, err := json.Marshal(msg); err == nil {
			client.send <- data
		}
	}

	// Send all agent statuses
	for _, agent := range h.agents {
		msg := Message{
			Type:      MsgAgentStatus,
			Timestamp: time.Now(),
			Data:      agent,
		}
		if data, err := json.Marshal(msg); err == nil {
			client.send <- data
		}
	}

	// Send recent logs
	for agentID, lines := range h.logs {
		for _, line := range lines {
			msg := Message{
				Type:      MsgAgentLog,
				Timestamp: time.Now(),
				Data:      AgentLogData{AgentID: agentID, Line: line},
			}
			if data, err := json.Marshal(msg); err == nil {
				client.send <- data
			}
		}
	}
}

// SetPhase updates the current phase
func (h *Hub) SetPhase(phase, description string) {
	h.mu.Lock()

	// Archive current phase before clearing (if we have agents)
	if h.phase != "" && len(h.agents) > 0 {
		h.archiveCurrentPhase()
	}

	// Update to new phase
	h.phase = phase
	h.phaseDesc = description
	h.phaseStartTime = time.Now()

	// Clear agents for new phase
	h.agents = make(map[int]*AgentStatusData)
	h.logs = make(map[int][]string)
	h.agentOutput = make(map[int]*strings.Builder)
	h.mu.Unlock()

	h.Broadcast(Message{
		Type:      MsgPhaseChange,
		Timestamp: time.Now(),
		Data:      PhaseData{Phase: phase, Description: description},
	})
}

// archiveCurrentPhase saves the current phase to history (must hold lock)
func (h *Hub) archiveCurrentPhase() {
	complete := PhaseCompleteData{
		Phase:       h.phase,
		Description: h.phaseDesc,
		StartTime:   h.phaseStartTime,
		EndTime:     time.Now(),
		Agents:      make([]AgentCompleteData, 0, len(h.agents)),
	}

	// Collect all agent data
	for id, agent := range h.agents {
		output := ""
		if builder, ok := h.agentOutput[id]; ok {
			output = builder.String()
		}

		complete.Agents = append(complete.Agents, AgentCompleteData{
			ID:        agent.ID,
			Name:      agent.Name,
			Provider:  agent.Provider,
			Model:     agent.Model,
			State:     agent.State,
			Lines:     agent.Lines,
			Output:    output,
			StartTime: agent.StartTime,
			EndTime:   agent.EndTime,
		})
	}

	// Sort agents by ID for consistent ordering
	sort.Slice(complete.Agents, func(i, j int) bool {
		return complete.Agents[i].ID < complete.Agents[j].ID
	})

	// Store in history
	h.phaseHistory = append(h.phaseHistory, complete)

	// Broadcast phase complete
	msg := Message{
		Type:      MsgPhaseComplete,
		Timestamp: time.Now(),
		Data:      complete,
	}
	if data, err := json.Marshal(msg); err == nil {
		// Non-blocking broadcast
		select {
		case h.broadcast <- data:
		default:
		}
	}
}

// UpdateAgent updates an agent's status
func (h *Hub) UpdateAgent(agent *AgentStatusData) {
	h.mu.Lock()
	h.agents[agent.ID] = agent
	h.mu.Unlock()

	h.Broadcast(Message{
		Type:      MsgAgentStatus,
		Timestamp: time.Now(),
		Data:      agent,
	})
}

// AddLog adds a log line for an agent
func (h *Hub) AddLog(agentID int, line string) {
	h.mu.Lock()
	// Ring buffer for recent logs (for new client sync)
	logs := h.logs[agentID]
	logs = append(logs, line)
	if len(logs) > h.maxLogLines {
		logs = logs[len(logs)-h.maxLogLines:]
	}
	h.logs[agentID] = logs

	// Accumulate full output for phase history
	if _, ok := h.agentOutput[agentID]; !ok {
		h.agentOutput[agentID] = &strings.Builder{}
	}
	h.agentOutput[agentID].WriteString(line)
	h.agentOutput[agentID].WriteString("\n")
	h.mu.Unlock()

	h.Broadcast(Message{
		Type:      MsgAgentLog,
		Timestamp: time.Now(),
		Data:      AgentLogData{AgentID: agentID, Line: line},
	})
}

// Broadcast sends a message to all clients
func (h *Hub) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.broadcast <- data
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// SetPipelineMode initializes the hub for pipelined debate mode
func (h *Hub) SetPipelineMode(findingLabels []string) {
	h.mu.Lock()
	h.pipelineMode = true
	h.findingLabels = findingLabels
	h.findingStates = make(map[int]*FindingState, len(findingLabels))

	// Initialize finding states
	for i, label := range findingLabels {
		h.findingStates[i] = &FindingState{
			FindingIdx:   i,
			Label:        label,
			CurrentPhase: "pending",
		}
	}

	// Clear regular phase state
	h.phase = "pipeline"
	h.phaseDesc = "Adversarial Review (Pipelined)"
	h.phaseStartTime = time.Now()
	h.agents = make(map[int]*AgentStatusData)
	h.logs = make(map[int][]string)
	h.agentOutput = make(map[int]*strings.Builder)
	h.mu.Unlock()

	// Broadcast pipeline start
	h.Broadcast(Message{
		Type:      MsgPipelineStart,
		Timestamp: time.Now(),
		Data:      PipelineStartData{FindingLabels: findingLabels},
	})
}

// UpdateFindingPhase updates a finding's current phase and agent
func (h *Hub) UpdateFindingPhase(findingIdx int, phase string, agent *AgentStatusData) {
	h.mu.Lock()
	if state, ok := h.findingStates[findingIdx]; ok {
		state.CurrentPhase = phase
		state.Agent = agent
	}
	h.mu.Unlock()

	// Also update the agent in the main agent map
	if agent != nil {
		h.UpdateAgent(agent)
	}

	h.Broadcast(Message{
		Type:      MsgFindingUpdate,
		Timestamp: time.Now(),
		Data: FindingUpdateData{
			FindingIdx: findingIdx,
			Phase:      phase,
			Agent:      agent,
		},
	})
}

// CompleteFinding marks a finding as done with its verdict
func (h *Hub) CompleteFinding(findingIdx int, verdict string) {
	h.mu.Lock()
	if state, ok := h.findingStates[findingIdx]; ok {
		state.CurrentPhase = "done"
		state.Verdict = verdict
	}
	h.mu.Unlock()

	h.Broadcast(Message{
		Type:      MsgFindingUpdate,
		Timestamp: time.Now(),
		Data: FindingUpdateData{
			FindingIdx: findingIdx,
			Phase:      "done",
			Verdict:    verdict,
		},
	})
}

// IsPipelineMode returns whether the hub is in pipeline mode
func (h *Hub) IsPipelineMode() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.pipelineMode
}

// BroadcastSessionUsage sends session-level usage data to all clients
func (h *Hub) BroadcastSessionUsage(data SessionUsageData) {
	h.Broadcast(Message{
		Type:      MsgSessionUsage,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// ExitPipelineMode clears pipeline mode state
func (h *Hub) ExitPipelineMode() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pipelineMode = false
	h.findingLabels = nil
	h.findingStates = nil
}
