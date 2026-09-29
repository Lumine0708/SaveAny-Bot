package batchtfile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/celestix/gotgproto/ext"
	"github.com/gotd/td/tg"
	"github.com/krau/SaveAny-Bot/common/msgedit"
	"github.com/krau/SaveAny-Bot/common/utils/ioutil"
	"github.com/krau/SaveAny-Bot/common/utils/tgutil"
	storconfig "github.com/krau/SaveAny-Bot/config/storage"
	"github.com/krau/SaveAny-Bot/storage/local"
)

func TestBlockedMessageEditDoesNotBlockBatchLocalCopyCallbacks(t *testing.T) {
	useProgressRegressionLocale(t)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	pool := msgedit.New(t.Context(), 1, 8, func(ctx context.Context, _ int64, _ *tg.MessagesEditMessageRequest) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	t.Cleanup(func() { pool.Close(time.Second) })
	botCtx := &ext.Context{Context: tgutil.WithEditor(t.Context(), pool)}
	ctx := tgutil.TaskNotification(tgutil.ExtWithContext(botCtx.Context, botCtx), "copy-task", msgedit.Key{ChatID: 1, MessageID: 2})
	progress := NewProgressTracker(2, 1)
	const size = 128 * 1024
	task := newProgressRegressionTask(progress, progressRegressionFile{"first", size}, progressRegressionFile{"second", size})
	for _, id := range []string{"first", "second"} {
		task.markItemActive(id, false, time.Now())
		task.recordDownloadComplete(id, size)
	}
	progress.OnStart(ctx, task)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("edit sender not entered")
	}
	root := t.TempDir()
	store := &local.Local{}
	if err := store.Init(ctx, &storconfig.LocalStorageConfig{BasePath: root}); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("a"), size)
	done := make(chan error, 1)
	go func() {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, id := range []string{"first", "second"} {
			wg.Go(func() {
				reader := ioutil.NewProgressReader(bytes.NewReader(data), size, task.uploadCallback(ctx, id))
				errs <- store.Save(ctx, reader, id+".bin")
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				done <- err
				return
			}
		}
		progress.OnDone(ctx, task, nil)
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("local copy or progress callback blocked on message edit")
	}
	for _, id := range []string{"first", "second"} {
		got, err := os.ReadFile(filepath.Join(root, id+".bin"))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal(fmt.Sprintf("copy %s: bytes=%d error=%v", id, len(got), err))
		}
	}
	close(release)
}
