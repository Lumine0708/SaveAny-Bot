package msgedit

import (
	"reflect"

	"github.com/gotd/td/tg"
)

// SDK Encode methods mutate presence flags, including on nested entities and
// keyboards. Copy the generated TL object tree without calling those methods on
// caller-owned objects. TL requests contain exported data fields only.
func cloneRequest(req *tg.MessagesEditMessageRequest) *tg.MessagesEditMessageRequest {
	owned := copyTL(reflect.ValueOf(req)).Interface().(*tg.MessagesEditMessageRequest)
	owned.SetFlags()
	if owned.Flags.Has(2) && owned.ReplyMarkup == nil {
		// Preserve an explicit clear until pending patches have been merged.
		owned.ReplyMarkup = &tg.ReplyInlineMarkup{}
	}
	return owned
}
func copyTL(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		result := reflect.New(v.Type().Elem())
		result.Elem().Set(copyTL(v.Elem()))
		return result
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		result := reflect.New(v.Type()).Elem()
		result.Set(copyTL(v.Elem()))
		return result
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		result := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			result.Index(i).Set(copyTL(v.Index(i)))
		}
		return result
	case reflect.Struct, reflect.Array:
		result := reflect.New(v.Type()).Elem()
		if v.Kind() == reflect.Struct {
			for i := 0; i < v.NumField(); i++ {
				result.Field(i).Set(copyTL(v.Field(i)))
			}
		} else {
			for i := 0; i < v.Len(); i++ {
				result.Index(i).Set(copyTL(v.Index(i)))
			}
		}
		return result
	default:
		return v
	}
}
func merge(old, next *tg.MessagesEditMessageRequest) *tg.MessagesEditMessageRequest {
	result := *next
	if !next.Flags.Has(11) && old.Flags.Has(11) {
		result.SetMessage(old.Message)
	}
	if !next.Flags.Has(14) && old.Flags.Has(14) {
		result.SetMedia(old.Media)
	}
	if !next.Flags.Has(2) && old.Flags.Has(2) {
		result.SetReplyMarkup(old.ReplyMarkup)
	}
	if !next.Flags.Has(3) && old.Flags.Has(3) {
		result.SetEntities(old.Entities)
	}
	if !next.Flags.Has(15) && old.Flags.Has(15) {
		result.SetScheduleDate(old.ScheduleDate)
	}
	if !next.Flags.Has(18) && old.Flags.Has(18) {
		result.SetScheduleRepeatPeriod(old.ScheduleRepeatPeriod)
	}
	if !next.Flags.Has(17) && old.Flags.Has(17) {
		result.SetQuickReplyShortcutID(old.QuickReplyShortcutID)
	}
	if !next.Flags.Has(23) && old.Flags.Has(23) {
		result.SetRichMessage(old.RichMessage)
	}
	// Preview flags belong to text/media edits; a keyboard-only patch preserves them.
	if !next.Flags.Has(11) && !next.Flags.Has(14) {
		result.SetNoWebpage(old.NoWebpage)
		result.SetInvertMedia(old.InvertMedia)
	}
	if result.Peer == nil {
		result.Peer = old.Peer
	}
	return &result
}
