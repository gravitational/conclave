# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Conclave is a CLI agent orchestration tool for systematic codebase security auditing. It coordinates multiple LLM agents (via Codex, Claude, or Gemini CLI) to analyze codebases, identify vulnerabilities, and synthesize findings through a multi-stage debate process.

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

```bash
conclave run [path]               # Full pipeline: plan → assess → convene → complete
conclave plan [path]              # Analyze codebase, create plan with subsystems
conclave assess [--plan UUID]     # Assess random subsystem with 3 parallel agents
conclave ass                      # Alias for assess
conclave convene --subsystem X    # Have agents debate perspectives
conclave complete --subsystem X   # Synthesize final results
conclave status                   # Show analysis state

# Use Claude or Gemini CLI instead of Codex (default)
conclave --claude run
conclave --gemini run

# Multi-provider mode: distribute parallel agents across providers
conclave --multi run                    # All three providers
conclave --claude --gemini run          # Claude + Gemini
conclave --claude --codex run           # Claude + Codex
```

## Architecture

```
cmd/conclave/main.go          Entry point
internal/
  cli/                        Cobra commands (root, plan, assess, convene, complete, status)
  agent/                      Agent interface + Codex/Claude/Gemini implementations with streaming
  plan/                       Plan generation and parsing
  assess/                     Assessment prompt generation
  convene/                    Debate orchestration
  state/                      .conclave directory management, markdown+frontmatter persistence
```

**Agent execution**: Codex uses `codex exec --full-auto -` with prompt via stdin. Claude uses `claude -p <prompt>`. Gemini uses `gemini -y` with prompt via stdin. All stream stdout line-by-line through goroutines.

**State files**: Plans, perspectives, debates, and results are stored in `.conclave/` as markdown files with YAML frontmatter. Plans have UUID-based filenames.

**Parallel agents**: The assess and convene commands run 3 agents concurrently using goroutines, with colored prefixes for real-time output streaming.
