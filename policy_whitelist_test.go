package main

import (
	"context"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestWhitelistGiftWrapInbox(t *testing.T) {
	member := "1111111111111111111111111111111111111111111111111111111111111111"
	stranger := "2222222222222222222222222222222222222222222222222222222222222222"
	throwaway := "3333333333333333333333333333333333333333333333333333333333333333"

	config.WhiteListedPubkey = true
	defer func() { config.WhiteListedPubkey = false }()
	management.AllowedPubkeys = map[string]string{member: "family"}
	management.BannedPubkeys = map[string]string{}
	management.BannedEvents = map[string]string{}
	management.BlockedIPs = map[string]string{}
	management.DisallowedKins = nil

	cases := []struct {
		name   string
		ev     nostr.Event
		reject bool
	}{
		{"member post", nostr.Event{Kind: 1, PubKey: member}, false},
		{"stranger post", nostr.Event{Kind: 1, PubKey: stranger}, true},
		{"wrap to member", nostr.Event{Kind: 1059, PubKey: throwaway, Tags: nostr.Tags{{"p", member}}}, false},
		{"wrap to stranger", nostr.Event{Kind: 1059, PubKey: throwaway, Tags: nostr.Tags{{"p", stranger}}}, true},
		{"wrap mixed", nostr.Event{Kind: 1059, PubKey: throwaway, Tags: nostr.Tags{{"p", member}, {"p", stranger}}}, true},
		{"wrap no p", nostr.Event{Kind: 1059, PubKey: throwaway}, true},
		{"kind1 p member by stranger", nostr.Event{Kind: 1, PubKey: stranger, Tags: nostr.Tags{{"p", member}}}, true},
	}
	for _, c := range cases {
		got, _ := RejectEvent(context.Background(), &c.ev)
		if got != c.reject {
			t.Errorf("%s: reject=%v want %v", c.name, got, c.reject)
		}
	}
}
