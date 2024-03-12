package htevents

import "time"

var _ Message = (*Track)(nil)

// This type represents object sent in a track call as described in
// https://hightouch.com/docs/events/event-spec#track-events
type Track struct {
	// This field is exported for serialization purposes and shouldn't be set by
	// the application, its value is always overwritten by the library.
	Type string `json:"type,omitempty"`

	MessageId    string       `json:"messageId,omitempty"`
	AnonymousId  string       `json:"anonymousId,omitempty"`
	UserId       string       `json:"userId,omitempty"`
	Event        string       `json:"event"`
	Timestamp    time.Time    `json:"timestamp,omitempty"`
	Context      *Context     `json:"context,omitempty"`
	Properties   Properties   `json:"properties,omitempty"`
	Integrations Integrations `json:"integrations,omitempty"`
}

func (msg Track) Validate() error {
	if len(msg.Event) == 0 {
		return FieldError{
			Type:  "htevents.Track",
			Name:  "Event",
			Value: msg.Event,
		}
	}

	if len(msg.UserId) == 0 && len(msg.AnonymousId) == 0 {
		return FieldError{
			Type:  "htevents.Track",
			Name:  "UserId",
			Value: msg.UserId,
		}
	}

	return nil
}
