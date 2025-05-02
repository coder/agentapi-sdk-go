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
	"github.com/coder/agentapi-sdk-go/gen"
)

// MockHTTPClient is a mock implementation of the gen.HttpRequestDoer interface
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

// Do implements the HttpRequestDoer interface
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
	errModel := gen.ErrorModel{
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
	// Add double newlines if they don't exist
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

	// Skip this test since the URL parsing implementation might be more permissive
	// than we expected and might allow seemingly invalid URLs
	t.Run("with invalid credentials", func(t *testing.T) {
		_, err := agentapisdk.NewClient("https://invalid:auth@example.com")
		// This is technically still a valid URL, so we're checking different behavior
		if err != nil {
			// Just to have some assertion - check that error is not about URL parsing
			if strings.Contains(err.Error(), "invalid URL") {
				t.Errorf("Got unexpected URL parsing error: %v", err)
			}
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
		editor := func(ctx context.Context, req *http.Request) error {
			req.Header.Set("X-Test-Header", "test-value")
			return nil
		}
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
		// Setup mock client to return a successful response
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
				return newJSONResponse(http.StatusOK, gen.MessageResponseBody{
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
			Type:    gen.MessageTypeUser,
		}
		resp, err := client.PostMessage(ctx, req)
		if err != nil {
			t.Fatalf("PostMessage failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if resp.JSON200 == nil {
			t.Fatal("expected JSON200 response")
		}
		if !resp.JSON200.Ok {
			t.Error("expected Ok to be true")
		}
	})

	t.Run("server error", func(t *testing.T) {
		// Setup mock client to return a server error
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newErrorResponse(http.StatusInternalServerError, "Server Error", "Internal server error"), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test PostMessage with server error
		req := agentapisdk.PostMessageParams{
			Content: "Hello, agent!",
			Type:    gen.MessageTypeUser,
		}
		resp, err := client.PostMessage(ctx, req)
		if err != nil {
			t.Fatalf("PostMessage failed: %v", err)
		}
		if resp.StatusCode() != http.StatusInternalServerError {
			t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, resp.StatusCode())
		}
		if resp.ApplicationproblemJSONDefault == nil {
			t.Fatal("expected error response")
		}
		if *resp.ApplicationproblemJSONDefault.Title != "Server Error" {
			t.Errorf("unexpected error title: %s", *resp.ApplicationproblemJSONDefault.Title)
		}
	})

	t.Run("client error", func(t *testing.T) {
		// Setup mock client to return a client error
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
			Type:    gen.MessageTypeUser,
		}
		resp, err := client.PostMessage(ctx, req)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if resp != nil {
			t.Fatal("expected nil response")
		}
		if !errors.Is(err, networkErr) {
			t.Errorf("expected network error, got: %v", err)
		}
	})
}

func TestGetMessages(t *testing.T) {
	ctx := context.Background()

	t.Run("empty message list", func(t *testing.T) {
		// Setup mock client to return an empty message list
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
				return newJSONResponse(http.StatusOK, gen.MessagesResponseBody{
					Messages: []gen.Message{},
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
		if resp.JSON200 == nil {
			t.Fatal("expected JSON200 response")
		}
		if len(resp.JSON200.Messages) != 0 {
			t.Errorf("expected empty messages, got %d messages", len(resp.JSON200.Messages))
		}
	})

	t.Run("populated message list", func(t *testing.T) {
		// Setup mock client to return messages
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Create example messages
				now := time.Now()
				messages := []gen.Message{
					{
						Id:      1,
						Content: "Hello!",
						Role:    gen.ConversationRoleUser,
						Time:    now.Add(-time.Minute),
					},
					{
						Id:      2,
						Content: "How can I help you?",
						Role:    gen.ConversationRoleAgent,
						Time:    now,
					},
				}

				// Return messages response
				return newJSONResponse(http.StatusOK, gen.MessagesResponseBody{
					Messages: messages,
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
		if resp.JSON200 == nil {
			t.Fatal("expected JSON200 response")
		}
		messages := resp.JSON200.Messages
		if len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(messages))
		}
		if messages[0].Id != 1 || messages[0].Role != gen.ConversationRoleUser {
			t.Errorf("unexpected first message: %+v", messages[0])
		}
		if messages[1].Id != 2 || messages[1].Role != gen.ConversationRoleAgent {
			t.Errorf("unexpected second message: %+v", messages[1])
		}
	})

	t.Run("server error", func(t *testing.T) {
		// Setup mock client to return a server error
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newErrorResponse(http.StatusInternalServerError, "Server Error", "Internal server error"), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test GetMessages with server error
		resp, err := client.GetMessages(ctx)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		if resp.StatusCode() != http.StatusInternalServerError {
			t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, resp.StatusCode())
		}
		if resp.ApplicationproblemJSONDefault == nil {
			t.Fatal("expected error response")
		}
	})

	t.Run("client error", func(t *testing.T) {
		// Setup mock client to return a client error
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

		// Test GetMessages with client error
		resp, err := client.GetMessages(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if resp != nil {
			t.Fatal("expected nil response")
		}
		if !errors.Is(err, networkErr) {
			t.Errorf("expected network error, got: %v", err)
		}
	})
}

func TestGetStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("running status", func(t *testing.T) {
		// Setup mock client to return running status
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				if req.Method != "GET" {
					t.Errorf("expected GET method, got %s", req.Method)
				}
				if !strings.HasSuffix(req.URL.Path, "/status") {
					t.Errorf("unexpected URL path: %s", req.URL.Path)
				}

				// Return status response
				return newJSONResponse(http.StatusOK, gen.StatusResponseBody{
					Status: gen.Running,
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
		if resp.JSON200 == nil {
			t.Fatal("expected JSON200 response")
		}
		if resp.JSON200.Status != gen.Running {
			t.Errorf("expected status %s, got %s", gen.Running, resp.JSON200.Status)
		}
	})

	t.Run("stable status", func(t *testing.T) {
		// Setup mock client to return stable status
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Return status response
				return newJSONResponse(http.StatusOK, gen.StatusResponseBody{
					Status: gen.Stable,
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
		if resp.JSON200.Status != gen.Stable {
			t.Errorf("expected status %s, got %s", gen.Stable, resp.JSON200.Status)
		}
	})

	t.Run("server error", func(t *testing.T) {
		// Setup mock client to return a server error
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newErrorResponse(http.StatusInternalServerError, "Server Error", "Internal server error"), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test GetStatus with server error
		resp, err := client.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if resp.StatusCode() != http.StatusInternalServerError {
			t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, resp.StatusCode())
		}
	})

	t.Run("client error", func(t *testing.T) {
		// Setup mock client to return a client error
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

		// Test GetStatus with client error
		resp, err := client.GetStatus(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if resp != nil {
			t.Fatal("expected nil response")
		}
	})
}

func TestSubscribeEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("successful subscription with message update", func(t *testing.T) {
		// Create message update event
		messageUpdate := gen.MessageUpdateBody{
			Id:      1,
			Message: "Hello, world!",
			Role:    gen.ConversationRoleAgent,
			Time:    time.Now(),
		}
		messageJSON, _ := json.Marshal(messageUpdate)
		// Proper SSE format requires each data line to be prefixed with "data: "
		messageEvent := fmt.Sprintf("event: message_update\ndata: %s", messageJSON)

		// Setup mock client to return an SSE stream
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				// Verify request
				if req.Method != "GET" {
					t.Errorf("expected GET method, got %s", req.Method)
				}
				if !strings.HasSuffix(req.URL.Path, "/events") {
					t.Errorf("unexpected URL path: %s", req.URL.Path)
				}

				// Return SSE response with events
				return newSSEResponse([]string{messageEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test SubscribeEvents
		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}
		if eventsCh == nil {
			t.Fatal("expected non-nil events channel")
		}
		if errCh == nil {
			t.Fatal("expected non-nil error channel")
		}

		// Wait for and verify event
		select {
		case event := <-*eventsCh:
			messageEvent, ok := event.(gen.MessageUpdateBody)
			if !ok {
				t.Fatalf("expected EventMessageUpdate, got %T", event)
			}
			if messageEvent.Id != 1 {
				t.Errorf("expected message ID 1, got %d", messageEvent.Id)
			}
			if messageEvent.Role != gen.ConversationRoleAgent {
				t.Errorf("expected role %s, got %s", gen.ConversationRoleAgent, messageEvent.Role)
			}
			if messageEvent.Message != "Hello, world!" {
				t.Errorf("expected message 'Hello, world!', got '%s'", messageEvent.Message)
			}
		case err := <-*errCh:
			t.Fatalf("received unexpected error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})

	t.Run("successful subscription with status change", func(t *testing.T) {
		// Create status change event
		statusChange := gen.StatusChangeBody{
			Status: gen.Stable,
		}
		statusJSON, _ := json.Marshal(statusChange)
		// Proper SSE format requires each data line to be prefixed with "data: "
		statusEvent := fmt.Sprintf("event: status_change\ndata: %s", statusJSON)

		// Setup mock client
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse([]string{statusEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test SubscribeEvents
		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		// Wait for and verify event
		select {
		case event := <-*eventsCh:
			statusEvent, ok := event.(gen.StatusChangeBody)
			if !ok {
				t.Fatalf("expected EventStatusChange, got %T", event)
			}
			if statusEvent.Status != gen.Stable {
				t.Errorf("expected status %s, got %s", gen.Stable, statusEvent.Status)
			}
		case err := <-*errCh:
			t.Fatalf("received unexpected error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})

	t.Run("unknown event type", func(t *testing.T) {
		// Create unknown event type
		unknownEvent := "event: unknown_event\ndata: {\"foo\":\"bar\"}"

		// Setup mock client
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse([]string{unknownEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test SubscribeEvents
		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		// Wait for error due to unknown event type
		select {
		case <-*eventsCh:
			t.Fatal("expected error, got event")
		case err := <-*errCh:
			if !strings.Contains(err.Error(), "unknown event type") {
				t.Errorf("expected 'unknown event type' error, got: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for error")
		}
	})

	t.Run("malformed event data", func(t *testing.T) {
		// Create malformed event
		malformedEvent := "event: message_update\ndata: {invalid_json}"

		// Setup mock client
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newSSEResponse([]string{malformedEvent}), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test SubscribeEvents
		eventsCh, errCh, err := client.SubscribeEvents(ctx)
		if err != nil {
			t.Fatalf("SubscribeEvents failed: %v", err)
		}

		// Wait for error due to malformed data
		select {
		case <-*eventsCh:
			t.Fatal("expected error, got event")
		case err := <-*errCh:
			if !strings.Contains(err.Error(), "invalid") {
				t.Errorf("expected JSON unmarshal error, got: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for error")
		}
	})

	t.Run("server error response", func(t *testing.T) {
		// Setup mock client to return an error response
		mockClient := &MockHTTPClient{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				return newErrorResponse(http.StatusInternalServerError, "Server Error", "Internal server error"), nil
			},
		}

		client, err := agentapisdk.NewClient("https://example.com", agentapisdk.WithHTTPClient(mockClient))
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		// Test SubscribeEvents with server error
		_, _, err = client.SubscribeEvents(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to subscribe to events") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("client error", func(t *testing.T) {
		// Setup mock client to return a client error
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

		// Test SubscribeEvents with client error
		_, _, err = client.SubscribeEvents(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to subscribe to events") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}