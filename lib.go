package agentapisdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/coder/agentapi-sdk-go/gen"
	"github.com/tmaxmax/go-sse"
)

type Client struct {
	client gen.ClientWithResponses
}

type ClientOption = gen.ClientOption

var WithHTTPClient = gen.WithHTTPClient
var WithRequestEditorFn = gen.WithRequestEditorFn
var WithBaseURL = gen.WithBaseURL

func NewClient(server string, opts ...ClientOption) (*Client, error) {
	client, err := gen.NewClientWithResponses(server, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{*client}, nil
}

type PostMessageParams = gen.PostMessageJSONRequestBody
type PostMessageResponse = gen.PostMessageResponse

func (c *Client) PostMessage(ctx context.Context, body PostMessageParams) (*PostMessageResponse, error) {
	return c.client.PostMessageWithResponse(ctx, body)
}

type GetMessagesResponse = gen.GetMessagesResponse

func (c *Client) GetMessages(ctx context.Context) (*GetMessagesResponse, error) {
	return c.client.GetMessagesWithResponse(ctx)
}

type GetStatusResponse = gen.GetStatusResponse

func (c *Client) GetStatus(ctx context.Context) (*GetStatusResponse, error) {
	return c.client.GetStatusWithResponse(ctx)
}

type Event = any
type EventMessageUpdate = gen.MessageUpdateBody
type EventStatusChange = gen.StatusChangeBody

type SubscribeEventsResponse = gen.SubscribeEventsResponse

func (c *Client) SubscribeEvents(ctx context.Context) (*chan Event, *chan error, error) {
	resp, err := c.client.SubscribeEvents(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to subscribe to events: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return nil, nil, fmt.Errorf("failed to subscribe to events: status code %d, body: %s", resp.StatusCode, string(body))
	}

	ch := make(chan Event, 16)
	errCh := make(chan error, 1)

	go func() {
		defer close(ch)
		defer close(errCh)
		defer resp.Body.Close()

		// A message update may be as large as 80,000 bytes if the agent's
		// response takes up the entire terminal screen.
		readCfg := &sse.ReadConfig{
			MaxEventSize: 1024 * 128, // 128KB
		}

		// We don't need to explicitly handle the context cancellation here
		// because the response body will be closed when the context is
		// cancelled.
		for ev, err := range sse.Read(resp.Body, readCfg) {
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				errCh <- err
				return
			}
			
			switch ev.Type {
			case "message_update":
				var messageUpdate EventMessageUpdate
				err = json.Unmarshal([]byte(ev.Data), &messageUpdate)
				if err != nil {
					errCh <- err
					return
				}
				ch <- messageUpdate
			case "status_change":
				var statusChange EventStatusChange
				err = json.Unmarshal([]byte(ev.Data), &statusChange)
				if err != nil {
					errCh <- err
					return
				}
				ch <- statusChange
			default:
				errCh <- fmt.Errorf("unknown event type: %s, data: %s", ev.Type, ev.Data)
				return
			}
		}

	}()

	return &ch, &errCh, nil
}