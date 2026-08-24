package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Hub is a thin wrapper around Redis pub/sub for delivering real-time
// notification events to SSE subscribers, on channel "notifications:{user_id}".
// Publishing is fire-and-forget: if nobody's subscribed (or Redis is briefly
// unavailable) the event is simply lost, not queued or retried - SSE is a
// best-effort live feed, not a delivery channel in its own right, and is
// never persisted to the notifications table like ChannelEmail/SMS/WhatsApp/
// InApp are.
type Hub struct {
	rdb *redis.Client
}

func NewHub(rdb *redis.Client) *Hub {
	return &Hub{rdb: rdb}
}

func sseChannelName(userID string) string {
	return "notifications:" + userID
}

// SentEvent is the JSON payload published on a notification's SSE channel.
type SentEvent struct {
	ID       string `json:"id"`
	Channel  string `json:"channel"`
	Status   string `json:"status"`
	Provider string `json:"provider,omitempty"`
}

// recipientUserID extracts an optional "user_id" field from a notification's
// Recipient jsonb blob, used to address the SSE channel to publish to.
func recipientUserID(recipient []byte) string {
	var fields map[string]any
	if err := json.Unmarshal(recipient, &fields); err != nil {
		return ""
	}
	userID, _ := fields["user_id"].(string)
	return userID
}

// PublishSent publishes a delivery notice for n on its recipient's SSE
// channel. No-op if the recipient doesn't carry a "user_id" field.
func (h *Hub) PublishSent(ctx context.Context, n *Notification) error {
	userID := recipientUserID(n.Recipient)
	if userID == "" {
		return nil
	}

	provider := ""
	if n.Provider != nil {
		provider = *n.Provider
	}
	payload, err := json.Marshal(SentEvent{ID: n.ID.String(), Channel: n.Channel, Status: n.Status, Provider: provider})
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}

	if err := h.rdb.Publish(ctx, sseChannelName(userID), payload).Err(); err != nil {
		return fmt.Errorf("publish sse event: %w", err)
	}
	return nil
}

// Subscribe subscribes to a user's SSE channel. The caller must Close() the
// returned *redis.PubSub when done.
func (h *Hub) Subscribe(ctx context.Context, userID string) *redis.PubSub {
	return h.rdb.Subscribe(ctx, sseChannelName(userID))
}
