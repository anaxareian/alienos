package main

import (
	"context"
	"net/http"
	"slices"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

func RejectEvent(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	management.Lock()
	defer management.Unlock()

	_, banned := management.BannedPubkeys[event.PubKey]
	if banned {
		return true, "blocked: you are banned"
	}

	if config.WhiteListedPubkey {
		_, allowed := management.AllowedPubkeys[event.PubKey]
		if !allowed {
			return true, "restricted: you are not allowed"
		}
	}

	if slices.Contains(management.DisallowedKins, event.Kind) {
		return true, "blocked: kind not allowed"
	}

	if config.WhiteListedKind {
		if !slices.Contains(management.AllowedKinds, event.Kind) {
			return true, "restricted: kind not allowed"
		}
	}

	_, eventBanned := management.BannedEvents[event.ID]
	if eventBanned {
		return true, "blocked: event is banned"
	}

	_, blocked := management.BlockedIPs[khatru.GetIP(ctx)]
	if blocked {
		return true, "blocked: this IP is blocked"
	}

	return false, ""
}

func StoreEvent(ctx context.Context, event *nostr.Event) error {
	management.Lock()
	defer management.Unlock()

	if event.Kind == nostr.KindReporting {
		for _, t := range event.Tags {
			if t.Key() == "e" && t.Value() != "" {
				if len(t.Value()) == 64 {
					management.ModerationEvents[t.Value()] = event.Content
				}
			}
		}
	}

	UpdateManagement()

	return nil
}

func RejectUpload(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
	management.Lock()
	defer management.Unlock()

	_, banned := management.BannedPubkeys[auth.PubKey]
	if banned {
		return true, "blocked: you are banned", http.StatusForbidden
	}

	if config.WhiteListedPubkey {
		_, allowed := management.AllowedPubkeys[auth.PubKey]
		if !allowed {
			return true, "restricted: you are not allowed", http.StatusForbidden
		}
	}

	_, blocked := management.BlockedIPs[khatru.GetIP(ctx)]
	if blocked {
		return true, "blocked: this IP is blocked", http.StatusForbidden
	}

	return false, "", http.StatusOK
}

// RejectFilter guards gift-wrapped (kind 1059) reads: the client must be
// NIP-42 authenticated and may only ask for wraps addressed to itself.
func RejectFilter(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	if !slices.Contains(filter.Kinds, nostr.KindGiftWrap) {
		return false, ""
	}

	auth := khatru.GetAuthed(ctx)
	if auth == "" {
		return true, "auth-required: you are reading gift-wrapped events"
	}

	ps := filter.Tags["p"]
	if len(ps) == 0 {
		return true, "restricted: you can read your gift-wrapped events only"
	}

	for _, pk := range ps {
		if pk != auth {
			return true, "restricted: you can't read other people's DMs"
		}
	}

	return false, ""
}

// HideGiftWraps wraps a query function so that gift-wrapped events are only
// returned to the authenticated recipient, whatever filter was used.
func HideGiftWraps(q func(context.Context, nostr.Filter) (chan *nostr.Event, error)) func(context.Context, nostr.Filter) (chan *nostr.Event, error) {
	return func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
		in, err := q(ctx, filter)
		if err != nil || in == nil {
			return in, err
		}
		auth := khatru.GetAuthed(ctx)
		out := make(chan *nostr.Event)
		go func() {
			defer close(out)
			for ev := range in {
				if ev.Kind == nostr.KindGiftWrap {
					tag := ev.Tags.GetFirst([]string{"p", ""})
					if auth == "" || tag == nil || (*tag)[1] != auth {
						continue
					}
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					for range in {
					}
					return
				}
			}
		}()
		return out, nil
	}
}
