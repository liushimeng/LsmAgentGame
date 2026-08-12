// Package llm is the parent of the LLM subsystem. It re-exports the leaf wire
// types from llm/types so callers (api/, agent/, main.go) only need to import
// "LsmAgentGame/llm":
//
//	- llm.LLMProvider  — unified provider interface
//	- llm.LLMRequest / llm.LLMResponse / llm.LLMUsage  — wire format
//	- llm.Message / llm.ContentBlock / llm.SystemBlock / llm.ToolDef / llm.Metadata
//	- llm.ModelInfo  — key-free model metadata for /api/llm/models
//	- llm.PlaceholderKey  — sentinel api_key for conf.example
//
// The Registry type lives in llm/registry.go (same package) and the
// anthropic-specific wire client lives in the llm/anthropic subpackage.
package llm

import (
	"LsmAgentGame/llm/types"
)

// Re-exports — see llm/types for full docs.
type (
	LLMProvider  = types.LLMProvider
	LLMRequest   = types.LLMRequest
	LLMResponse  = types.LLMResponse
	LLMUsage     = types.LLMUsage
	Message      = types.Message
	ContentBlock = types.ContentBlock
	SystemBlock  = types.SystemBlock
	ToolDef      = types.ToolDef
	Metadata     = types.Metadata
	ModelInfo    = types.ModelInfo
)

// PlaceholderKey is the api_key sentinel used in conf.example. Operators MUST
// replace it with a real key in the gitignored LsmAgentGame.conf before any model
// becomes usable.
const PlaceholderKey = types.PlaceholderKey
