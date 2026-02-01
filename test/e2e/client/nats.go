// Package client provides test clients for e2e scenarios.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/c360/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSClient provides NATS operations for e2e tests.
type NATSClient struct {
	client *natsclient.Client
	nc     *nats.Conn
	js     jetstream.JetStream
	closed bool
	mu     sync.Mutex
}

// NewNATSClient creates a new NATS client for e2e testing.
func NewNATSClient(ctx context.Context, natsURL string) (*NATSClient, error) {
	client, err := natsclient.NewClient(natsURL,
		natsclient.WithName("semspec-e2e"),
		natsclient.WithMaxReconnects(5),
		natsclient.WithReconnectWait(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create NATS client: %w", err)
	}

	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.WaitForConnection(connCtx); err != nil {
		return nil, fmt.Errorf("NATS connection timeout: %w", err)
	}

	nc := client.GetConnection()
	js, err := client.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get JetStream context: %w", err)
	}

	return &NATSClient{
		client: client,
		nc:     nc,
		js:     js,
	}, nil
}

// Close closes the NATS client.
func (c *NATSClient) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	return c.client.Close(ctx)
}

// Publish publishes a message to a subject.
// Note: NATS Publish is synchronous and doesn't support context cancellation directly,
// but we check context before publishing.
func (c *NATSClient) Publish(ctx context.Context, subject string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled before publish: %w", err)
	}
	return c.nc.Publish(subject, data)
}

// PublishJSON publishes a JSON-encoded message to a subject.
func (c *NATSClient) PublishJSON(ctx context.Context, subject string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return c.Publish(ctx, subject, data)
}

// Request sends a request and waits for a response using context for timeout.
func (c *NATSClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (*nats.Msg, error) {
	// Create a context with the shorter of the provided timeout or existing deadline
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.nc.RequestWithContext(reqCtx, subject, data)
}

// Subscribe subscribes to a subject pattern.
func (c *NATSClient) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	return c.nc.Subscribe(subject, handler)
}

// QueueSubscribe creates a queue subscription.
func (c *NATSClient) QueueSubscribe(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error) {
	return c.nc.QueueSubscribe(subject, queue, handler)
}

// MessageCapture helps capture messages from a subject for validation.
type MessageCapture struct {
	sub      *nats.Subscription
	messages []*nats.Msg
	mu       sync.Mutex
}

// CaptureMessages starts capturing messages from a subject.
// The caller MUST call Stop() when done to prevent goroutine leaks.
func (c *NATSClient) CaptureMessages(subject string) (*MessageCapture, error) {
	capture := &MessageCapture{
		messages: make([]*nats.Msg, 0),
	}

	sub, err := c.nc.Subscribe(subject, func(msg *nats.Msg) {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		capture.messages = append(capture.messages, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe to %s: %w", subject, err)
	}

	capture.sub = sub
	return capture, nil
}

// Messages returns a copy of all captured messages.
func (mc *MessageCapture) Messages() []*nats.Msg {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	result := make([]*nats.Msg, len(mc.messages))
	copy(result, mc.messages)
	return result
}

// Count returns the number of captured messages.
func (mc *MessageCapture) Count() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return len(mc.messages)
}

// WaitForCount waits until the specified number of messages are captured.
func (mc *MessageCapture) WaitForCount(ctx context.Context, count int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if mc.Count() >= count {
				return nil
			}
		}
	}
}

// Stop stops capturing messages.
func (mc *MessageCapture) Stop() error {
	if mc.sub != nil {
		return mc.sub.Unsubscribe()
	}
	return nil
}

// UserMessage represents a user message sent to semspec commands.
type UserMessage struct {
	MessageID   string    `json:"message_id"`
	ChannelType string    `json:"channel_type"`
	ChannelID   string    `json:"channel_id"`
	UserID      string    `json:"user_id"`
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
}

// UserResponse represents a response from semspec commands.
type UserResponse struct {
	ResponseID  string    `json:"response_id"`
	ChannelType string    `json:"channel_type"`
	ChannelID   string    `json:"channel_id"`
	UserID      string    `json:"user_id"`
	Type        string    `json:"type"`
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
}

// SendCommand sends a command message and returns the response.
func (c *NATSClient) SendCommand(ctx context.Context, channelType, channelID, userID, content string, timeout time.Duration) (*UserResponse, error) {
	msg := UserMessage{
		MessageID:   fmt.Sprintf("e2e-%d", time.Now().UnixNano()),
		ChannelType: channelType,
		ChannelID:   channelID,
		UserID:      userID,
		Content:     content,
		Timestamp:   time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	subject := fmt.Sprintf("user.message.%s.%s", channelType, channelID)
	resp, err := c.Request(ctx, subject, data, timeout)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", subject, err)
	}

	var response UserResponse
	if err := json.Unmarshal(resp.Data, &response); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &response, nil
}

// JetStreamContext returns the JetStream context for advanced operations.
func (c *NATSClient) JetStreamContext() jetstream.JetStream {
	return c.js
}

// IsConnected returns true if the client is connected to NATS.
func (c *NATSClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.nc.IsConnected()
}
