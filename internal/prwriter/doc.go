// Package prwriter renders PR comments (markdown) and drives the Apply Fix
// flow: signed-token validation, diff generation via internal/agent, and
// opening the resulting PR through the GitHub API.
package prwriter
