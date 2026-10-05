package notification

import (
	model "controlplane/internal/model/notification"
	"controlplane/internal/notification/provider"
)

// ChannelRegistry maps a notification channel name to its delivery Channel.
type ChannelRegistry map[string]provider.Channel

// NewChannelRegistry returns the registry wired to the concrete
// delivery channels supported by the app.
func NewChannelRegistry() ChannelRegistry {
	return ChannelRegistry{
		model.ChannelEmail: provider.EmailChannel{},
		model.ChannelInApp: provider.InAppChannel{},
	}
}
