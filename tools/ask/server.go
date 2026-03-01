package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/kidkuddy/galacta/events"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// QuestionGate manages pending questions waiting for user answers.
type QuestionGate struct {
	emitter *events.Emitter
	pending map[string]chan string
	mu      sync.Mutex
}

// NewQuestionGate creates a new QuestionGate.
func NewQuestionGate(emitter *events.Emitter) *QuestionGate {
	return &QuestionGate{
		emitter: emitter,
		pending: make(map[string]chan string),
	}
}

// Respond delivers a user's answer to a pending question.
func (g *QuestionGate) Respond(requestID, answer string) error {
	g.mu.Lock()
	ch, ok := g.pending[requestID]
	if ok {
		delete(g.pending, requestID)
	}
	g.mu.Unlock()

	if !ok {
		return fmt.Errorf("no pending question with id %s", requestID)
	}
	ch <- answer
	return nil
}

// NewServer creates an MCP server with the ask_user tool.
func NewServer(gate *QuestionGate) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-ask",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tool := mcp.NewTool("galacta_ask_user",
		mcp.WithDescription("Ask the user a question and wait for their response. Use this to gather preferences, clarify ambiguous instructions, or get decisions."),
		mcp.WithString("question", mcp.Required(), mcp.Description("The question to ask the user")),
		mcp.WithString("options", mcp.Description("JSON array of {label, description} objects for structured choices")),
		mcp.WithString("header", mcp.Description("Short header label for the question")),
	)

	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		question := mcp.ParseString(req, "question", "")
		if question == "" {
			return mcp.NewToolResultError("question is required"), nil
		}

		header := mcp.ParseString(req, "header", "")
		optionsStr := mcp.ParseString(req, "options", "")

		var options []events.QuestionOption
		if optionsStr != "" {
			if err := json.Unmarshal([]byte(optionsStr), &options); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid options JSON: %v", err)), nil
			}
		}

		requestID := uuid.New().String()
		ch := make(chan string, 1)

		gate.mu.Lock()
		gate.pending[requestID] = ch
		gate.mu.Unlock()

		// Emit event so the CLI knows to prompt the user
		gate.emitter.EmitQuestionRequest(requestID, question, header, options)

		// Block until the user responds or context is cancelled
		select {
		case answer := <-ch:
			return mcp.NewToolResultText(answer), nil
		case <-ctx.Done():
			gate.mu.Lock()
			delete(gate.pending, requestID)
			gate.mu.Unlock()
			return mcp.NewToolResultError("question cancelled"), nil
		}
	})

	return srv
}
