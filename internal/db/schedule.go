// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/FreshLabDev/maply/internal/round"
)

// Chat is one place the day's messages go: a group maply is in, or the DM of
// a person who has talked to maply there.
type Chat struct {
	ID   int64
	Kind string // "dm" or "group"
	// Zone is the chat's pick, empty while it follows its language.
	Zone string
	// Lang is core's language for the group, or for the person of a DM.
	Lang     string
	PostHour int
	PostMin  int
	Intrigue bool
	Mentions bool
}

// ActiveChats lists every chat the day is sent to. A DM counts once the person
// has been seen there and until they block maply.
func (s *Store) ActiveChats(ctx context.Context) ([]Chat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cs.chat_id, 'group', COALESCE(cs.timezone, ''),
		       COALESCE(core.effective_language(0, cs.chat_id, 'chat'), ''),
		       (EXTRACT(HOUR FROM cs.post_time) * 60 + EXTRACT(MINUTE FROM cs.post_time))::int, cs.intrigue, cs.mentions
		  FROM chat_settings cs
		 WHERE cs.kind = 'group' AND cs.active
		UNION ALL
		SELECT p.telegram_user_id, 'dm', COALESCE(cs.timezone, ''),
		       COALESCE(core.effective_language(p.telegram_user_id, NULL, 'user'), ''),
		       (EXTRACT(HOUR FROM COALESCE(cs.post_time, '07:00')) * 60 + EXTRACT(MINUTE FROM COALESCE(cs.post_time, '07:00')))::int, COALESCE(cs.intrigue, true), COALESCE(cs.mentions, true)
		  FROM core.maply_presence p
		  LEFT JOIN chat_settings cs ON cs.chat_id = p.telegram_user_id
		 WHERE p.chat_id = 0 AND COALESCE(cs.active, true)`)
	if err != nil {
		return nil, fmt.Errorf("active chats: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Chat, error) {
		var c Chat
		var post int
		err := row.Scan(&c.ID, &c.Kind, &c.Zone, &c.Lang, &post, &c.Intrigue, &c.Mentions)
		c.PostHour, c.PostMin = post/60, post%60
		return c, err
	})
}

// EnsureChat makes the settings row a chat's rounds hang off. A DM gets one
// the first time maply sends there.
func (s *Store) EnsureChat(ctx context.Context, chatID int64) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind) VALUES ($1, $2)
		ON CONFLICT (chat_id) DO NOTHING`, chatID, kindOf(chatID)); err != nil {
		return fmt.Errorf("ensure chat %d: %w", chatID, err)
	}
	return nil
}

// Job is one scheduled send.
type Job struct {
	ID       int64
	Key      string
	Kind     string
	ChatID   int64
	Date     round.Date
	RunAt    time.Time
	Attempts int
}

// PlanJob queues a send unless its key is already done or given up. A job
// still waiting for its first attempt moves to runAt: an admin may have moved
// the morning post.
func (s *Store) PlanJob(ctx context.Context, kind string, chatID int64, d round.Date, runAt time.Time) error {
	key := fmt.Sprintf("%s:%s:%d", kind, d, chatID)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO job (key, kind, chat_id, date, run_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key) DO UPDATE SET run_at = EXCLUDED.run_at
		 WHERE job.status = 'pending' AND job.attempts = 0 AND job.run_at <> EXCLUDED.run_at`, key, kind, chatID, d.Time(), runAt); err != nil {
		return fmt.Errorf("plan %s: %w", key, err)
	}
	return nil
}

// ClaimJobs takes up to limit due jobs and leases them for lease. A job whose
// lease ran out, because the process died in the middle of it, is due again.
func (s *Store) ClaimJobs(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Job, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE job SET status = 'running', attempts = attempts + 1, locked_until = $2
		 WHERE id IN (SELECT id FROM job
		               WHERE (status = 'pending' AND run_at <= $1)
		                  OR (status = 'running' AND locked_until < $1)
		               ORDER BY run_at
		               LIMIT $3
		               FOR UPDATE SKIP LOCKED)
		RETURNING id, key, kind, chat_id, date, run_at, attempts`, now, now.Add(lease), limit)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Job, error) {
		var j Job
		var d time.Time
		err := row.Scan(&j.ID, &j.Key, &j.Kind, &j.ChatID, &d, &j.RunAt, &j.Attempts)
		j.Date = round.DateIn(d, time.UTC)
		return j, err
	})
}

