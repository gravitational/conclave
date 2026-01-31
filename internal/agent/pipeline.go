package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rob-picard-teleport/conclave/internal/convene"
	"github.com/rob-picard-teleport/conclave/internal/display"
	"github.com/rob-picard-teleport/conclave/internal/state"
	"github.com/rob-picard-teleport/conclave/internal/web"
)

// capitalize returns the string with the first letter uppercased
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Phase constants for pipeline
const (
	PhaseSteelMan = "steelman"
	PhaseCritique = "critique"
	PhaseJudge    = "judge"
	PhaseDone     = "done"
)

// FindingPipeline holds the results from processing a single finding
type FindingPipeline struct {
	FindingIdx int
	Finding    state.Perspective
	SteelMan   AgentResult
	Critique   AgentResult
	Judge      AgentResult
	Error      error
}

// PipelineConfig configures a pipelined debate run
type PipelineConfig struct {
	Debate      *convene.Debate
	Findings    []state.Perspective
	CreateAgent func() Agent // Legacy fallback - used if phase-specific creators not set
	Hub         *web.Hub             // optional, for web mode
	Display     *display.PipelineDisplay // optional, for terminal mode

	// Phase-specific agent creators (optional - falls back to CreateAgent)
	CreateSteelManAgent func() Agent
	CreateCritiqueAgent func() Agent
	CreateJudgeAgent    func() Agent
}

// PipelineCallback is called when a finding changes phase
type PipelineCallback func(findingIdx int, phase string, agent *web.AgentStatusData)

// getAgentForPhase returns the appropriate agent for a pipeline phase
func getAgentForPhase(cfg PipelineConfig, phase string) Agent {
	switch phase {
	case PhaseSteelMan:
		if cfg.CreateSteelManAgent != nil {
			return cfg.CreateSteelManAgent()
		}
	case PhaseCritique:
		if cfg.CreateCritiqueAgent != nil {
			return cfg.CreateCritiqueAgent()
		}
	case PhaseJudge:
		if cfg.CreateJudgeAgent != nil {
			return cfg.CreateJudgeAgent()
		}
	}
	// Fallback to generic CreateAgent
	return cfg.CreateAgent()
}

// agentID generates a unique agent ID for pipeline mode
// ID = (findingIdx * 100) + phaseOffset
// phaseOffset: steelman=0, critique=1, judge=2
func agentID(findingIdx int, phase string) int {
	phaseOffset := 0
	switch phase {
	case PhaseCritique:
		phaseOffset = 1
	case PhaseJudge:
		phaseOffset = 2
	}
	return (findingIdx * 100) + phaseOffset
}

// agentName generates a finding-centric agent name
func agentName(findingIdx int, phase string) string {
	switch phase {
	case PhaseSteelMan:
		return fmt.Sprintf("F%d Advocate", findingIdx+1)
	case PhaseCritique:
		return fmt.Sprintf("F%d Critic", findingIdx+1)
	case PhaseJudge:
		return fmt.Sprintf("F%d Judge", findingIdx+1)
	default:
		return fmt.Sprintf("F%d Agent", findingIdx+1)
	}
}

// RunPipelinedDebate runs the adversarial debate with each finding progressing
// through its own pipeline independently
func RunPipelinedDebate(cfg PipelineConfig) []FindingPipeline {
	n := len(cfg.Findings)
	results := make([]FindingPipeline, n)

	// Initialize results
	for i, finding := range cfg.Findings {
		results[i] = FindingPipeline{
			FindingIdx: i,
			Finding:    finding,
		}
	}

	// Clear registry for fresh run
	GlobalRegistry.Clear()

	// Notify web hub of pipeline mode
	if cfg.Hub != nil {
		labels := make([]string, n)
		for i, finding := range cfg.Findings {
			labels[i] = fmt.Sprintf("Finding %d (%s)", i+1, findingLabel(finding))
		}
		cfg.Hub.SetPipelineMode(labels)
	}

	// Start pipeline display if in terminal mode
	if cfg.Display != nil {
		cfg.Display.Start()
	}

	var wg sync.WaitGroup
	for i := range cfg.Findings {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = runFindingPipeline(cfg, idx)
		}(i)
	}

	wg.Wait()

	// Stop pipeline display
	if cfg.Display != nil {
		cfg.Display.Stop()
	}

	return results
}

// runFindingPipeline runs a single finding through the full pipeline
func runFindingPipeline(cfg PipelineConfig, findingIdx int) FindingPipeline {
	result := FindingPipeline{
		FindingIdx: findingIdx,
		Finding:    cfg.Findings[findingIdx],
	}

	// Phase 1: Steel Man
	steelManResult, err := runPipelinePhase(cfg, findingIdx, PhaseSteelMan, func() string {
		return cfg.Debate.SteelManPromptForFinding(cfg.Findings[findingIdx])
	})
	if err != nil {
		result.Error = fmt.Errorf("steel man failed: %w", err)
		return result
	}
	result.SteelMan = steelManResult

	steelMan := convene.DebateRound{
		AgentNum: findingIdx + 1,
		Provider: steelManResult.Agent.Provider,
		Model:    steelManResult.Agent.Model,
		Content:  steelManResult.Content,
	}

	// Phase 2: Critique
	critiqueResult, err := runPipelinePhase(cfg, findingIdx, PhaseCritique, func() string {
		return cfg.Debate.CritiquePromptForFinding(cfg.Findings[findingIdx], steelMan)
	})
	if err != nil {
		result.Error = fmt.Errorf("critique failed: %w", err)
		return result
	}
	result.Critique = critiqueResult

	critique := convene.DebateRound{
		AgentNum: findingIdx + 1,
		Provider: critiqueResult.Agent.Provider,
		Model:    critiqueResult.Agent.Model,
		Content:  critiqueResult.Content,
	}

	// Phase 3: Judge
	judgeResult, err := runPipelinePhase(cfg, findingIdx, PhaseJudge, func() string {
		return cfg.Debate.JudgePromptForFinding(cfg.Findings[findingIdx], steelMan, critique)
	})
	if err != nil {
		result.Error = fmt.Errorf("judge failed: %w", err)
		return result
	}
	result.Judge = judgeResult

	// Notify completion
	if cfg.Hub != nil {
		verdict := extractVerdict(judgeResult.Content)
		cfg.Hub.CompleteFinding(findingIdx, verdict)
	}
	if cfg.Display != nil {
		verdict := extractVerdict(judgeResult.Content)
		cfg.Display.SetDone(findingIdx, verdict)
	}

	return result
}

