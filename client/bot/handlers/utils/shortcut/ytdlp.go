package shortcut

import (
	"github.com/celestix/gotgproto/dispatcher"
	"github.com/celestix/gotgproto/ext"
	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/krau/SaveAny-Bot/common/i18n"
	"github.com/krau/SaveAny-Bot/common/i18n/i18nk"
	"github.com/krau/SaveAny-Bot/common/msgedit"
	"github.com/krau/SaveAny-Bot/common/utils/tgutil"
	"github.com/krau/SaveAny-Bot/core"
	"github.com/krau/SaveAny-Bot/core/tasks/ytdlp"
	"github.com/krau/SaveAny-Bot/storage"
	"github.com/rs/xid"
)

func CreateAndAddYtdlpTaskWithEdit(ctx *ext.Context, stor storage.Storage, dirPath string, urls []string, flags []string, msgID int, userID int64) error {
	logger := log.FromContext(ctx)
	injectCtx := tgutil.ExtWithContext(ctx.Context, ctx)

	if len(urls) == 0 {
		logger.Error("URLs list is empty")
		tgutil.EditMessage(ctx, userID, &tg.MessagesEditMessageRequest{
			ID:      msgID,
			Message: i18n.T(i18nk.BotMsgYtdlpErrorNoValidUrls, nil),
		})
		return dispatcher.EndGroups
	}

	logger.Infof("Creating yt-dlp task for %d URL(s) with %d flag(s)", len(urls), len(flags))

	task := ytdlp.NewTask(
		xid.New().String(),
		injectCtx,
		urls,
		flags,
		stor,
		dirPath,
		ytdlp.NewProgress(msgID, userID),
	)

	injectCtx = tgutil.TaskNotification(injectCtx, task.TaskID(), msgedit.Key{ChatID: userID, MessageID: msgID})
	if err := core.AddTask(injectCtx, task); err != nil {
		logger.Errorf("Failed to add yt-dlp task: %s", err)
		tgutil.EditTaskMessage(injectCtx, userID, &tg.MessagesEditMessageRequest{
			ID: msgID,
			Message: i18n.T(i18nk.BotMsgCommonErrorTaskAddFailed, map[string]any{
				"Error": err.Error(),
			}),
		}, msgedit.Final)
		return dispatcher.EndGroups
	}

	tgutil.EditTaskMessage(injectCtx, userID, &tg.MessagesEditMessageRequest{
		ID:      msgID,
		Message: i18n.T(i18nk.BotMsgCommonInfoTaskAdded, nil),
	}, msgedit.Queued)

	return dispatcher.EndGroups
}
