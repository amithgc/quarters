package main

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// ---- the prompt queue: prompts sent one by one as the agent finishes ----

// queueQuiet is how long an agent must be quiet (no output, nothing typed)
// before the next queued prompt goes in; like "your turn" alerts.
const queueQuiet = 3 * time.Second

// maxQueued is how many prompts a session can have waiting.
const maxQueued = 50

// promptQueue is a session's waiting prompts; App.fmu guards it.
type promptQueue struct {
	items []string
	sent  time.Time // when the last one was sent
}

// QueueView is a session's queue, as windows show it.
type QueueView struct {
	ID    int      `json:"id"`
	Items []string `json:"items"`
}

// Queues lists every session's queued prompts.
func (a *App) Queues() []QueueView {
	live := map[int]bool{}
	for _, info := range a.sessions.List() {
		live[info.ID] = !info.Exited
	}
	a.fmu.Lock()
	defer a.fmu.Unlock()
	out := []QueueView{}
	for id, q := range a.f.queues {
		if !live[id] {
			delete(a.f.queues, id) // its session ended
			continue
		}
		out = append(out, QueueView{id, slices.Clone(q.items)})
	}
	slices.SortFunc(out, func(x, y QueueView) int { return x.ID - y.ID })
	return out
}

// agentSession checks id is a running coding agent (not a shell or an
// automation): only those are given prompts.
func (a *App) agentSession(id int) error {
	info, ok := a.sessions.Info(id)
	if !ok || info.Exited {
		return errors.New("that session has ended")
	}
	if m := metaOf(info); m.Kind == "shell" || m.AutoKey != "" {
		return errors.New("prompts can only be queued for a coding agent")
	}
	return nil
}

// QueueAdd adds a prompt to the end of a session's queue.
func (a *App) QueueAdd(id int, text string) error {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return errors.New("write the prompt first")
	}
	if len(text) > 64<<10 {
		return errors.New("that prompt is too long")
	}
	if !a.config().On("prompt_queue") {
		return errors.New("the prompt queue is switched off (Settings)")
	}
	if err := a.agentSession(id); err != nil {
		return err
	}
	a.fmu.Lock()
	q := a.f.queues[id]
	if q == nil {
		q = &promptQueue{}
		a.f.queues[id] = q
	}
	if len(q.items) >= maxQueued {
		a.fmu.Unlock()
		return errors.New("the queue is full")
	}
	q.items = append(q.items, text)
	v := QueueView{id, slices.Clone(q.items)}
	a.fmu.Unlock()
	a.emit("queue", v)
	return nil
}

// QueueRemove takes prompt i out of a session's queue; QueueMove moves it
// one place up (d = -1) or down (d = 1).
func (a *App) QueueRemove(id, i int) error { return a.editQueue(id, i, 0) }

func (a *App) QueueMove(id, i, d int) error {
	if d != -1 && d != 1 {
		return errors.New("move up or down one place")
	}
	return a.editQueue(id, i, d)
}

func (a *App) editQueue(id, i, d int) error {
	a.fmu.Lock()
	q := a.f.queues[id]
	if q == nil || i < 0 || i >= len(q.items) {
		a.fmu.Unlock()
		return errors.New("that prompt is no longer queued")
	}
	switch j := i + d; {
	case d == 0:
		q.items = slices.Delete(q.items, i, i+1)
	case j >= 0 && j < len(q.items):
		q.items[i], q.items[j] = q.items[j], q.items[i]
	}
	v := QueueView{id, slices.Clone(q.items)}
	if len(q.items) == 0 {
		delete(a.f.queues, id)
	}
	a.fmu.Unlock()
	a.emit("queue", v)
	return nil
}

// queued reports whether a session has prompts waiting.
func (a *App) queued(id int) bool {
	a.fmu.Lock()
	defer a.fmu.Unlock()
	q := a.f.queues[id]
	return q != nil && len(q.items) > 0
}

// QueueNext sends a session's next queued prompt, if the agent is ready
// for it. A window calls it when it sees the agent's input box on screen
// (not a question or a permission prompt: only the window can see that);
// here Quarters checks the agent has been quiet for a while and has worked
// since the last queued prompt went in, so two windows asking at once, or
// an agent that didn't take the last one, never get a prompt twice.
func (a *App) QueueNext(id int) (bool, error) {
	if !a.config().On("prompt_queue") {
		return false, nil
	}
	if err := a.agentSession(id); err != nil {
		return false, err
	}
	work, input, ok := a.sessions.Quiet(id)
	now := time.Now()
	if !ok || now.Sub(work) < queueQuiet || now.Sub(input) < queueQuiet {
		return false, nil
	}
	a.fmu.Lock()
	q := a.f.queues[id]
	if q == nil || len(q.items) == 0 || !q.sent.IsZero() && !work.After(q.sent) {
		a.fmu.Unlock()
		return false, nil
	}
	text := q.items[0]
	q.items = q.items[1:]
	q.sent = now
	v := QueueView{id, slices.Clone(q.items)}
	if len(q.items) == 0 {
		delete(a.f.queues, id)
	}
	a.fmu.Unlock()

	switch {
	case !strings.Contains(text, "\n"):
		a.sessions.Send(id, text)
	case a.sessions.Pastes(id):
		a.sessions.Send(id, "\x1b[200~"+text+"\x1b[201~") // several lines arrive as one paste
	default: // a line break would send the first line alone
		a.sessions.Send(id, strings.Join(strings.Fields(text), " "))
	}
	// Enter on its own, a moment later: agents treat Enter that arrives
	// with the text as part of a paste
	time.AfterFunc(80*time.Millisecond, func() { a.sessions.Send(id, "\r") })
	a.emit("queue", v)
	return true, nil
}
