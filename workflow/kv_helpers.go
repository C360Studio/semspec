package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/nats-io/nats.go/jetstream"
)

// WaitForKVBucket retries opening a KV bucket until it exists or ctx is cancelled.
// Components that watch a bucket owned by another component use this to handle
// start-order races. Should move to natsclient as a framework primitive.
func WaitForKVBucket(ctx context.Context, js jetstream.JetStream, bucket string) (jetstream.KeyValue, error) {
	return retry.DoWithResult(ctx, retry.Config{
		MaxAttempts:  30,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   1.5,
	}, func() (jetstream.KeyValue, error) {
		kv, err := js.KeyValue(ctx, bucket)
		if err != nil {
			return nil, fmt.Errorf("bucket %s: %w", bucket, err)
		}
		return kv, nil
	})
}
