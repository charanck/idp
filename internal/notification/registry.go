package notification

import "controlplane/internal/notification/provider"

// ChannelRegistry maps a notification channel name to its delivery Channel.
type ChannelRegistry map[string]provider.Channel

// NewChannelRegistry returns the registry wired to the skeleton
// Email/SMS/WhatsApp channels plus InApp.
func NewChannelRegistry() ChannelRegistry {
	return ChannelRegistry{
		ChannelEmail:    provider.EmailChannel{},
		ChannelSMS:      provider.SMSChannel{},
		ChannelWhatsApp: provider.WhatsAppChannel{},
		ChannelInApp:    provider.InAppChannel{},
	}
}
