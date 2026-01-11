package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed skills/mcpx.md
var mcpxSkillContent string

// ClaudeSkillsDir is where Claude Code looks for user skills
var ClaudeSkillsDir = filepath.Join(os.Getenv("HOME"), ".claude", "skills")

// InitSkill installs the mcpx skill to Claude Code's skills directory
func InitSkill() error {
	// Create skills directory if it doesn't exist
	if err := os.MkdirAll(ClaudeSkillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	skillPath := filepath.Join(ClaudeSkillsDir, "mcpx.md")

	// Check if skill already exists
	if _, err := os.Stat(skillPath); err == nil {
		return fmt.Errorf("skill already exists at %s (use --init-skill --force to overwrite)", skillPath)
	}

	// Write the skill file
	if err := os.WriteFile(skillPath, []byte(mcpxSkillContent), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	return nil
}

// InitSkillForce installs the mcpx skill, overwriting if it exists
func InitSkillForce() error {
	// Create skills directory if it doesn't exist
	if err := os.MkdirAll(ClaudeSkillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	skillPath := filepath.Join(ClaudeSkillsDir, "mcpx.md")

	// Write the skill file
	if err := os.WriteFile(skillPath, []byte(mcpxSkillContent), 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	return nil
}
