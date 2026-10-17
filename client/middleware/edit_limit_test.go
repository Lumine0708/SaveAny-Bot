package middleware

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/clock"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/krau/SaveAny-Bot/client/middleware/retry"
	"golang.org/x/time/rate"
)

type fastWaitClock struct {
	clock.Clock
	waits chan time.Duration
}

func (c fastWaitClock) Timer(d time.Duration) clock.Timer {
	c.waits <- d
	return clock.System.Timer(time.Millisecond)
}
func wrapInvokers(base tg.Invoker, chain []telegram.Middleware) tg.Invoker {
	for i := len(chain) - 1; i >= 0; i-- {
		base = chain[i].Handle(base)
	}
	return base
}
func TestBotEditLimiterCoversWaiterRetryAndRecovery(t *testing.T) {
	chain := NewBotMiddlewares(t.Context(), time.Second*3)
	if len(chain) != 4 {
		t.Fatalf("unexpected middleware chain: %d", len(chain))
	}
	waiter, ok := chain[2].(*floodwait.SimpleWaiter)
	if !ok {
		t.Fatalf("waiter moved: %T", chain[2])
	}
	waits := make(chan time.Duration, 1)
	chain[2] = waiter.WithClock(fastWaitClock{clock.System, waits})
	// Config is not initialized in offline middleware tests; use its production
	// retry implementation with an explicit retry budget and a short edit interval.
	chain[1] = retry.New(5)
	chain[3] = newEditLimiter(rate.NewLimiter(rate.Every(25*time.Millisecond), 1))
	var mu sync.Mutex
	var attempts []time.Time
	base := telegram.InvokeFunc(func(_ context.Context, input bin.Encoder, _ bin.Decoder) error {
		if _, ok := input.(*tg.MessagesEditMessageRequest); !ok {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		attempts = append(attempts, time.Now())
		switch len(attempts) {
		case 1:
			return tgerr.New(420, "FLOOD_WAIT_239")
		case 2:
			return tgerr.New(500, "RPC_CALL_FAIL")
		case 3:
			return errors.New("disconnected")
		}
		return nil
	})
	invoke := wrapInvokers(base, chain)
	if err := invoke.Invoke(t.Context(), &tg.MessagesEditMessageRequest{ID: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if d := <-waits; d != 239*time.Second {
		t.Fatalf("waiter duration=%s", d)
	}
	if err := invoke.Invoke(t.Context(), &tg.MessagesEditMessageRequest{ID: 2}, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != 5 {
		t.Fatalf("attempts=%d", len(attempts))
	}
	for i := 1; i < len(attempts); i++ {
		if gap := attempts[i].Sub(attempts[i-1]); gap < 20*time.Millisecond {
			t.Fatalf("attempt %d bypassed limiter: %s", i, gap)
		}
	}
	// Non-edit calls do not wait for an edit token, even with a canceled context.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := invoke.Invoke(ctx, &tg.MessagesGetHistoryRequest{}, nil); err != nil {
		t.Fatalf("non-edit limited: %v", err)
	}
}
func TestBotEditCancellationInterruptsAllWaits(t *testing.T) {
	for _, kind := range []string{"limiter", "flood wait", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			chain := NewBotMiddlewares(t.Context(), time.Minute)
			chain[1] = retry.New(5)
			limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
			if kind == "limiter" {
				limiter.Allow()
			}
			chain[3] = newEditLimiter(limiter)
			entered := make(chan struct{}, 1)
			base := telegram.InvokeFunc(func(context.Context, bin.Encoder, bin.Decoder) error {
				entered <- struct{}{}
				if kind == "flood wait" {
					return tgerr.New(420, "FLOOD_WAIT_239")
				}
				return errors.New("disconnected")
			})
			result := make(chan error, 1)
			go func() { result <- wrapInvokers(base, chain).Invoke(ctx, &tg.MessagesEditMessageRequest{}, nil) }()
			if kind != "limiter" {
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("invoker not entered")
				}
			}
			time.Sleep(20 * time.Millisecond) // let the outer middleware enter its wait
			cancel()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("cancel returned success")
				}
			case <-time.After(200 * time.Millisecond):
				t.Fatal("cancellation did not interrupt wait")
			}
		})
	}
}
