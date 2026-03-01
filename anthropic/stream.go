package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// ParseSSEStream reads from an io.Reader (HTTP response body) and sends parsed
// StreamEvents to the provided channel. Closes the channel when done.
func ParseSSEStream(ctx context.Context, reader io.Reader, events chan<- StreamEvent) {
	defer close(events)

	scanner := bufio.NewScanner(reader)
	var currentEventType string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		// Ignore SSE comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Blank lines are SSE record delimiters
		if line == "" {
			continue
		}

		// Track event type
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Parse data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			event := StreamEvent{
				Type: currentEventType,
				Data: json.RawMessage(data),
			}

			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}
}
