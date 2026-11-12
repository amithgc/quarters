package stats

import "time"

// ---- "at this pace": when a plan limit runs out ----

// paceWindow is how far back the pace looks: recent enough to follow what
// you're doing now, long enough for the percentages (whole numbers, read
// once a minute) to move.
const (
	paceWindow  = 20 * time.Minute
	paceMinSpan = 5 * time.Minute
)

type paceSample struct {
	at       time.Time
	percent  float64
	resetsAt int64
}

// Pace remembers each limit's recent readings to tell when it runs out at
// the pace you're using it. It isn't safe for concurrent use.
type Pace struct {
	samples map[string][]paceSample // limit → readings, oldest first
}

// NewPace returns an empty Pace.
func NewPace() *Pace { return &Pace{samples: map[string][]paceSample{}} }

// Add records a reading of the account's limits at now and sets each
// limit's RunsOutAt.
func (p *Pace) Add(acct *Account, now time.Time) {
	seen := map[string]bool{}
	for i := range acct.Limits {
		l := &acct.Limits[i]
		key := l.Label
		seen[key] = true
		// readings of an earlier window (it reset since) don't count
		var list []paceSample
		for _, s := range p.samples[key] {
			moved := s.resetsAt-l.ResetsAt > time.Minute.Milliseconds() || l.ResetsAt-s.resetsAt > time.Minute.Milliseconds()
			if now.Sub(s.at) <= paceWindow && !moved && s.percent <= l.Percent {
				list = append(list, s)
			}
		}
		list = append(list, paceSample{now, l.Percent, l.ResetsAt})
		p.samples[key] = list
		l.RunsOutAt = runsOut(list, *l, now)
	}
	for key := range p.samples {
		if !seen[key] { // reset, or gone
			delete(p.samples, key)
		}
	}
}

// runsOut is when a limit reaches 100% at the pace of the readings (unix
// ms), or 0: not enough readings, no use lately, or it resets first.
func runsOut(list []paceSample, l Limit, now time.Time) int64 {
	if len(list) < 2 || l.Percent >= 100 {
		return 0
	}
	first, last := list[0], list[len(list)-1]
	span := last.at.Sub(first.at)
	rise := last.percent - first.percent
	if span < paceMinSpan || rise <= 0 {
		return 0
	}
	perMs := rise / float64(span.Milliseconds())
	at := now.UnixMilli() + int64((100-l.Percent)/perMs)
	if l.ResetsAt > 0 && at >= l.ResetsAt {
		return 0
	}
	return at
}
