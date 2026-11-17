// SPDX-License-Identifier: Apache-2.0
package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asterfield-dev/maply/internal/card"
	"github.com/asterfield-dev/maply/internal/db"
	"github.com/asterfield-dev/maply/internal/i18n"
)

// JobWarmup sends a group's warm-up result. The bot plans it when it welcomes
// a group that has no day left to play.
const JobWarmup = "warmup"

// WarmupFor is how long a warm-up takes answers. It starts when maply joins
// a group after the group's reveal, so the result comes the same evening, or
// a little after midnight, and well before the next morning's post.
const WarmupFor = time.Hour

// warmup sends the warm-up's result: the archive day's place and the
// group's table of it. The place is of a day over in every zone, which the
// days page shows to anyone, so it is no secret here. Nobody who played
// means nothing to say: the job ends without a message, and the group's
// next message is the morning post.
func (s *Scheduler) warmup(ctx context.Context, chatID int64) error {
	c, ok, err := s.chatOf(ctx, chatID)
	if err != nil || !ok {
		return orSkip(err)
	}
	w, ok, err := s.Store.WarmupOf(ctx, c.ID)
	if err != nil || !ok {
		return orSkip(err)
	}
	if w.ResultMsgID != 0 {
		return nil
	}
	if now := s.now(); now.Before(w.ResultAt) {
		return laterError{w.ResultAt}
	}
	guesses, err := s.Store.WarmupGuesses(ctx, c.ID)
	if err != nil {
		return err
	}
	if len(guesses) == 0 {
		return errSkip
	}
	rd, ok, err := s.Store.RoundOn(ctx, w.Day)
	if err != nil || !ok {
		return orSkip(err)
	}
	answer, err := s.Store.Answer(ctx, rd.PlaceID)
	if err != nil {
		return err
	}
	lang := i18n.Resolve(c.Lang)
	msg, err := s.sendPhoto(ctx, c.ID, rd, warmupCaption(lang, rd.DayNo, answer, guesses, fmt.Sprintf("%02d:%02d", c.PostHour, c.PostMin), revealAt(lang, c)), nil)
	if err != nil {
		return err
	}
	return s.Store.SetWarmupResult(ctx, c.ID, msg.MessageID)
}

// warmupCaption is the warm-up's result: the head of a day's result marked
// as the warm-up, the table, and when the real game begins.
func warmupCaption(lang string, dayNo int, answer db.Answer, guesses []db.Guess, post, at string) string {
	head := card.Quoted(lang, dayNo, answer)
	head = strings.Replace(head, "</b>", "</b> · "+i18n.T(lang, "warmup.title"), 1)
	lines := table(lang, guesses, nil)
	lines = append(lines, "", "<i>"+i18n.T(lang, "warmup.after", "post", post, "time", at)+"</i>")
	return fitCaption(head, lines)
}
