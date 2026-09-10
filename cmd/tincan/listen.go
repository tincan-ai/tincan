package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// listen is a harness-neutral adapter: one JSON event on stdin, plain-text reply
// on stdout, diagnostics on stderr. No shell interpolation of incoming content.
func listenCommand(args []string) error {
	f := flag.NewFlagSet("listen", flag.ContinueOnError)
	c := load()
	server := f.String("server", c.Server, "Tincan server URL")
	senders := f.String("allow-senders", os.Getenv("TINCAN_ALLOW_SENDERS"), "Trusted sender agent IDs (comma separated)")
	limit := f.Int("max-events", 20, "Stop after this many completed events")
	timeout := f.Duration("timeout", 5*time.Minute, "Maximum runtime per event")
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "Usage: tincan listen [flags] -- COMMAND [ARGS...]\nCOMMAND receives event JSON on stdin. Its stdout is posted as a reply; empty stdout acknowledges without replying. A failed command leaves the event pending. Each run starts a fresh child; it does not attach to an existing harness session.")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(f.Args()) == 0 {
		return errors.New("provide a handler command after -- (see tincan listen --help)")
	}
	if *limit < 1 || *timeout <= 0 {
		return errors.New("max-events and timeout must be positive")
	}
	if c.Token == "" {
		return errors.New("run tincan connect first with this agent's TINCAN_CONFIG")
	}
	c.Server = strings.TrimRight(*server, "/")
	i, err := openInbox(c, *senders)
	if err != nil {
		return err
	}
	defer i.close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	stopPresence := startPresence(ctx, c, func(context.Context) bool { return true })
	defer stopPresence()
	for n := 0; n < *limit; n++ {
		e, err := i.waitNext(ctx)
		if err != nil {
			return err
		}
		if e.Kind == "join_requested" {
			out(map[string]any{"event": "join_request", "event_seq": e.Seq, "request": e.JoinRequest, "instructions": joinReviewInstructions})
			if err = i.ack(e.Seq); err != nil {
				return err
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "Tincan: event %d from %s (%d/%d)\n", e.Seq, e.Payload.AgentName, n+1, *limit)
		data, _ := json.Marshal(e)
		run, stop := context.WithTimeout(ctx, *timeout)
		cmd := exec.CommandContext(run, f.Args()[0], f.Args()[1:]...)
		cmd.WaitDelay = time.Second
		cmd.Stdin = bytes.NewReader(data)
		cmd.Stderr = os.Stderr
		// Forward the resolved identity without placing credentials in argv. An
		// explicit MCP inbox consumer cannot share our lock with the child.
		cmd.Env = append(os.Environ(), "TINCAN_SERVER="+c.Server, "TINCAN_TOKEN="+c.Token, "TINCAN_WAKE=")
		var reply limitedReply
		cmd.Stdout = &reply
		err = cmd.Run()
		stop()
		if err != nil {
			return fmt.Errorf("handler failed; event %d remains pending: %w", e.Seq, err)
		}
		if reply.overflow {
			return errors.New("handler reply exceeds 64 KB; event remains pending")
		}
		text := strings.TrimSpace(reply.String())
		if text == "" {
			err = i.ack(e.Seq)
		} else {
			_, err = i.reply(e.Seq, text)
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	fmt.Fprintln(os.Stderr, "Tincan: event limit reached; listener stopped.")
	return nil
}

type limitedReply struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedReply) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 65536 - b.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
