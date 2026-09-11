package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

const defaultInboxWait = 15 * time.Minute
const maxInboxWait = time.Hour

// A waiter is a live tool call, not a durable worker claim or proof that the
// host preserves subagents after the parent finishes. Never persist readiness.
type inboxWaiter struct {
	WorkerID string    `json:"worker_id"`
	Until    time.Time `json:"until"`
}

// save and close call this with mu held. Broadcast separately from changed:
// consuming the SSE acknowledgement channel here could strand the stream.
func (i *inbox) signalUpdate() {
	if i.updates != nil {
		close(i.updates)
	}
	i.updates = make(chan struct{})
}

func (i *inbox) waiting() *inboxWaiter {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.waiter == nil || !time.Now().Before(i.waiter.Until) {
		return nil
	}
	w := *i.waiter
	return &w
}

// waitMention only observes the durable inbox. The child must claim separately
// before execution, so concurrent native delivery cannot duplicate the work.
// No network polling, progress pings, or periodic returns to the model.
func (i *inbox) waitMention(ctx context.Context, worker string, duration time.Duration) (map[string]any, error) {
	if strings.TrimSpace(worker) == "" || len(worker) > 200 || strings.ContainsAny(worker, "\x00\r\n") {
		return nil, errors.New("worker_id must identify this delegated listener")
	}
	if duration <= 0 || duration > maxInboxWait {
		return nil, errors.New("wait duration must be positive and at most one hour")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	until := time.Now().Add(duration)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
		until = deadline
	}
	w := &inboxWaiter{WorkerID: worker, Until: until}
	i.mu.Lock()
	if i.closed || !i.background {
		i.mu.Unlock()
		return nil, errors.New("background inbox is not running")
	}
	if i.waiter != nil {
		i.mu.Unlock()
		return nil, errors.New("a delegated listener is already waiting; do not start another")
	}
	i.waiter = w
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		i.waiter = nil
		i.mu.Unlock()
	}()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		i.mu.Lock()
		if i.closed {
			i.mu.Unlock()
			return nil, errors.New("inbox closed; stop the delegated listener")
		}
		pending, claim, reply := i.state.Pending, i.state.Claim, i.state.Reply
		if pending != nil && i.accepts(*pending) {
			result := map[string]any{"event_seq": pending.Seq, "kind": pending.Kind, "experimental": true}
			switch {
			case reply != nil:
				result["status"] = "reply_pending"
				result["instructions"] = "Stop. A saved reply needs recovery; do not execute this event again."
			case claim != nil:
				result["status"] = "claimed"
				result["worker_id"] = claim.WorkerID
				result["instructions"] = "Stop. This event already has a worker claim. Resume only the known worker after checking its outcome; never release or rerun uncertain work."
			case pending.Kind == "join_requested":
				result["status"] = "owner_review"
				result["instructions"] = "Read inbox_next once and report the join verification phrase to the parent for owner review, then stop. Leave the request pending; never approve automatically."
			default:
				result["status"] = "event"
				result["instructions"] = "Call inbox_claim with this event_seq and your worker_id. Act only if acquired=true, within the parent's authorized scope. Reply or acknowledge with the claim after completion. Then inbox_wait again within the authorized listening period."
			}
			i.mu.Unlock()
			return result, nil
		}
		if i.updates == nil {
			i.updates = make(chan struct{})
		}
		updates := i.updates
		i.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return map[string]any{"status": "expired", "experimental": true, "instructions": "Stop the delegated listener. Do not loop on an empty timeout or start a replacement. Re-arm only on later user activity or explicit host scheduling."}, nil
		case <-updates:
		}
	}
}
