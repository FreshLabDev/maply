// SPDX-License-Identifier: Apache-2.0

// Package free is the free game: places alone or with friends, as many as one
// likes, outside the day's ranking. A solo game is three requests a place:
// start, answer, picture. A party is the same places for everyone at once, its
// phase worked out from the clock on every read, with no timers on the server.
//
// Nobody learns a place before answering it. Starting a place writes when it
// began and when it ends, and only then gives its picture, under the round's
// own address and never under the place's image key: the keys of closed days
// are on the days page. The place itself comes in the answer, alone, or when
// the place closes, in a party.
package free

import (
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	Classic = "classic"
	Streak  = "streak"

	Solo  = "solo"
	Party = "party"
	Duel  = "duel"

	StatusLobby     = "lobby"
	StatusPlaying   = "playing"
	StatusDone      = "done"
	StatusAbandoned = "abandoned"
)

const (
	// Grace is how long after the end an answer is still taken, for the
	// network. It earns no speed bonus.
	Grace = 2 * time.Second
	// Countdown is the pause before a party's place, for everyone at once.
	Countdown = 3 * time.Second
	// Silent is how long a party player may go without a poll before the
	// place stops waiting for them.
	Silent = 10 * time.Second
	// HostAfter is how long a lobby waits for a silent host before the next
	// player by joining time becomes the host.
	HostAfter = 30 * time.Second
	// LobbyFor is how long a lobby lives without a start.
	LobbyFor = 30 * time.Minute
	// AbandonAfter closes a game nobody has touched.
	AbandonAfter = 24 * time.Hour
	// MaxMembers is a party's size.
	MaxMembers = 8
	// MaxSpeed is the most the speed bonus gives for a place.
	MaxSpeed = 200
	// MaxKm is the distance a place without a pin counts as when two players
	// tie on points: half the Earth's circumference, the farthest a pin can be.
	MaxKm = 20037.5
	// loadFree is how much of the place's time goes to loading its picture
	// without costing any of the speed bonus.
	loadFree = 2 * time.Second
	// maxDistancePoints is the judge's most for the distance.
	maxDistancePoints = 3500
)

// Settings is what a game is played on. Seconds is the time for a place, 0
// for no timer; Rounds is 0 for a streak, which goes on while the country is
// right.
type Settings struct {
	Mode    string `json:"mode"`
	Seconds int    `json:"seconds"`
	Rounds  int    `json:"rounds"`
}

var (
	partySeconds = []int{30, 60, 90, 120}
	soloSeconds  = []int{0, 30, 60, 90, 120}
	// streakSeconds has no "no timer": without one a streak turns into a
	// search for the answer on the internet.
	streakSeconds = []int{30, 60}
	allRounds     = []int{3, 5, 10}
)

func in(v int, set []int) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

// ValidSolo is whether one person may play on s: a classic of 3, 5 or 10
// places on any timer or none, or a streak on 30 or 60 seconds.
func (s Settings) ValidSolo() bool {
	switch s.Mode {
	case Classic:
		return in(s.Seconds, soloSeconds) && in(s.Rounds, allRounds)
	case Streak:
		return in(s.Seconds, streakSeconds) && s.Rounds == 0
	}
	return false
}

// ValidParty is whether a party may play on s. A party has no "no timer": one
// player who walked away would hold everyone.
func (s Settings) ValidParty() bool {
	return s.Mode == Classic && in(s.Seconds, partySeconds) && in(s.Rounds, allRounds)
}

// Key names the settings a record is kept for: "classic/60/5",
// "classic/0/10", "streak/30".
func (s Settings) Key() string {
	if s.Mode == Streak {
		return Streak + "/" + strconv.Itoa(s.Seconds)
	}
	return s.Mode + "/" + strconv.Itoa(s.Seconds) + "/" + strconv.Itoa(s.Rounds)
}

// Limit is the time for a place, 0 without a timer.
func (s Settings) Limit() time.Duration { return time.Duration(s.Seconds) * time.Second }

// DefaultSolo is a first game's settings.
var DefaultSolo = Settings{Mode: Classic, Seconds: 60, Rounds: 5}

// RevealFor is how long a party shows a closed place: 8 s for a blitz, 10 s
// on the longer timers.
func RevealFor(seconds int) time.Duration {
	if seconds == 30 {
		return 8 * time.Second
	}
	return 10 * time.Second
}

// Length is how long a party on s lasts when everyone answers at the end of
// the time: what the lobby shows under the settings.
func Length(s Settings) time.Duration {
	return time.Duration(s.Rounds) * (s.Limit() + Grace + RevealFor(s.Seconds) + Countdown)
}

// Speed is the bonus for answering early. It counts the share of the time
// left, not the seconds, so the same share earns the same bonus on every
// timer, and it scales with the distance points, so a quick random click
// earns nothing:
//
//	left  = clamp(ends_at − answered_at, 0, T)
//	share = min(1, left / (T − 2 s))   // the first 2 s load the picture
//	speed = round10(200 × share × points_for_distance / 3500)
//
// No timer, an answer in the 2 s grace and a pin the app sent itself at zero
// earn none.
func Speed(seconds int, endsAt, answeredAt time.Time, distancePoints int, auto bool) int {
	if seconds <= 0 || auto {
		return 0
	}
	limit := time.Duration(seconds) * time.Second
	left := min(max(endsAt.Sub(answeredAt), 0), limit)
	if left <= 0 {
		return 0
	}
	share := math.Min(1, float64(left)/float64(limit-loadFree))
	p := MaxSpeed * share * float64(distancePoints) / maxDistancePoints
	return int(math.Round(p/10) * 10)
}

// InvitePrefix starts the startapp payload of a party's invitation: "p" and
// the party's code. A group post's payload is base64 of a version byte and
// always starts with "A", so the two never meet.
const InvitePrefix = "p"

// InviteCode is the party code in a startapp payload, if it is an
// invitation.
func InviteCode(startParam string) (string, bool) {
	code, ok := strings.CutPrefix(startParam, InvitePrefix)
	if !ok || !ValidCode(code) {
		return "", false
	}
	return code, true
}
