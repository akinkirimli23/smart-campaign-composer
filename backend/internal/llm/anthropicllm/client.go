// Package anthropicllm implements llm.Client on top of the official Anthropic Go
// SDK (DECISIONS 16). It is the only package that imports the SDK.
//
// The client only transports: it sends the schema as a structured-output
// constraint and returns the model's text. Callers still parse and validate
// that text themselves; a schema-conforming reply is not a valid reply.
package anthropicllm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
)

// DefaultModel is used when ANTHROPIC_MODEL is not set.
const DefaultModel = "claude-opus-5-5"

// maxTokens bounds one reply. Replies here are small JSON documents, but the
// budget also covers the model's thinking, so it is deliberately generous;
// hitting it is reported as ErrTruncated rather than returned as broken JSON.
const maxTokens = 16000

var (
	// ErrRefused means the model declined the request (stop_reason "refusal").
	// Retrying the same conversation will not help.
	ErrRefused = errors.New("anthropic: model refused the request")
	// ErrTruncated means the reply was cut off before it was complete.
	ErrTruncated = errors.New("anthropic: reply truncated")
	// ErrEmpty means the model finished without producing any text.
	ErrEmpty = errors.New("anthropic: empty reply")
)

// Client calls the Messages API. It is safe for concurrent use.
type Client struct {
	api   anthropic.Client
	model string
}

// New returns a Client. An empty model means DefaultModel. opts are passed to
// the SDK after the API key, so tests can point the client at a local server.
// The SDK retries 408/409/429/5xx and connection errors (twice by default).
func New(apiKey, model string, opts ...option.RequestOption) *Client {
	if model == "" {
		model = DefaultModel
	}
	opts = append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	return &Client{api: anthropic.NewClient(opts...), model: model}
}

// Model reports the model id requests are sent to.
func (c *Client) Model() string { return c.model }

// Complete implements llm.Client.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	return c.Stream(ctx, req, nil)
}

// Stream implements llm.Streamer. Every request is streamed, also when onText
// is nil, so a slow reply never runs into an idle HTTP timeout.
func (c *Client) Stream(ctx context.Context, req llm.Request, onText func(delta string)) (llm.Response, error) {
	stream := c.api.Messages.NewStreaming(ctx, c.params(req))
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return llm.Response{}, fmt.Errorf("anthropic: reading stream: %w", err)
		}
		if onText == nil {
			continue
		}
		if delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && text.Text != "" {
				onText(text.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return llm.Response{}, wrap(err)
	}

	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return llm.Response{}, fmt.Errorf("%w (category %q)", ErrRefused, msg.StopDetails.Category)
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return llm.Response{}, fmt.Errorf("%w (stop_reason %s)", ErrTruncated, msg.StopReason)
	}

	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return llm.Response{}, ErrEmpty
	}
	// Raw keeps the whole assistant turn, thinking blocks included, so a retry
	// replays it unchanged instead of a text-only reconstruction.
	return llm.Response{Text: text.String(), Raw: msg.ToParam()}, nil
}

func (c *Client) params(req llm.Request) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: maxTokens,
		Messages:  make([]anthropic.MessageParam, 0, len(req.Messages)),
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.Schema != nil {
		// Thinking and effort are left at the model's defaults so the same
		// request works on any current model.
		params.OutputConfig = anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		}
	}
	for _, m := range req.Messages {
		switch {
		case m.Role == llm.RoleUser:
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Text)))
		case isAssistantTurn(m.Raw):
			params.Messages = append(params.Messages, m.Raw.(anthropic.MessageParam))
		default:
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Text)))
		}
	}
	return params
}

func isAssistantTurn(raw any) bool {
	p, ok := raw.(anthropic.MessageParam)
	return ok && p.OfAssistant != nil
}

// wrap adds the HTTP status and request id of an API error; the id is what
// Anthropic support asks for. The API key never appears in SDK errors.
func wrap(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return fmt.Errorf("anthropic: status %d (request %s): %w", apiErr.StatusCode, apiErr.RequestID, err)
	}
	return fmt.Errorf("anthropic: %w", err)
}
