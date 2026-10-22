package agent

import "context"

// ImageInputStatus describes this sender's actual request shape, including its
// fixed reasoning choice. Unknown leaves non-product callers' policy intact.
type ImageInputStatus struct {
	Known   bool
	Allowed bool
	Reason  string
}

type ImageInputQualifier interface {
	ImageInputStatus(context.Context, string, bool) ImageInputStatus
}

func SenderImageInputStatus(ctx context.Context, sender Sender, model string, withTools bool) ImageInputStatus {
	if qualified, ok := sender.(ImageInputQualifier); ok {
		return qualified.ImageInputStatus(ctx, model, withTools)
	}
	return ImageInputStatus{}
}

type imageInputContextKey struct{}

func withSenderImageInput(ctx context.Context, sender Sender, model string, withTools bool) context.Context {
	return WithImageInputPolicy(ctx, func(ctx context.Context) ImageInputStatus {
		return SenderImageInputStatus(ctx, sender, model, withTools)
	})
}

// WithImageInputPolicy carries the sender's qualification to tool executors;
// tools depend on agent, so the agent never needs to import the tools package.
func WithImageInputPolicy(ctx context.Context, policy func(context.Context) ImageInputStatus) context.Context {
	return context.WithValue(ctx, imageInputContextKey{}, policy)
}

func ImageInputForContext(ctx context.Context) ImageInputStatus {
	if policy, ok := ctx.Value(imageInputContextKey{}).(func(context.Context) ImageInputStatus); ok {
		return policy(ctx)
	}
	return ImageInputStatus{}
}
