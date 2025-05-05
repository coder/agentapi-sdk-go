package agentapisdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	agentapisdk "github.com/coder/agentapi-sdk-go"
	"github.com/stretchr/testify/assert"
)

// MockHTTPClient is a mock implementation of the agentapisdk.HTTPDoer interface
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

// Do implements the HTTPDoer interface
func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, errors.New("mock not implemented")
}

// Helper function to create a successful JSON response
func newJSONResponse(statusCode int, body interface{}) *http.Response {
	bodyBytes, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// Helper function to create an error response
func newErrorResponse(statusCode int, errTitle, errDetail string) *http.Response {
	errModel := agentapisdk.ErrorModel{
		Title:  &errTitle,
		Detail: &errDetail,
		Status: &[]int64{int64(statusCode)}[0],
	}
	bodyBytes, _ := json.Marshal(errModel)
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

// Custom SSE event implementation that mimics the server response format
// that the go-sse library expects
type mockEventStream struct {
	events []string
	pos    int
	closed bool
}

func newMockEventStream(events []string) *mockEventStream {
	return &mockEventStream{
		events: events,
		pos:    0,
		closed: false,
	}
}

func (m *mockEventStream) Read(p []byte) (n int, err error) {
	if m.closed {
		return 0, io.EOF
	}

	if m.pos >= len(m.events) {
		m.closed = true
		return 0, io.EOF
	}

	event := m.events[m.pos]
	if !strings.HasSuffix(event, "\n\n") {
		event = event + "\n\n"
	}

	bytesToCopy := copy(p, event)
	if bytesToCopy < len(event) {
		// Buffer too small, we'll need multiple reads
		m.events[m.pos] = event[bytesToCopy:]
	} else {
		// Move to next event
		m.pos++
	}

	return bytesToCopy, nil
}

func (m *mockEventStream) Close() error {
	m.closed = true
	return nil
}

// Helper function to create an SSE response for events
func newSSEResponse(events []string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(newMockEventStream(events)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func TestNewClient(t *testing.T) {
	t.Run("valid server URL", func(t *testing.T) {
		client, err := agentapisdk.NewClient("https://example.com")
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("with custom HTTP client", func(t *testing.T) {
		mockClient := &MockHTTPClient{}
		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("with base URL option", func(t *testing.T) {
		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithBaseURL("https://other.example.com"))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("with request editor", func(t *testing.T) {
		editor := agentapisdk.RequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("X-Test-Header", "test-value")
			return nil
		})
		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithRequestEditorFn(editor))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestPostMessage(t *testing.T) {
	ctx := context.Background()

	t.Run("successful post", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				if req.Method != "POST" {
					t.Errorf("expected POST method, got %s", req.Method)
				}
				if !strings.HasSuffix(req.URL.Path, "/message") {
					t.Errorf("unexpected URL path: %s", req.URL.Path)
				}

				// Return a success response
				return newJSONResponse(http.StatusOK, agentapisdk.PostMessageResponse{
					Ok: true,
				}), nil
			},
		}

		// Create client with mock
		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test PostMessage
		req := agentapisdk.PostMessageParams{
			Content: "Hello, agent!",
			Type:    agentapisdk.MessageTypeUser,
		}
		resp, err := client.PostMessage(ctx, req)
		if err != nil {
			t.Fatalf("PostMessage failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !resp.Ok {
			t.Error("expected Ok to be true")
		}
	})

	t.Run("server error", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate a response with no JSON200 field to trigger an error in lib.go
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
				}, nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		req := agentapisdk.PostMessageParams{
			Content: "Hello, agent!",
			Type:    agentapisdk.MessageTypeUser,
		}
		resp, err := client.PostMessage(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid response")
		}
		if resp != nil {
			t.Fatal("expected nil response for error case")
		}
	})

	t.Run("client error", func(t *testing.T) {
		networkErr := errors.New("network error")
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, networkErr
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test PostMessage with client error
		req := agentapisdk.PostMessageParams{
			Content: "Hello, agent!",
			Type:    agentapisdk.MessageTypeUser,
		}
		resp, err := client.PostMessage(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if resp != nil {
			t.Fatal("expected nil response for error case")
		}
		if !errors.Is(err, networkErr) {
			t.Errorf("expected network error, got: %v", err)
		}
	})
}

func TestGetMessages(t *testing.T) {
	ctx := context.Background()

	t.Run("empty message list", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				if req.Method != "GET" {
					t.Errorf("expected GET method, got %s", req.Method)
				}
				if !strings.HasSuffix(req.URL.Path, "/messages") {
					t.Errorf("unexpected URL path: %s", req.URL.Path)
				}

				// Return empty messages response
				return newJSONResponse(http.StatusOK, agentapisdk.GetMessagesResponse{
					Messages: []agentapisdk.Message{},
				}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test GetMessages
		resp, err := client.GetMessages(ctx)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if len(resp.Messages) != 0 {
			t.Errorf("expected empty messages, got %d messages", len(resp.Messages))
		}
	})

	t.Run("populated message list", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				now := time.Now()
				messages := []agentapisdk.Message{
					{
						Id:      1,
						Content: "Hello!",
						Role:    agentapisdk.RoleUser,
						Time:    now.Add(-time.Minute),
					},
					{
						Id:      2,
						Content: "How can I help you?",
						Role:    agentapisdk.RoleAgent,
						Time:    now,
					},
				}

				return newJSONResponse(http.StatusOK, agentapisdk.GetMessagesResponse{
					Messages: messages,
				}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.GetMessages(ctx)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		messages := resp.Messages
		if len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(messages))
		}
		if messages[0].Id != 1 || messages[0].Role != agentapisdk.RoleUser {
			t.Errorf("unexpected first message: %+v", messages[0])
		}
		if messages[1].Id != 2 || messages[1].Role != agentapisdk.RoleAgent {
			t.Errorf("unexpected second message: %+v", messages[1])
		}
	})

	t.Run("server error", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate a response with no JSON200 field to trigger an error in lib.go
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
				}, nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.GetMessages(ctx)
		if err == nil {
			t.Fatal("expected error for invalid response")
		}
		if resp != nil {
			t.Fatal("expected nil response for error case")
		}
	})

	t.Run("client error", func(t *testing.T) {
		networkErr := errors.New("network error")
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, networkErr
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.GetMessages(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if resp != nil {
			t.Fatal("expected nil response for error case")
		}
		if !errors.Is(err, networkErr) {
			t.Errorf("expected network error, got: %v", err)
		}
	})
}

func TestGetStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("running status", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					t.Errorf("expected GET method, got %s", req.Method)
				}
				if !strings.HasSuffix(req.URL.Path, "/status") {
					t.Errorf("unexpected URL path: %s", req.URL.Path)
				}

				return newJSONResponse(http.StatusOK, agentapisdk.GetStatusResponse{
					Status: agentapisdk.StatusRunning,
				}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test GetStatus
		resp, err := client.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if resp.Status != agentapisdk.StatusRunning {
			t.Errorf("expected status %s, got %s", agentapisdk.StatusRunning, resp.Status)
		}
	})

	t.Run("stable status", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newJSONResponse(http.StatusOK, agentapisdk.GetStatusResponse{
					Status: agentapisdk.StatusStable,
				}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if resp.Status != agentapisdk.StatusStable {
			t.Errorf("expected status %s, got %s", agentapisdk.StatusStable, resp.Status)
		}
	})

	t.Run("server error", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Simulate a response with no JSON200 field to trigger an error in lib.go
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte{})),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
				}, nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.GetStatus(ctx)
		if err == nil {
			t.Fatal("expected error for invalid response")
		}
		if resp != nil {
			t.Fatal("expected nil response for error case")
		}
	})

	t.Run("client error", func(t *testing.T) {
		networkErr := errors.New("network error")
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, networkErr
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		resp, err := client.GetStatus(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if resp != nil {
			t.Fatal("expected nil response for error case")
		}
	})
}

