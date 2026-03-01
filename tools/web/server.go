package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with web fetching tools.
func NewServer() *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-web",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerWebFetch(srv)

	return srv
}

var htmlTagsRe = regexp.MustCompile(`<[^>]*>`)
var whitespaceRe = regexp.MustCompile(`\n{3,}`)

func stripHTMLTags(s string) string {
	// Remove script and style blocks entirely
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	s = scriptRe.ReplaceAllString(s, "")
	s = styleRe.ReplaceAllString(s, "")

	// Remove all remaining HTML tags
	s = htmlTagsRe.ReplaceAllString(s, "")

	// Collapse excessive newlines
	s = whitespaceRe.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

func registerWebFetch(srv *server.MCPServer) {
	tool := mcp.NewTool("galacta_web_fetch",
		mcp.WithDescription("Fetch content from a URL"),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL to fetch")),
		mcp.WithNumber("max_bytes", mcp.Description("Maximum bytes to return (default 1048576)")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url := mcp.ParseString(req, "url", "")
		maxBytes := mcp.ParseInt(req, "max_bytes", 1048576)

		if url == "" {
			return mcp.NewToolResultError("url is required"), nil
		}

		if maxBytes <= 0 {
			maxBytes = 1048576
		}

		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("creating request: %s", err.Error())), nil
		}
		httpReq.Header.Set("User-Agent", "Galacta/1.0")

		resp, err := client.Do(httpReq)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching URL: %s", err.Error())), nil
		}
		defer resp.Body.Close()

		// Read up to maxBytes
		body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reading response: %s", err.Error())), nil
		}

		contentType := resp.Header.Get("Content-Type")
		content := string(body)

		// Strip HTML tags if content is HTML
		if strings.Contains(contentType, "text/html") {
			content = stripHTMLTags(content)
		}

		truncated := ""
		if len(body) >= maxBytes {
			truncated = " (truncated)"
		}

		result := fmt.Sprintf("Status: %d\nContent-Type: %s\nBytes: %d%s\n\n%s",
			resp.StatusCode, contentType, len(body), truncated, content)

		return mcp.NewToolResultText(result), nil
	})
}