// runPipelinePhase runs a single phase for a finding
func runPipelinePhase(cfg PipelineConfig, findingIdx int, phase string, promptFn func() string) (AgentResult, error) {
	ag := getAgentForPhase(cfg, phase)
	prompt := promptFn()
	id := agentID(findingIdx, phase)
	name := agentName(findingIdx, phase)

	ctx, cancel := context.WithCancel(context.Background())

	// Get model info
	model := ""
	if m, ok := ag.(interface{ Model() string }); ok {
		model = m.Model()
	}

	// Register with global registry
	GlobalRegistry.Register(id, name, ag.Name(), model, cancel)
	defer GlobalRegistry.Unregister(id)

	startTime := time.Now()

	// Notify phase start
	if cfg.Hub != nil {
		cfg.Hub.UpdateFindingPhase(findingIdx, phase, &web.AgentStatusData{
			ID:        id,
			Name:      name,
			Provider:  capitalize(ag.Name()),
			Model:     model,
			State:     "running",
			Activity:  "Starting...",
			Lines:     0,
			StartTime: startTime,
		})
	}
	if cfg.Display != nil {
		cfg.Display.SetPhase(findingIdx, phase, ag.Name(), model)
	}

	// Run the agent
	output, errCh := ag.Run(ctx, prompt)

	var result strings.Builder
	lineCount := 0

	for line := range output {
		result.WriteString(line)
		result.WriteString("\n")
		lineCount++

		if cfg.Hub != nil {
			cfg.Hub.AddLog(id, line)

			activity := extractActivity(line)
			if activity == "" {
				activity = fmt.Sprintf("Processing... (%d lines)", lineCount)
			}

			cfg.Hub.UpdateAgent(&web.AgentStatusData{
				ID:        id,
				Name:      name,
				Provider:  capitalize(ag.Name()),
				Model:     model,
				State:     "running",
				Activity:  activity,
				Lines:     lineCount,
				StartTime: startTime,
			})
		}
		if cfg.Display != nil {
			activity := extractActivity(line)
			if activity != "" {
				cfg.Display.SetActivity(findingIdx, activity)
			}
		}
	}

	endTime := time.Now()
	if err := <-errCh; err != nil {
		// Check if this was a cancellation (kill)
		errState := "error"
		errStr := err.Error()
		if ctx.Err() == context.Canceled {
			errState = "killed"
			errStr = "Killed by user"
		}
		if cfg.Hub != nil {
			cfg.Hub.UpdateAgent(&web.AgentStatusData{
				ID:        id,
				Name:      name,
				Provider:  capitalize(ag.Name()),
				Model:     model,
				State:     errState,
				Activity:  errStr,
				Lines:     lineCount,
				StartTime: startTime,
				EndTime:   &endTime,
				Error:     errStr,
			})
		}
		if cfg.Display != nil {
			cfg.Display.SetError(findingIdx, err)
		}
		return AgentResult{}, err
	}

	if cfg.Hub != nil {
		cfg.Hub.UpdateAgent(&web.AgentStatusData{
			ID:        id,
			Name:      name,
			Provider:  capitalize(ag.Name()),
			Model:     model,
			State:     "done",
			Activity:  "Complete",
			Lines:     lineCount,
			StartTime: startTime,
			EndTime:   &endTime,
		})
	}

	return AgentResult{
		Content: result.String(),
		Agent:   GetMeta(ag),
	}, nil
}

// extractVerdict extracts RAISE or DISMISS from judge output
func extractVerdict(content string) string {
	content = strings.ToUpper(content)
	if strings.Contains(content, "VERDICT: RAISE") || strings.Contains(content, "VERDICT:RAISE") {
		return "RAISE"
	}
	return "DISMISS"
}

// findingLabel creates a short label for a finding
func findingLabel(p state.Perspective) string {
	if p.Agent.Provider != "" && p.Agent.Provider != "unknown" {
		return p.Agent.Provider
	}
	return fmt.Sprintf("Agent %d", p.AgentNum)
}

// ToDebateRounds converts FindingPipeline results to DebateRounds for synthesis
func ToDebateRounds(results []FindingPipeline, phase string) []convene.DebateRound {
	rounds := make([]convene.DebateRound, len(results))
	for i, r := range results {
		var ar AgentResult
		switch phase {
		case PhaseSteelMan:
			ar = r.SteelMan
		case PhaseCritique:
			ar = r.Critique
		case PhaseJudge:
			ar = r.Judge
		}
		rounds[i] = convene.DebateRound{
			AgentNum: i + 1,
			Provider: ar.Agent.Provider,
			Model:    ar.Agent.Model,
			Content:  ar.Content,
		}
	}
	return rounds
}
