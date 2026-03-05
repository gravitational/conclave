package assess

// Perspective represents a single agent's assessment of a subsystem
type Perspective struct {
	AgentID   int
	Subsystem string
	Content   string
}
