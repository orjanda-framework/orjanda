package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTPStatusError identifies an LLM provider HTTP failure carrying its status
// code. The Gateway's failover logic walks the error chain (via Unwrap) looking
// for this type to decide whether to retry on a fallback provider: any
// status >= 500 triggers failover (PRD §26.4). It is wrapped inside an
// orjanda errors.Error by the concrete providers.
type HTTPStatusError struct {
	StatusCode int
	Message    string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("llm provider returned HTTP %d: %s", e.StatusCode, e.Message)
}

// ProviderOptions configures the built-in OpenAI and Anthropic adapters.
type ProviderOptions struct {
	// APIKey is the provider secret.
	APIKey string
	// Model is the default model identifier.
	Model string
	// BaseURL overrides the provider endpoint (defaults to the official
	// endpoint). Tests point this at an httptest.Server.
	BaseURL string
	// MaxTokens is the default completion token cap (0 = provider default).
	MaxTokens int
	// HTTPClient overrides the shared HTTP client (tests inject a short one).
	HTTPClient *http.Client
}

// doJSON performs an HTTP request with a JSON body and decodes the JSON
// response. Non-2xx statuses become an HTTPStatusError wrapped in the context
// of the caller. The response body is always closed.
func doJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPStatusError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// openStream performs an HTTP request and returns the response for streaming.
// Non-2xx responses are drained and returned as an HTTPStatusError so callers
// can trigger Gateway failover before consuming any stream events.
func openStream(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(msg))}
	}
	return resp, nil
}

// streamEvents scans an SSE response body, invoking onEvent for every "data:"
// payload line and stopping at [DONE] or EOF. The caller is responsible for
// closing resp.Body.
func streamEvents(ctx context.Context, resp *http.Response, onEvent func([]byte) error) error {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				return nil
			}
			continue
		}
		if err := onEvent([]byte(data)); err != nil {
			return err
		}
	}
	return scanner.Err()
}
