package questiontimeout

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "question",
		Category:    "timeout",
		Version:     "v1",
		Description: "Question timeout event when SLA is exceeded",
		Factory:     func() any { return &TimeoutEvent{} },
	}); err != nil {
		panic("failed to register TimeoutEvent: " + err.Error())
	}

	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "question",
		Category:    "escalation",
		Version:     "v1",
		Description: "Question escalation event when answer is routed to another answerer",
		Factory:     func() any { return &EscalationEvent{} },
	}); err != nil {
		panic("failed to register EscalationEvent: " + err.Error())
	}
}
