package tgutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"
	"github.com/krau/SaveAny-Bot/common/msgedit"
)

func awaitEdit(t *testing.T, ch <-chan *tg.MessagesEditMessageRequest) *tg.MessagesEditMessageRequest {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("edit not sent")
		return nil
	}
}
func TestCancelledTaskFinalUsesBotContextAndSealsLateUpdates(t *testing.T) {
	sent := make(chan *tg.MessagesEditMessageRequest, 8)
	pool := msgedit.New(t.Context(), 1, 4, func(ctx context.Context, _ int64, r *tg.MessagesEditMessageRequest) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sender got cancelled task context: %w", err)
		}
		sent <- r
		return nil
	})
	t.Cleanup(func() { pool.Close(time.Second) })
	botBase := WithEditor(t.Context(), pool)
	handlerCtx, cancelHandler := context.WithCancel(botBase)
	extCtx := &ext.Context{Context: handlerCtx}
	taskBase := TaskNotification(ExtWithContext(handlerCtx, extCtx), "task", msgedit.Key{ChatID: 1, MessageID: 2})
	taskCtx, cancelTask := context.WithCancel(taskBase)
	StartNotification(taskCtx)
	cancelTask()
	cancelHandler()
	req := &tg.MessagesEditMessageRequest{ID: 2, Message: "cancelled"}
	if got, err := EditTaskMessage(taskCtx, 1, req, msgedit.Final); got != msgedit.Accepted || err != nil {
		t.Fatalf("final: %s %v", got, err)
	}
	got := awaitEdit(t, sent)
	if got.Message != "cancelled" || got.ReplyMarkup != nil || got.Flags.Has(2) || !got.Flags.Has(3) {
		t.Fatalf("final snapshot: %+v", got)
	}
	if req.ReplyMarkup != nil || req.Flags.Has(3) {
		t.Fatal("caller request mutated")
	}
	if extCtx.Context != handlerCtx {
		t.Fatal("handler ext context mutated")
	}
	for _, phase := range []msgedit.Phase{msgedit.Queued, msgedit.Progress, msgedit.Cancelling} {
		if result, _ := EditTaskMessage(taskCtx, 1, &tg.MessagesEditMessageRequest{ID: 2, Message: "late"}, phase); result != msgedit.Expired {
			t.Fatalf("late phase %d: %s", phase, result)
		}
	}
	CancelNotification(extCtx, "task", "cancelling", "cancelled")
	e := editorFrom(botBase)
	e.mu.Lock()
	count := len(e.tasks)
	e.mu.Unlock()
	if count != 0 {
		t.Fatalf("completed tasks retained: %d", count)
	}
}
func TestQueuedCancelBeforeAddedAndRunningCancel(t *testing.T) {
	sent := make(chan *tg.MessagesEditMessageRequest, 8)
	pool := msgedit.New(t.Context(), 1, 4, func(_ context.Context, _ int64, r *tg.MessagesEditMessageRequest) error { sent <- r; return nil })
	t.Cleanup(func() { pool.Close(time.Second) })
	base := WithEditor(t.Context(), pool)
	botCtx := &ext.Context{Context: base}
	for _, started := range []bool{false, true} {
		ctx := TaskNotification(ExtWithContext(base, botCtx), "task", msgedit.Key{ChatID: 1, MessageID: 2})
		if started {
			StartNotification(ctx)
		}
		CancelNotification(botCtx, "task", "cancelling", "cancelled")
		got := awaitEdit(t, sent).Message
		want := "cancelled"
		if started {
			want = "cancelling"
		}
		if got != want {
			t.Fatalf("got %s want %s", got, want)
		}
		if result, _ := EditTaskMessage(ctx, 1, &tg.MessagesEditMessageRequest{ID: 2, Message: "added late"}, msgedit.Queued); result != msgedit.Expired {
			t.Fatal(result)
		}
		if started {
			if result, _ := EditTaskMessage(ctx, 1, &tg.MessagesEditMessageRequest{ID: 2, Message: "cancelled final"}, msgedit.Final); result != msgedit.Accepted {
				t.Fatal(result)
			}
			awaitEdit(t, sent)
		}
	}
	e := editorFrom(base)
	e.mu.Lock()
	count := len(e.tasks)
	e.mu.Unlock()
	if count != 0 {
		t.Fatal("cancelled tasks retained")
	}
}
func TestWatchedTaskUsesBotEditorAndPreservesUserbot(t *testing.T) {
	sent := make(chan *tg.MessagesEditMessageRequest, 1)
	pool := msgedit.New(t.Context(), 1, 4, func(_ context.Context, _ int64, r *tg.MessagesEditMessageRequest) error { sent <- r; return nil })
	t.Cleanup(func() { pool.Close(time.Second) })
	botCtx := &ext.Context{Context: WithEditor(t.Context(), pool)}
	userCtx := &ext.Context{Context: t.Context()}
	ctx := TaskNotification(WithNotificationBot(ExtWithContext(t.Context(), userCtx), botCtx), "watch")
	if ExtFromContext(ctx) != userCtx {
		t.Fatal("downloader context replaced")
	}
	progressCtx := ExtWithContext(ctx, botCtx)
	if result, err := EditTaskMessage(progressCtx, 1, &tg.MessagesEditMessageRequest{ID: 2, Message: "watch done"}, msgedit.Final); result != msgedit.Accepted || err != nil {
		t.Fatalf("watch: %s %v", result, err)
	}
	awaitEdit(t, sent)
	if result, _ := EditTaskMessage(t.Context(), 1, &tg.MessagesEditMessageRequest{ID: 2, Message: "API"}, msgedit.Progress); result != msgedit.Closed {
		t.Fatal("API task acquired a Telegram editor")
	}
}

func TestInvalidTaskEditPreservesQueuedCancellationTarget(t *testing.T) {
	for _, tt := range []struct {
		name string
		chat int64
		req  *tg.MessagesEditMessageRequest
	}{
		{"nil request", 1, nil},
		{"missing message ID", 1, &tg.MessagesEditMessageRequest{Message: "queued"}},
		{"negative message ID", 1, &tg.MessagesEditMessageRequest{ID: -1, Message: "queued"}},
		{"missing chat ID", 0, &tg.MessagesEditMessageRequest{ID: 2, Message: "queued"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sent := make(chan *tg.MessagesEditMessageRequest, 1)
			pool := msgedit.New(t.Context(), 1, 4, func(_ context.Context, chat int64, req *tg.MessagesEditMessageRequest) error {
				if chat != 1 {
					t.Errorf("cancellation chat = %d, want 1", chat)
				}
				sent <- req
				return nil
			})
			t.Cleanup(func() { pool.Close(time.Second) })
			base := WithEditor(t.Context(), pool)
			botCtx := &ext.Context{Context: base}
			ctx := TaskNotification(ExtWithContext(base, botCtx), "task", msgedit.Key{ChatID: 1, MessageID: 2})
			if result, err := EditTaskMessage(ctx, tt.chat, tt.req, msgedit.Queued); result != msgedit.Invalid || err == nil {
				t.Fatalf("invalid edit = %s, %v", result, err)
			}
			CancelNotification(botCtx, "task", "cancelling", "cancelled")
			got := awaitEdit(t, sent)
			if got.ID != 2 || got.Message != "cancelled" {
				t.Fatalf("cancellation = (%d, %q), want (2, cancelled)", got.ID, got.Message)
			}
		})
	}
}