func marshal(t *testing.T, event any) string {
	json, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}
	return string(json)
}

func TestSubscribeEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("successful subscription with message update", func(t *testing.T) {
		messageUpdate := agentapisdk.EventMessageUpdate{
			Id:      1,
			Message: "Hello, world!",
			Role:    agentapisdk.RoleAgent,
			Time:    time.Now(),
		}
		messageJSON := marshal(t, messageUpdate)
		messageEvent := fmt.Sprintf("event: %s\ndata: %s", agentapisdk.EventTypeMessageUpdate, messageJSON)

		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" {
					t.Errorf("expected GET method, got %s", req.Method)
				}
				if !strings.HasSuffix(req.URL.Path, "/events") {
					t.Errorf("unexpected URL path: %s", req.URL.Path)
				}

				return newSSEResponse([]string{messageEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		select {
		case event := <-eventsCh:
			msgEvent, ok := event.(agentapisdk.EventMessageUpdate)
			if !ok {
				t.Fatalf("expected EventMessageUpdate, got %T", event)
			}
			assert.Equal(t, marshal(t, msgEvent), marshal(t, messageUpdate))
		case err := <-errCh:
			t.Fatalf("received unexpected error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})

	t.Run("successful subscription with status change", func(t *testing.T) {
		statusChange := agentapisdk.EventStatusChange{
			Status: agentapisdk.StatusStable,
		}
		statusJSON := marshal(t, statusChange)
		statusEvent := fmt.Sprintf("event: %s\ndata: %s", agentapisdk.EventTypeStatusChange, statusJSON)

		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse([]string{statusEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		select {
		case event := <-eventsCh:
			statusEvt, ok := event.(agentapisdk.EventStatusChange)
			if !ok {
				t.Fatalf("expected EventStatusChange, got %T", event)
			}
			assert.Equal(t, marshal(t, statusEvt), marshal(t, statusChange))
		case err := <-errCh:
			t.Fatalf("received unexpected error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})

	t.Run("multiple events", func(t *testing.T) {
		events := []agentapisdk.Event{
			agentapisdk.EventMessageUpdate{
				Id:      1,
				Message: "Hello, world!",
				Role:    agentapisdk.RoleAgent,
				Time:    time.Now(),
			},
			agentapisdk.EventStatusChange{
				Status: agentapisdk.StatusStable,
			},
			agentapisdk.EventMessageUpdate{
				Id:      2,
				Message: "Hello, world 2!",
				Role:    agentapisdk.RoleAgent,
				Time:    time.Now(),
			},
			agentapisdk.EventStatusChange{
				Status: agentapisdk.StatusRunning,
			},
		}
		eventsStrings := []string{}
		for _, event := range events {
			eventJSON, _ := json.Marshal(event)
			switch event.(type) {
			case agentapisdk.EventMessageUpdate:
				eventsStrings = append(eventsStrings, fmt.Sprintf("event: %s\ndata: %s", agentapisdk.EventTypeMessageUpdate, eventJSON))
			case agentapisdk.EventStatusChange:
				eventsStrings = append(eventsStrings, fmt.Sprintf("event: %s\ndata: %s", agentapisdk.EventTypeStatusChange, eventJSON))
			default:
				t.Fatalf("unknown event type: %T", event)
			}

		}

		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse(eventsStrings), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		receivedEvents := make([]agentapisdk.Event, 0, len(events))
		timeout := time.After(500 * time.Millisecond)

		for {
			select {
			case event, ok := <-eventsCh:
				if !ok {
					goto checkEvents
				}
				receivedEvents = append(receivedEvents, event)
				if len(receivedEvents) == len(events) {
					goto checkEvents
				}
			case err := <-errCh:
				if err != nil {
					t.Fatalf("received unexpected error: %v", err)
				}
			case <-timeout:
				goto checkEvents
			}
		}

	checkEvents:
		if len(receivedEvents) != len(events) {
			t.Fatalf("expected %d events, got %d", len(events), len(receivedEvents))
		}

		for i, event := range events {
			assert.Equal(t, marshal(t, receivedEvents[i]), marshal(t, event))
		}
	})

	t.Run("unknown event type", func(t *testing.T) {
		unknownEvent := "event: unknown_event\ndata: {\"foo\":\"bar\"}"

		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse([]string{unknownEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		select {
		case <-eventsCh:
			t.Fatal("expected error, got event")
		case err := <-errCh:
			if !strings.Contains(err.Error(), "unknown event type") {
				t.Errorf("expected 'unknown event type' error, got: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for error")
		}
	})

	t.Run("malformed event data", func(t *testing.T) {
		malformedEvent := fmt.Sprintf("event: %s\ndata: {invalid_json}", agentapisdk.EventTypeMessageUpdate)

		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse([]string{malformedEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		select {
		case <-eventsCh:
			t.Fatal("expected error, got event")
		case err := <-errCh:
			if !strings.Contains(err.Error(), "invalid") {
				t.Errorf("expected JSON unmarshal error, got: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for error")
		}
	})

	t.Run("server error response", func(t *testing.T) {
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newErrorResponse(http.StatusInternalServerError, "Server Error", "Internal server error"), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, _, err = client.SubscribeEvents(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to subscribe to events") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("client error", func(t *testing.T) {
		networkErr := errors.New("network error")
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return nil, networkErr
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, _, err = client.SubscribeEvents(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to subscribe to events") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
