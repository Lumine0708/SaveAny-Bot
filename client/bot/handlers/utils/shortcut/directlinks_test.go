package shortcut

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"

	"github.com/krau/SaveAny-Bot/common/i18n"
	"github.com/krau/SaveAny-Bot/common/i18n/i18nk"
	"github.com/krau/SaveAny-Bot/common/msgedit"
	"github.com/krau/SaveAny-Bot/common/utils/tgutil"
	"github.com/krau/SaveAny-Bot/core"
	"github.com/krau/SaveAny-Bot/storage"
)

type queuedDirectLinkStorage struct{ storage.Storage }

func (queuedDirectLinkStorage) Name() string { return "queued-test" }

func TestCreateAndAddDirectTaskWithEditQueuedCancellation(t *testing.T) {
	i18n.Init("zh-Hans")
	t.Cleanup(func() { i18n.Init("zh-Hans") })
	const chatID, messageID = 123, 42
	type edit struct {
		chat int64
		req  *tg.MessagesEditMessageRequest
	}
	sent := make(chan edit, 2)
	pool := msgedit.New(t.Context(), 1, 4, func(_ context.Context, chat int64, req *tg.MessagesEditMessageRequest) error {
		sent <- edit{chat, req}
		return nil
	})
	t.Cleanup(func() { pool.Close(time.Second) })
	ctx := &ext.Context{Context: tgutil.WithEditor(t.Context(), pool)}
	// Leave core workers unstarted: exercise admission and cancellation without
	// executing the download or contacting Telegram or storage.
	if err := CreateAndAddDirectTaskWithEdit(ctx, queuedDirectLinkStorage{}, "", []string{"https://example.invalid/file"}, messageID, chatID); !errors.Is(err, dispatcher.EndGroups) {
		t.Fatalf("create task: %v", err)
	}
	queued := core.GetQueuedTasks(ctx)
	if len(queued) != 1 {
		t.Fatalf("queued tasks = %d, want 1", len(queued))
	}
	taskID := queued[0].ID
	t.Cleanup(func() {
		if err := core.CancelTask(ctx, taskID); err != nil {
			t.Errorf("cleanup queued task: %v", err)
		}
	})
	checkEdit := func(want string) {
		t.Helper()
		select {
		case got := <-sent:
			if got.chat != chatID || got.req.ID != messageID || got.req.Message != want {
				t.Errorf("edit = (%d, %d, %q), want (%d, %d, %q)", got.chat, got.req.ID, got.req.Message, chatID, messageID, want)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("edit %q not sent", want)
		}
	}
	checkEdit(i18n.T(i18nk.BotMsgCommonInfoTaskAdded, nil))
	if err := core.CancelTask(ctx, taskID); err != nil {
		t.Fatalf("cancel queued task: %v", err)
	}
	cancelled := i18n.T(i18nk.BotMsgProgressTaskCanceledWithId, map[string]any{"TaskID": taskID})
	tgutil.CancelNotification(ctx, taskID, i18n.T(i18nk.BotMsgCancelInfoCancellingTask, nil), cancelled)
	checkEdit(cancelled)
	if got := core.GetLength(ctx); got != 0 {
		t.Fatalf("active queued tasks after cancellation = %d, want 0", got)
	}
}
