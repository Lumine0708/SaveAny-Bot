package tgutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/krau/SaveAny-Bot/common/msgedit"
)

type editorKey struct{}
type notificationKey struct{}

// Editor shares the Bot pool. The registry contains only live file tasks; the
// task context retains its lifecycle after removal so late callbacks stay sealed.
type Editor struct {
	Pool  *msgedit.Pool
	mu    sync.Mutex
	tasks map[string]*notification
}
type notification struct {
	life    *msgedit.Lifecycle
	editor  *Editor
	mu      sync.Mutex
	chat    int64
	message int
}

func WithEditor(ctx context.Context, pool *msgedit.Pool) context.Context {
	e := &Editor{Pool: pool, tasks: make(map[string]*notification)}
	context.AfterFunc(ctx, func() { e.mu.Lock(); clear(e.tasks); e.mu.Unlock() })
	return context.WithValue(ctx, editorKey{}, e)
}
func editorFrom(ctx context.Context) *Editor {
	if e, ok := ctx.Value(editorKey{}).(*Editor); ok {
		return e
	}
	if ext := ExtFromContext(ctx); ext != nil {
		e, _ := ext.Value(editorKey{}).(*Editor)
		return e
	}
	return nil
}

// EditMessage submits an ordinary interaction patch; success means admission,
// not delivery. The worker owns the request copy and reports eventual RPC errors.
func EditMessage(ctx *ext.Context, chat int64, req *tg.MessagesEditMessageRequest) (msgedit.Result, error) {
	e := editorFrom(ctx)
	if e == nil {
		return msgedit.Closed, nil
	}
	return e.Pool.Submit(chat, req, nil, msgedit.Progress)
}

// TaskNotification must run before AddTask. It does not mutate the shared ext
// context and preserves the existing task cancellation and first Reply boundary.
func TaskNotification(ctx context.Context, id string, target ...msgedit.Key) context.Context {
	e := editorFrom(ctx)
	if e == nil {
		return ctx
	}
	n := &notification{life: msgedit.NewLifecycle(id), editor: e}
	if len(target) > 0 {
		n.chat, n.message = target[0].ChatID, target[0].MessageID
	}
	e.mu.Lock()
	e.tasks[id] = n
	e.mu.Unlock()
	return context.WithValue(ctx, notificationKey{}, n)
}
func StartNotification(ctx context.Context) {
	if n, ok := ctx.Value(notificationKey{}).(*notification); ok {
		n.editor.Pool.Started(n.life)
	}
}
func ForgetNotification(ctx context.Context) {
	if n, ok := ctx.Value(notificationKey{}).(*notification); ok {
		n.editor.Pool.Seal(n.life)
		n.editor.mu.Lock()
		delete(n.editor.tasks, n.life.TaskID)
		n.editor.mu.Unlock()
	}
}
func EditTaskMessage(ctx context.Context, chat int64, req *tg.MessagesEditMessageRequest, phase msgedit.Phase) (msgedit.Result, error) {
	if req == nil {
		return msgedit.Invalid, fmt.Errorf("nil task edit request")
	}
	if req.ID <= 0 || chat == 0 {
		return msgedit.Invalid, fmt.Errorf("invalid edit target")
	}
	if ExtFromContext(ctx) == nil {
		return msgedit.Closed, nil
	}
	n, ok := ctx.Value(notificationKey{}).(*notification)
	if !ok {
		return msgedit.Closed, nil
	}
	n.mu.Lock()
	n.chat, n.message = chat, req.ID
	n.mu.Unlock()
	if phase == msgedit.Final {
		// Task snapshots replace all display fields, including empty entities and buttons.
		copy := *req
		copy.SetEntities(req.Entities)
		copy.SetReplyMarkup(&tg.ReplyInlineMarkup{})
		req = &copy
	}
	result, err := n.editor.Pool.Submit(chat, req, n.life, phase)
	if phase == msgedit.Final {
		ForgetNotification(ctx)
	}
	return result, err
}

// CancelNotification follows successful file-task cancellation. Queued tasks
// will never call OnDone, so their final notification is submitted here.
func CancelNotification(ctx context.Context, id, cancelling, cancelled string) {
	e := editorFrom(ctx)
	if e == nil {
		return
	}
	e.mu.Lock()
	n := e.tasks[id]
	e.mu.Unlock()
	if n == nil {
		return
	}
	started := e.Pool.Cancel(n.life)
	n.mu.Lock()
	chat, message := n.chat, n.message
	n.mu.Unlock()
	if message == 0 {
		if !started {
			e.mu.Lock()
			delete(e.tasks, id)
			e.mu.Unlock()
		}
		return
	}
	phase, text := msgedit.Cancelling, cancelling
	if !started {
		phase, text = msgedit.Final, cancelled
	}
	req := &tg.MessagesEditMessageRequest{ID: message}
	req.SetMessage(text)
	req.SetEntities(nil)
	req.SetReplyMarkup(&tg.ReplyInlineMarkup{})
	if _, err := e.Pool.Submit(chat, req, n.life, phase); err != nil {
		log.FromContext(ctx).Error("Failed to submit cancellation edit", "task", id, "error", err)
	}
	if !started {
		e.mu.Lock()
		delete(e.tasks, id)
		e.mu.Unlock()
	}
}

// WithNotificationBot carries only the Bot notification service into a watched
// task; its userbot context continues to serve downloads and storage.
func WithNotificationBot(ctx context.Context, botCtx *ext.Context) context.Context {
	if botCtx == nil {
		return ctx
	}
	if e := editorFrom(botCtx); e != nil {
		return context.WithValue(ctx, editorKey{}, e)
	}
	return ctx
}
