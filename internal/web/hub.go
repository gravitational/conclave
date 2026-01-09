package web

import (
	"encoding/json"
	"sync"
	"time"
)

// MessageType identifies the type of WebSocket message
type MessageType string

const (
	MsgAgentStatus MessageType = "agent_status"
	MsgAgentLog    MessageType = "agent_log"
	MsgPhaseChange MessageType = "phase_change"
	MsgError       MessageType = "error"
	MsgCommand     MessageType = "command"
	MsgCommandAck  MessageType = "command_ack"
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

// AgentStatusData holds agent status info
type AgentStatusData struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model,omitempty"`
	State     string    `json:"state"` // waiting, running, done, error
	Activity  string    `json:"activity"`
	Lines     int       `json:"lines"`
	StartTime time.Time `json:"startTime"`
	EndTime   *time.Time `json:"endTime,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// AgentLogData holds a log line from an agent
type AgentLogData struct {
	AgentID int    `json:"agentId"`
	Line    string `json:"line"`
}

// PhaseData holds phase change info
type PhaseData struct {
	Phase       string `json:"phase"` // plan, assess, convene, synthesize
	Description string `json:"description"`
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
		clients:     make(map[*Client]bool),
		broadcast:   make(chan []byte, 256),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		commands:    make(chan clientCommand, 16),
		agents:      make(map[int]*AgentStatusData),
		logs:        make(map[int][]string),
		maxLogLines: 500,
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
	// Send phase
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
	h.phase = phase
	h.phaseDesc = description
	// Clear agents for new phase
	h.agents = make(map[int]*AgentStatusData)
	h.logs = make(map[int][]string)
	h.mu.Unlock()

	h.Broadcast(Message{
		Type:      MsgPhaseChange,
		Timestamp: time.Now(),
		Data:      PhaseData{Phase: phase, Description: description},
	})
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
	logs := h.logs[agentID]
	logs = append(logs, line)
	if len(logs) > h.maxLogLines {
		logs = logs[len(logs)-h.maxLogLines:]
	}
	h.logs[agentID] = logs
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
