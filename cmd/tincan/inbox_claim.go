package main

import (
	"errors"
	"strings"

	"github.com/tincan-ai/tincan-plugin/internal/core"
)

// Claims serialize delegated execution, independently of transport delivery.
// They deliberately do not expire: a slow or disconnected worker may still be
// making changes. Only its owner may release it after the worker has stopped.
type inboxClaim struct {
	WorkerID string `json:"worker_id"`
	Token    string `json:"token"`
}

type claimResult struct {
	Acquired bool        `json:"acquired"`
	WorkerID string      `json:"worker_id,omitempty"`
	Claim    string      `json:"claim,omitempty"`
	Event    *inboxEvent `json:"event,omitempty"`
}

func (i *inbox) claim(seq int64, worker string) (claimResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if strings.TrimSpace(worker) == "" || len(worker) > 200 || strings.ContainsAny(worker, "\x00\r\n") {
		return claimResult{}, errors.New("worker_id must identify this delegated worker")
	}
	if i.state.Pending == nil || i.state.Pending.Seq != seq || i.state.Reply != nil || !i.accepts(*i.state.Pending) {
		return claimResult{}, nil
	}
	if i.state.Claim != nil && i.state.Claim.WorkerID != worker {
		return claimResult{WorkerID: i.state.Claim.WorkerID}, nil
	}
	if i.state.Claim == nil {
		s := i.state
		s.Claim = &inboxClaim{WorkerID: worker, Token: core.ID("claim_")}
		if err := i.save(s); err != nil {
			return claimResult{}, err
		}
	}
	return claimResult{Acquired: true, WorkerID: worker, Claim: i.state.Claim.Token, Event: i.state.Pending}, nil
}

func (i *inbox) checkClaim(tokens []string) error {
	if i.state.Claim != nil && (len(tokens) != 1 || tokens[0] != i.state.Claim.Token) {
		return errors.New("mention belongs to a delegated worker; its claim is required")
	}
	if i.state.Claim == nil && len(tokens) > 0 && tokens[0] != "" && i.state.Pending != nil {
		return errors.New("worker claim is no longer active")
	}
	return nil
}

func (i *inbox) release(seq int64, token string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state.Pending == nil || i.state.Pending.Seq != seq || i.state.Claim == nil {
		return errors.New("no matching worker claim")
	}
	if err := i.checkClaim([]string{token}); err != nil {
		return err
	}
	if i.state.Reply != nil {
		return errors.New("reply delivery is pending; recover the reply before releasing")
	}
	s := i.state
	s.Claim = nil
	return i.save(s)
}

func (i *inbox) execution() map[string]any {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state.Pending != nil && i.state.Pending.Kind == "join_requested" {
		return map[string]any{"mode": "owner_review", "claim_required": false, "instructions": joinReviewInstructions}
	}
	v := inboundExecution()
	if i.state.Claim != nil {
		v["worker_id"] = i.state.Claim.WorkerID
		v["state"] = "claimed"
	}
	return v
}

func inboundExecution() map[string]any {
	return map[string]any{"mode": "background_delegate", "foreground_execution": false, "claim_required": true}
}

const inboundDispatchInstructions = "Delegate this mention to a native background subagent or isolated harness worker, passing its connection, event sequence and the user's scope. The worker calls inbox_claim before acting and commits with its claim only after completion. Keep the main conversation free; do not execute inline or wait for the worker. If delegation is unavailable, leave the request pending. Do not poll."

// Wake the parent only to dispatch. The worker retrieves the body when claiming;
// peer instructions never get embedded into a foreground queue/hook prompt.
func inboundNotification(p map[string]any) map[string]any {
	if p["kind"] == "join_request" {
		return map[string]any{"kind": "join_request", "connection": p["connection"], "event_seq": p["event_seq"], "instructions": joinReviewInstructions}
	}
	if p["kind"] != "mention" {
		return p
	}
	v := map[string]any{"kind": "mention", "event_seq": p["event_seq"], "execution": inboundExecution(), "instructions": inboundDispatchInstructions}
	if handle, ok := p["connection"]; ok {
		v["connection"] = handle
	}
	return v
}
