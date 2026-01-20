# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Project Overview

Conclave is a CLI agent orchestration tool for systematic codebase security auditing. It coordinates multiple LLM agents (via Claude, Codex, or Gemini CLI) to analyze codebases, identify vulnerabilities, and synthesize findings through a structured adversarial review process.

## Build and Development Commands

```bash
# Build
go build ./cmd/conclave

# Run
./conclave --help

# Build and install to GOPATH/bin
go install ./cmd/conclave
```

## CLI Usage

**Important**: You must specify at least one provider flag (`--claude`, `--codex`, or `--gemini`). There is no default.

```bash
# Full pipeline (most common usage)
conclave --claude run [path]          # Run full audit with Claude
conclave --claude --gemini run        # Distribute across Claude + Gemini

# Individual stages
conclave --claude plan [path]         # Analyze codebase, create plan with subsystems
conclave --claude assess              # Assess random subsystem with 3 parallel agents
conclave --claude convene --subsystem X   # Run adversarial review on findings
conclave --claude complete --subsystem X  # Synthesize final results

# Learning and feedback
conclave --claude feedback 'message'  # Provide natural language feedback on findings
conclave --claude learn               # Auto-extract learnings from audit results

# Status (no provider needed)
conclave status                       # Show analysis state

# Additional flags
conclave --claude run --web           # Open web dashboard for real-time monitoring
conclave --claude run --gist          # Create secret GitHub gist of final report
```

## Architecture

```
cmd/conclave/main.go          Entry point
internal/
  cli/                        Cobra commands (root, plan, assess, convene, complete, status, feedback, learn)
  agent/                      Agent interface + Codex/Claude/Gemini implementations with streaming
  plan/                       Plan generation and parsing
  assess/                     Assessment prompt generation (focuses on single most critical finding)
  convene/                    Adversarial review orchestration (Steel Man/Critique/Judge/Synthesis)
  context/                    CONCLAVE.md repository context management
  state/                      .conclave directory management, markdown+frontmatter persistence
  display/                    Terminal output formatting and status display
  web/                        WebSocket-based dashboard for real-time monitoring
```

## Adversarial Review Flow (Convene)

The convene stage uses a structured 4-phase adversarial process:

```
ASSESS: 3 agents → 3 findings (filtered for actual vulnerabilities)
                              │
┌─────────────────────────────┴─────────────────────────────┐
│  STEEL MAN (parallel per finding)                         │
│  Advocate makes strongest case that finding is real       │
└─────────────────────────────┬─────────────────────────────┘
                              │
┌─────────────────────────────┴─────────────────────────────┐
│  CRITIQUE (parallel per finding)                          │
│  Skeptic argues finding should NOT be raised              │
└─────────────────────────────┬─────────────────────────────┘
                              │
┌─────────────────────────────┴─────────────────────────────┐
│  JUDGE (parallel per finding)                             │
│  Renders VERDICT: RAISE or DISMISS with confidence        │
└─────────────────────────────┬─────────────────────────────┘
                              │
┌─────────────────────────────┴─────────────────────────────┐
│  SYNTHESIS (single agent)                                 │
│  Combines verdicts into final report                      │
└───────────────────────────────────────────────────────────┘
```

## Agent Implementations

**Claude** (`internal/agent/claude.go`):
- Uses agentic mode with read-only tools: `Read`, `Grep`, `Glob`, `LSP`
- Real-time streaming via `--output-format stream-json --include-partial-messages`
- Tools like Edit, Write, Bash are blocked for safety

**Gemini** (`internal/agent/gemini.go`):
- Uses yolo mode (`-y`) for auto-approval
- Real-time streaming via `--output-format stream-json`

**Codex** (`internal/agent/codex.go`):
- Uses `codex exec --sandbox workspace-write` for sandboxed execution
- Line-by-line streaming via stdout

**Resilient Agent** (`internal/agent/resilient.go`):
- Wraps primary agent with fallback list
- Auto-retries with next provider on failure

## State Files

All state is stored in `.conclave/` as markdown files with YAML frontmatter:

```
.conclave/
  plans/              {uuid[:8]}-{slug}.md - Analysis plans with subsystems
  assessments/        {planID[:8]}/{subsystem}/agent-{n}.md - Individual perspectives
  verdicts/           {planID[:8]}/{subsystem}/verdict-{n}.md - Judge decisions
  debates/            {planID[:8]}/{subsystem}/debate-{n}.md - Debate round outputs
  results/            {planID[:8]}/{subsystem}.md - Final synthesized reports
```

## Repository Context (CONCLAVE.md)

The `CONCLAVE.md` file in target repos stores learned context:
- Known false positives (patterns to ignore)
- Focus areas (high-priority code paths)
- Ignore patterns (paths to skip)
- Subsystem-specific notes
- Confirmed findings history

This context is loaded and included in agent prompts to improve accuracy over time.

## Key Types

**state.Plan**: Analysis plan with subsystems
**state.Subsystem**: Part of codebase to analyze (slug, name, paths, description)
**state.Perspective**: Single agent's security assessment
**state.Verdict**: Judge's RAISE/DISMISS decision with confidence
**agent.Agent**: Interface implemented by Claude/Codex/Gemini agents
**agent.AgentResult**: Agent output with provider metadata

## Provider Distribution

When multiple providers are specified, agents are distributed across them:
- Each provider is used at least once (if n >= num_providers)
- Remaining slots filled randomly
- Each agent has failover capability to other providers
