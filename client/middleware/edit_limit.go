package middleware

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/krau/SaveAny-Bot/client/middleware/recovery"
	"golang.org/x/time/rate"
)

// NewBotMiddlewares puts the shared edit limiter inside recovery, retry and
// SimpleWaiter, so every retry consumes a token. Userbot RPCs are unchanged.
func NewBotMiddlewares(ctx context.Context, timeout time.Duration) []telegram.Middleware {
	chain := NewDefaultMiddlewares(ctx, timeout)
	// Existing recovery sleeps use a plain backoff. Bind Bot edit recovery to
	// the worker context too, so stopping the pool interrupts that sleep.
	original := chain[0]
	chain[0] = telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		unchanged := original.Handle(next)
		return func(callCtx context.Context, input bin.Encoder, output bin.Decoder) error {
			if _, ok := input.(*tg.MessagesEditMessageRequest); !ok {
				return unchanged(callCtx, input, output)
			}
			recoverEdit := recovery.New(ctx, func() backoff.BackOff {
				return backoff.WithContext(newBackoff(timeout), callCtx)
			})
			return recoverEdit.Handle(next)(callCtx, input, output)
		}
	})
	return append(chain, newEditLimiter(rate.NewLimiter(rate.Every(time.Second), 1)))
}
func newEditLimiter(limiter *rate.Limiter) telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			if _, ok := input.(*tg.MessagesEditMessageRequest); ok {
				if err := limiter.Wait(ctx); err != nil {
					return err
				}
			}
			return next.Invoke(ctx, input, output)
		}
	})
}