// FinishJob closes a job as done, skipped or failed.
func (s *Store) FinishJob(ctx context.Context, id int64, status, lastErr string) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE job SET status = $2, last_error = NULLIF($3, ''), locked_until = NULL,
		               done_at = CASE WHEN $2 = 'done' THEN now() END
		 WHERE id = $1`, id, status, lastErr); err != nil {
		return fmt.Errorf("finish job %d: %w", id, err)
	}
	return nil
}

// RetryJob puts a failed attempt back in the queue for at.
func (s *Store) RetryJob(ctx context.Context, id int64, at time.Time, lastErr string) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE job SET status = 'pending', run_at = $2, last_error = $3, locked_until = NULL
		 WHERE id = $1`, id, at, lastErr); err != nil {
		return fmt.Errorf("retry job %d: %w", id, err)
	}
	return nil
}

// ChatRound is what has already been sent to a chat for a date.
type ChatRound struct {
	PostMsgID    int64
	Pinned       bool
	IntrigueSent bool
	IntrigueKind string
	ResultMsgID  int64
	Answered     string
	AnsweredAt   time.Time
}

// ChatRound returns the chat's state for d, creating the row the first time.
func (s *Store) ChatRound(ctx context.Context, chatID int64, d round.Date) (ChatRound, error) {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_round (chat_id, date) VALUES ($1, $2) ON CONFLICT DO NOTHING`, chatID, d.Time()); err != nil {
		return ChatRound{}, fmt.Errorf("chat round %d %s: %w", chatID, d, err)
	}
	var cr ChatRound
	var post, result *int64
	var answered *string
	var answeredAt *time.Time
	var kind *string
	err := s.pool.QueryRow(ctx, `
		SELECT post_msg_id, pinned, intrigue_sent, intrigue_kind, result_msg_id, answered, answered_at
		  FROM chat_round WHERE chat_id = $1 AND date = $2`, chatID, d.Time()).
		Scan(&post, &cr.Pinned, &cr.IntrigueSent, &kind, &result, &answered, &answeredAt)
	if err != nil {
		return ChatRound{}, fmt.Errorf("chat round %d %s: %w", chatID, d, err)
	}
	cr.PostMsgID, cr.ResultMsgID = deref(post), deref(result)
	if answered != nil {
		cr.Answered = *answered
	}
	if answeredAt != nil {
		cr.AnsweredAt = *answeredAt
	}
	if kind != nil {
		cr.IntrigueKind = *kind
	}
	return cr, nil
}

// LastIntrigue is the kind of intrigue the chat got on the date before d.
func (s *Store) LastIntrigue(ctx context.Context, chatID int64, d round.Date) (string, error) {
	var kind *string
	err := s.pool.QueryRow(ctx, `SELECT intrigue_kind FROM chat_round WHERE chat_id = $1 AND date = $2`,
		chatID, d.AddDays(-1).Time()).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) || kind == nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("last intrigue of %d: %w", chatID, err)
	}
	return *kind, nil
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (s *Store) updateChatRound(ctx context.Context, chatID int64, d round.Date, set string, args ...any) error {
	all := append([]any{chatID, d.Time()}, args...)
	if _, err := s.pool.Exec(ctx, `UPDATE chat_round SET `+set+` WHERE chat_id = $1 AND date = $2`, all...); err != nil {
		return fmt.Errorf("chat round %d %s: %w", chatID, d, err)
	}
	return nil
}

func (s *Store) SetPost(ctx context.Context, chatID int64, d round.Date, msgID int64, line string, at time.Time) error {
	return s.updateChatRound(ctx, chatID, d, `post_msg_id = $3, answered = $4, answered_at = $5`, msgID, line, at)
}

func (s *Store) SetPinned(ctx context.Context, chatID int64, d round.Date, pinned bool) error {
	return s.updateChatRound(ctx, chatID, d, `pinned = $3`, pinned)
}

func (s *Store) SetIntrigueSent(ctx context.Context, chatID int64, d round.Date, kind string, at time.Time) error {
	return s.updateChatRound(ctx, chatID, d, `intrigue_sent = true, intrigue_kind = $3, intrigue_at = $4`, kind, at)
}

func (s *Store) SetResult(ctx context.Context, chatID int64, d round.Date, msgID int64) error {
	return s.updateChatRound(ctx, chatID, d, `result_msg_id = $3`, msgID)
}

func (s *Store) SetAnswered(ctx context.Context, chatID int64, d round.Date, line string, at time.Time) error {
	return s.updateChatRound(ctx, chatID, d, `answered = $3, answered_at = $4`, line, at)
}

// OpenPost is a group's morning post that is still waiting for the result.
type OpenPost struct {
	ChatID     int64
	Date       round.Date
	PostMsgID  int64
	Answered   string
	AnsweredAt time.Time
}

// OpenPosts lists the groups' posts of the last two dates that have no result
// yet: the ones whose who-answered line may need redrawing.
func (s *Store) OpenPosts(ctx context.Context, since round.Date) ([]OpenPost, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cr.chat_id, cr.date, cr.post_msg_id, COALESCE(cr.answered, ''), COALESCE(cr.answered_at, 'epoch')
		  FROM chat_round cr
		  JOIN chat_settings cs ON cs.chat_id = cr.chat_id
		 WHERE cs.kind = 'group' AND cs.active AND cr.date >= $1
		   AND cr.post_msg_id IS NOT NULL AND cr.result_msg_id IS NULL`, since.Time())
	if err != nil {
		return nil, fmt.Errorf("open posts: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (OpenPost, error) {
		var p OpenPost
		var d time.Time
		err := row.Scan(&p.ChatID, &d, &p.PostMsgID, &p.Answered, &p.AnsweredAt)
		p.Date = round.DateIn(d, time.UTC)
		return p, err
	})
}

