package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestCheckAudience(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		expected string
		got      []string
		wantErr  bool
	}{
		{"empty expected accepts anything", "", nil, false},
		{"empty expected accepts mismatched", "", []string{"https://other/mcp"}, false},
		{"exact match", "https://mcp/", []string{"https://mcp/"}, false},
		{
			"contains match among multiple",
			"https://mcp/",
			[]string{"https://other/", "https://mcp/"},
			false,
		},
		{
			"mismatch single",
			"https://mcp/",
			[]string{"https://attacker/"},
			true,
		},
		{
			"mismatch multiple",
			"https://mcp/",
			[]string{"https://a/", "https://b/"},
			true,
		},
		{"empty got with expected", "https://mcp/", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkAudience(context.Background(), tc.expected, tc.got)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, auth.ErrInvalidToken) {
					t.Fatalf("expected wrapped auth.ErrInvalidToken, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestMasked(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", "***"},
		{"a", "***"},
		{"abcd", "***"},
		{"abcde", "ab***de"},
		{"abcdefghij", "ab***ij"},
	}
	for _, tc := range cases {
		if got := masked(tc.in); got != tc.want {
			t.Errorf("masked(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskHint(t *testing.T) {
	t.Parallel()
	if got := maskHint("", ""); got != "***" {
		t.Errorf("maskHint(empty,empty) = %q, want ***", got)
	}
	got := maskHint("user-12345", "client-abcde")
	if !strings.Contains(got, "sub=us***45") || !strings.Contains(got, "client=cl***de") {
		t.Errorf("maskHint produced unexpected output: %q", got)
	}
}
