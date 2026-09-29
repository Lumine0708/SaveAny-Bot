package msgedit

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type sentEdit struct {
	chat int64
	req  *tg.MessagesEditMessageRequest
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for worker")
		var zero T
		return zero
	}
}
func submit(t *testing.T, p *Pool, chat int64, id int, text string, l *Lifecycle, phase Phase) Result {
	t.Helper()
	result, err := p.Submit(chat, &tg.MessagesEditMessageRequest{ID: id, Message: text}, l, phase)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func waitIdle(t *testing.T, p *Pool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		idle := len(p.pending) == 0 && len(p.active) == 0
		p.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pool did not release pending/active records")
}
func TestLatestFinalAndSameMessageSerial(t *testing.T) {
	entered := make(chan sentEdit, 8)
	release := make(chan struct{})
	p := New(t.Context(), 2, 8, func(ctx context.Context, chat int64, r *tg.MessagesEditMessageRequest) error {
		entered <- sentEdit{chat, r}
		if r.Message == "10%" {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	t.Cleanup(func() { p.Close(time.Second) })
	life := NewLifecycle("task")
	submit(t, p, 1, 10, "10%", life, Progress)
	receive(t, entered)
	for _, v := range []string{"20%", "80%"} {
		if got := submit(t, p, 1, 10, v, life, Progress); got != Accepted && got != Merged {
			t.Fatalf("submit = %s", got)
		}
	}
	submit(t, p, 1, 10, "done", life, Final)
	for _, phase := range []Phase{Queued, Progress, Cancelling, Final} {
		if got := submit(t, p, 1, 10, "late", life, phase); got != Expired {
			t.Fatalf("late %d = %s", phase, got)
		}
	}
	// A different chat with the same message ID can use the other worker.
	submit(t, p, 2, 10, "other", NewLifecycle("other"), Final)
	if got := receive(t, entered); got.chat != 2 {
		t.Fatalf("same message ran concurrently: %+v", got)
	}
	select {
	case v := <-entered:
		t.Fatalf("unexpected intermediate: %+v", v)
	default:
	}
	close(release)
	if got := receive(t, entered); got.req.Message != "done" {
		t.Fatalf("final = %q", got.req.Message)
	}
	waitIdle(t, p)
	if got := submit(t, p, 1, 10, "late after cleanup", life, Progress); got != Expired {
		t.Fatal(got)
	}
	// Explicitly creating a new lifecycle permits message reuse.
	submit(t, p, 1, 10, "new task", NewLifecycle("new"), Final)
	receive(t, entered)
}

func TestDrainedMessageReuseExpiresOldLifecycle(t *testing.T) {
	for _, phase := range []Phase{Progress, Final} {
		t.Run(map[Phase]string{Progress: "new progress", Final: "new terminal"}[phase], func(t *testing.T) {
			entered := make(chan sentEdit, 8)
			p := New(t.Context(), 1, 8, func(_ context.Context, chat int64, r *tg.MessagesEditMessageRequest) error {
				entered <- sentEdit{chat, r}
				return nil
			})
			t.Cleanup(func() { p.Close(time.Second) })
			old, newer := NewLifecycle("old"), NewLifecycle("newer")
			other := NewLifecycle("other chat")
			for _, entry := range []struct {
				chat  int64
				life  *Lifecycle
				phase Phase
			}{{1, old, Progress}, {2, other, Progress}, {1, newer, phase}} {
				if got := submit(t, p, entry.chat, 8, entry.life.TaskID, entry.life, entry.phase); got != Accepted {
					t.Fatalf("submit %s = %s", entry.life.TaskID, got)
				}
				receive(t, entered)
				waitIdle(t, p)
			}
			// Both sends have drained; neither pending nor active can reject A.
			for _, late := range []Phase{Progress, Final} {
				if got := submit(t, p, 1, 8, "old late", old, late); got != Expired {
					t.Fatalf("old late phase %d = %s, want expired", late, got)
				}
			}
			// Cleaning up the old task must not release the newer task's ownership.
			p.Seal(old)
			if phase == Progress {
				p.mu.Lock()
				owner := p.owners[Key{1, 8}]
				p.mu.Unlock()
				if owner != newer {
					t.Fatal("old task cleanup released newer ownership")
				}
				if got := submit(t, p, 1, 8, "new done", newer, Final); got != Accepted {
					t.Fatal(got)
				}
				if got := receive(t, entered); got.req.Message != "new done" {
					t.Fatalf("unexpected edit: %+v", got)
				}
				waitIdle(t, p)
			}
			if got := submit(t, p, 2, 8, "other done", other, Final); got != Accepted {
				t.Fatalf("unrelated chat expired: %s", got)
			}
			if got := receive(t, entered); got.chat != 2 || got.req.Message != "other done" {
				t.Fatalf("unexpected edit: %+v", got)
			}
			waitIdle(t, p)
			p.mu.Lock()
			size := len(p.owners)
			p.mu.Unlock()
			if size != 0 {
				t.Fatalf("completed tasks retained %d owners", size)
			}
			select {
			case got := <-entered:
				t.Fatalf("stale edit sent: %+v", got)
			default:
			}
		})
	}
}

func TestSealReleasesDrainedOwnership(t *testing.T) {
	for _, sealBeforeDrain := range []bool{false, true} {
		t.Run(map[bool]string{false: "after drain", true: "before drain"}[sealBeforeDrain], func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			p := New(t.Context(), 1, 1, func(ctx context.Context, _ int64, _ *tg.MessagesEditMessageRequest) error {
				entered <- struct{}{}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			t.Cleanup(func() { p.Close(time.Second) })
			life := NewLifecycle("task")
			submit(t, p, 1, 1, "progress", life, Progress)
			receive(t, entered)
			if sealBeforeDrain {
				p.Seal(life)
			}
			close(release)
			waitIdle(t, p)
			if !sealBeforeDrain {
				p.Seal(life)
			}
			p.mu.Lock()
			size := len(p.owners)
			p.mu.Unlock()
			if size != 0 {
				t.Fatalf("sealed task retained %d owners", size)
			}
			if got := submit(t, p, 1, 1, "late", life, Progress); got != Expired {
				t.Fatal(got)
			}
		})
	}
}
func TestPendingPatchesAndImmutableOwnership(t *testing.T) {
	entered := make(chan sentEdit, 8)
	release := make(chan struct{})
	p := New(t.Context(), 1, 8, func(ctx context.Context, chat int64, r *tg.MessagesEditMessageRequest) error {
		entered <- sentEdit{chat, r}
		if r.ID == 1 {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	t.Cleanup(func() { p.Close(time.Second) })
	submit(t, p, 1, 1, "block", nil, Progress)
	receive(t, entered)
	entity := &tg.MessageEntityBold{Length: 3}
	button := &tg.KeyboardButtonCallback{Text: "button", Data: []byte("original")}
	req := &tg.MessagesEditMessageRequest{ID: 2, Message: "old", Entities: []tg.MessageEntityClass{entity}, NoWebpage: true}
	if _, err := p.Submit(1, req, nil, Progress); err != nil {
		t.Fatal(err)
	}
	markup := &tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: []tg.KeyboardButtonClass{button}}}}
	if _, err := p.Submit(1, &tg.MessagesEditMessageRequest{ID: 2, ReplyMarkup: markup}, nil, Progress); err != nil {
		t.Fatal(err)
	}
	req.Message = "mutated"
	entity.Length = 999
	button.Data[0] = 'X'
	markup.Rows = nil
	clearReq := &tg.MessagesEditMessageRequest{ID: 3}
	clearReq.SetMessage("before")
	clearReq.SetEntities([]tg.MessageEntityClass{&tg.MessageEntityItalic{Length: 6}})
	clearReq.SetReplyMarkup(&tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: []tg.KeyboardButtonClass{&tg.KeyboardButton{Text: "old"}}}}})
	p.Submit(1, clearReq, nil, Progress)
	clearReq = &tg.MessagesEditMessageRequest{ID: 3}
	clearReq.SetMessage("")
	clearReq.SetEntities(nil)
	clearReq.SetReplyMarkup(nil)
	if _, err := p.Submit(1, clearReq, nil, Progress); err != nil {
		t.Fatal(err)
	}
	close(release)
	cleared := receive(t, entered).req
	if cleared.ID != 3 || !cleared.Flags.Has(11) || cleared.Message != "" || !cleared.Flags.Has(3) || len(cleared.Entities) != 0 || !cleared.Flags.Has(2) || len(cleared.ReplyMarkup.(*tg.ReplyInlineMarkup).Rows) != 0 {
		t.Fatalf("explicit clear lost: %+v", cleared)
	}
	got := receive(t, entered).req
	if got.ID != 2 || got.Message != "old" || got.Entities[0].(*tg.MessageEntityBold).Length != 3 || !got.NoWebpage {
		t.Fatalf("patch lost or mutated: %+v", got)
	}
	if data := got.ReplyMarkup.(*tg.ReplyInlineMarkup).Rows[0].Buttons[0].(*tg.KeyboardButtonCallback).Data; string(data) != "original" {
		t.Fatalf("keyboard ownership: %s", data)
	}
}
func TestCapacityNewestTasksAndTerminalEviction(t *testing.T) {
	for _, allFinal := range []bool{false, true} {
		t.Run(map[bool]string{false: "progress first", true: "all terminals"}[allFinal], func(t *testing.T) {
			var logs bytes.Buffer
			ctx := log.WithContext(t.Context(), log.New(&logs))
			entered := make(chan int, 8)
			release := make(chan struct{})
			p := New(ctx, 1, 2, func(ctx context.Context, _ int64, r *tg.MessagesEditMessageRequest) error {
				entered <- r.ID
				if r.ID == 1 {
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			})
			t.Cleanup(func() { p.Close(time.Second) })
			submit(t, p, 1, 1, "block", nil, Progress)
			receive(t, entered)
			first, second, newest := NewLifecycle("first"), NewLifecycle("second"), NewLifecycle("newest")
			submit(t, p, 1, 2, "old final", first, Final)
			phase := Progress
			if allFinal {
				phase = Final
			}
			submit(t, p, 1, 3, "old", second, phase)
			if got := submit(t, p, 1, 4, "new", newest, Progress); got != Accepted {
				t.Fatal(got)
			}
			evicted := second
			if allFinal {
				evicted = first
			}
			for range 100 {
				if got := submit(t, p, 1, 8, "revive", evicted, Progress); got != Expired {
					t.Fatal(got)
				}
			}
			p.mu.Lock()
			size := len(p.pending)
			p.mu.Unlock()
			if size != 2 {
				t.Fatalf("pending=%d", size)
			}
			close(release)
			if got := receive(t, entered); got != 4 {
				t.Fatalf("new task not preferred: %d", got)
			}
			want := 2
			if allFinal {
				want = 3
			}
			if got := receive(t, entered); got != want {
				t.Fatalf("wrong victim: got %d want %d", got, want)
			}
			waitIdle(t, p)
			p.Seal(newest)
			p.mu.Lock()
			owners := len(p.owners)
			p.mu.Unlock()
			if owners != 0 {
				t.Fatalf("eviction or completion retained %d owners", owners)
			}
			if !strings.Contains(logs.String(), "Message edit evicted") || strings.Count(logs.String(), "Message edit evicted") != 1 {
				t.Fatalf("eviction log missing or spammed: %s", logs.String())
			}
		})
	}
}
func TestConcurrencyShutdownAndFinalErrors(t *testing.T) {
	entered := make(chan struct{}, 8)
	var active, maximum atomic.Int32
	p := New(t.Context(), 2, 16, func(ctx context.Context, _ int64, _ *tg.MessagesEditMessageRequest) error {
		count := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); count > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, count) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	for i := 1; i <= 10; i++ {
		submit(t, p, 1, i, "progress", NewLifecycle("task"), Progress)
	}
	receive(t, entered)
	receive(t, entered)
	if !p.Close(time.Second) {
		t.Fatal("workers did not stop")
	}
	if active.Load() != 0 || maximum.Load() != 2 {
		t.Fatalf("active=%d maximum=%d", active.Load(), maximum.Load())
	}
	if got := submit(t, p, 1, 11, "late", nil, Progress); got != Closed {
		t.Fatal(got)
	}
	p.mu.Lock()
	pending := len(p.pending)
	owners := len(p.owners)
	p.mu.Unlock()
	if pending != 0 || owners != 0 {
		t.Fatalf("shutdown retained pending=%d owners=%d", pending, owners)
	}
	var mu sync.Mutex
	var logs bytes.Buffer
	ctx := log.WithContext(t.Context(), log.New(&logs))
	calls := make(chan struct{}, 4)
	q := New(ctx, 1, 4, func(_ context.Context, _ int64, r *tg.MessagesEditMessageRequest) error {
		mu.Lock()
		defer mu.Unlock()
		calls <- struct{}{}
		if r.ID == 1 {
			return tgerr.New(400, "MESSAGE_NOT_MODIFIED")
		}
		return errors.New("message deleted")
	})
	submit(t, q, 1, 1, "same", nil, Progress)
	receive(t, calls)
	waitIdle(t, q)
	submit(t, q, 1, 2, "gone", nil, Progress)
	receive(t, calls)
	waitIdle(t, q)
	q.Close(time.Second)
	if strings.Count(logs.String(), "Message edit failed") != 1 {
		t.Fatalf("unexpected failure logging: %s", logs.String())
	}
}
func TestCancellationInvalidatesPendingProgress(t *testing.T) {
	entered := make(chan string, 4)
	release := make(chan struct{})
	p := New(t.Context(), 1, 4, func(ctx context.Context, _ int64, r *tg.MessagesEditMessageRequest) error {
		entered <- r.Message
		if r.Message == "block" {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	t.Cleanup(func() { p.Close(time.Second) })
	submit(t, p, 1, 1, "block", nil, Progress)
	receive(t, entered)
	life := NewLifecycle("cancelled")
	submit(t, p, 1, 2, "queued", life, Queued)
	if p.Cancel(life) {
		t.Fatal("queued task reported as started")
	}
	for _, phase := range []Phase{Queued, Progress} {
		if submit(t, p, 1, 2, "late", life, phase) != Expired {
			t.Fatal("late progress accepted")
		}
	}
	submit(t, p, 1, 2, "cancelled", life, Final)
	close(release)
	if got := receive(t, entered); got != "cancelled" {
		t.Fatal(got)
	}
}

func TestConcurrentSubmissionsDoNotMutateSDKObjects(t *testing.T) {
	button := &tg.KeyboardButtonCallback{RequiresPassword: true, Text: "button", Data: []byte("data")}
	req := &tg.MessagesEditMessageRequest{ID: 1, Message: "text", ReplyMarkup: &tg.ReplyInlineMarkup{Rows: []tg.KeyboardButtonRow{{Buttons: []tg.KeyboardButtonClass{button}}}}}
	failures := make(chan error, 100)
	p := New(t.Context(), 2, 8, func(_ context.Context, _ int64, r *tg.MessagesEditMessageRequest) error {
		// This is the same mutation performed by peer resolution and SDK encoding.
		r.Peer = &tg.InputPeerEmpty{}
		var buffer bin.Buffer
		if err := r.Encode(&buffer); err != nil {
			failures <- err
		}
		return nil
	})
	t.Cleanup(func() { p.Close(time.Second) })
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := p.Submit(1, req, nil, Progress); err != nil {
				failures <- err
			}
		})
	}
	wg.Wait()
	waitIdle(t, p)
	if req.Peer != nil || !req.Flags.Zero() || !button.Flags.Zero() {
		t.Fatal("SDK mutated shared caller objects")
	}
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
}
