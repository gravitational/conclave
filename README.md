# Conclave

A CLI tool that orchestrates multiple LLM agents to systematically audit codebases for security vulnerabilities.

## Warning

**This is an entirely vibe-coded project.** No humans have reviewed this code. It was generated through AI-to-AI conversation and should be treated with appropriate caution.

- Do not run this on sensitive systems without review
- Do not trust the security of this tool itself
- The agents run with `--full-auto` flags and can execute arbitrary commands
- State files in `.conclave/` contain unvalidated LLM output

Use at your own risk.

## What It Does

Conclave runs a multi-stage security audit pipeline:

1. **Plan** - Analyzes a codebase and breaks it into subsystems
2. **Assess** - Spins up 3 parallel agents to review a subsystem for vulnerabilities
3. **Convene** - Has agents debate and refine their findings
4. **Complete** - Synthesizes final results

## Quick Start

```bash
go build ./cmd/conclave
./conclave run             # Uses Codex CLI (default)
./conclave --claude run    # Uses Claude CLI
./conclave --gemini run    # Uses Gemini CLI
```

## Requirements

- Go 1.21+
- One of: [Codex CLI](https://github.com/openai/codex), [Claude CLI](https://claude.ai/code), or [Gemini CLI](https://github.com/google-gemini/gemini-cli)
