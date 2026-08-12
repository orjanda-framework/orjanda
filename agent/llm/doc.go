// Package llm defines the llm.Provider interface and implements the OpenAI,
// openai_compatible, and Anthropic adapters with streaming, tool-calling,
// structured-output support, failover, circuit-breaking, and token tracking.
// The openai_compatible adapter speaks the OpenAI chat-completions wire format
// to any server exposing it (Ollama, vLLM, LM Studio, Together, Groq).
//
// See TAD §2.6, §2.7 and PRD §26 for the full specification.
// Implemented in Phase 7.
package llm
