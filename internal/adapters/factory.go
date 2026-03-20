package adapters

import (
	"github.com/infercore/infercore/internal/adapters/gemini"
	"github.com/infercore/infercore/internal/adapters/mock"
	"github.com/infercore/infercore/internal/adapters/vllm"
	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
)

// NewBackend constructs a BackendAdapter for a backend config entry.
func NewBackend(backend config.BackendConfig) (interfaces.BackendAdapter, bool) {
	switch backend.Type {
	case "mock":
		return mock.New(backend), true
	case "vllm", "openai", "openai_compatible":
		return vllm.New(backend), true
	case "gemini":
		return gemini.New(backend), true
	default:
		return nil, false
	}
}