func (s *Store) PhotoFileID(ctx context.Context, d round.Date) (string, error) {
	var id *string
	if err := s.pool.QueryRow(ctx, `SELECT photo_file_id FROM round WHERE date = $1`, d.Time()).Scan(&id); err != nil {
		return "", fmt.Errorf("photo of %s: %w", d, err)
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

func (s *Store) SetPhotoFileID(ctx context.Context, d round.Date, id string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE round SET photo_file_id = $2 WHERE date = $1`, d.Time(), id); err != nil {
		return fmt.Errorf("photo of %s: %w", d, err)
	}
	return nil
}

// WarnPin reports whether the admins still have to hear that the post is
// better pinned, and marks that they have.
func (s *Store) WarnPin(ctx context.Context, chatID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE chat_settings SET pin_warned = true WHERE chat_id = $1 AND NOT pin_warned`, chatID)
	if err != nil {
		return false, fmt.Errorf("pin warning of %d: %w", chatID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// Member is somebody core has seen in a chat with maply.
type Member struct {
	ID        int64
	FirstName string
	Username  string
}

// Silent lists the people seen in a group who have no answer for d.
func (s *Store) Silent(ctx context.Context, chatID int64, d round.Date) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pe.telegram_user_id, COALESCE(pe.first_name, ''), COALESCE(pe.username, '')
		  FROM core.maply_presence p
		  JOIN core.person pe ON pe.telegram_user_id = p.telegram_user_id
		 WHERE p.chat_id = $1 AND NOT pe.is_bot
		   AND NOT EXISTS (SELECT 1 FROM guess g WHERE g.date = $2 AND g.user_id = p.telegram_user_id)`, chatID, d.Time())
	if err != nil {
		return nil, fmt.Errorf("silent in %d: %w", chatID, err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Member])
}

// Streaks counts, for each person, the dates in a row they answered, ending
// at d, or at the date before when d has no answer yet.
func (s *Store) Streaks(ctx context.Context, userIDs []int64, d round.Date) (map[int64]int, error) {
	out := map[int64]int{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, date FROM guess
		 WHERE user_id = ANY($1) AND date <= $2 AND date > $2::date - 1000
		 ORDER BY user_id, date DESC`, userIDs, d.Time())
	if err != nil {
		return nil, fmt.Errorf("streaks: %w", err)
	}
	defer rows.Close()
	next := map[int64]round.Date{}
	for rows.Next() {
		var id int64
		var t time.Time
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		date := round.DateIn(t, time.UTC)
		want, seen := next[id]
		switch {
		case !seen && (date.String() == d.String() || date.String() == d.AddDays(-1).String()):
			out[id], next[id] = 1, date.AddDays(-1)
		case seen && date.String() == want.String() && out[id] > 0:
			out[id]++
			next[id] = date.AddDays(-1)
		default:
			// A gap: this person's streak is already counted.
			next[id] = round.Date{}
		}
	}
	return out, rows.Err()
}

// DayPins are every answer of d, for the "world against you" intrigue.
func (s *Store) DayPins(ctx context.Context, d round.Date) ([][2]float64, error) {
	rows, err := s.pool.Query(ctx, `SELECT lat, lon FROM guess WHERE date = $1`, d.Time())
	if err != nil {
		return nil, fmt.Errorf("day pins: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]float64, error) {
		var p [2]float64
		err := row.Scan(&p[0], &p[1])
		return p, err
	})
}

// InGroups reports whether the person plays in any group maply is still in.
func (s *Store) InGroups(ctx context.Context, userID int64) (bool, error) {
	var in bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM core.maply_presence p
		                 JOIN chat_settings cs ON cs.chat_id = p.chat_id
		                WHERE p.telegram_user_id = $1 AND p.chat_id <> 0 AND cs.active)`, userID).Scan(&in)
	if err != nil {
		return false, fmt.Errorf("groups of %d: %w", userID, err)
	}
	return in, nil
}

const postMinutes = "(EXTRACT(HOUR FROM post_time) * 60 + EXTRACT(MINUTE FROM post_time))::int"

// Settings are a group's switches.
type Settings struct {
	PostHour int
	PostMin  int
	Intrigue bool
	Mentions bool
}

func (s *Store) ChatSettings(ctx context.Context, chatID int64) (Settings, error) {
	st := Settings{PostHour: 7, Intrigue: true, Mentions: true}
	var post int
	err := s.pool.QueryRow(ctx, `SELECT `+postMinutes+`, intrigue, mentions FROM chat_settings WHERE chat_id = $1`, chatID).
		Scan(&post, &st.Intrigue, &st.Mentions)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("settings of %d: %w", chatID, err)
	}
	st.PostHour, st.PostMin = post/60, post%60
	return st, nil
}

// MovePost shifts the morning post by minutes, within 05:00–19:00.
func (s *Store) MovePost(ctx context.Context, chatID int64, minutes int) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind) VALUES ($1, $2) ON CONFLICT (chat_id) DO NOTHING;`,
		chatID, kindOf(chatID)); err != nil {
		return fmt.Errorf("move post of %d: %w", chatID, err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE chat_settings
		   SET post_time = LEAST(GREATEST(post_time + make_interval(mins => $2), '05:00'::time), '19:00'::time),
		       updated_at = now()
		 WHERE chat_id = $1`, chatID, minutes); err != nil {
		return fmt.Errorf("move post of %d: %w", chatID, err)
	}
	return nil
}

// SetSwitch turns the intrigue or the streak mentions on or off.
func (s *Store) SetSwitch(ctx context.Context, chatID int64, name string, on bool) error {
	column := map[string]string{"intrigue": "intrigue", "mentions": "mentions"}[name]
	if column == "" {
		return fmt.Errorf("unknown switch %q", name)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO chat_settings (chat_id, kind, `+column+`) VALUES ($1, $2, $3)
		ON CONFLICT (chat_id) DO UPDATE SET `+column+` = EXCLUDED.`+column+`, updated_at = now()`,
		chatID, kindOf(chatID), on); err != nil {
		return fmt.Errorf("switch %s of %d: %w", name, chatID, err)
	}
	return nil
}
