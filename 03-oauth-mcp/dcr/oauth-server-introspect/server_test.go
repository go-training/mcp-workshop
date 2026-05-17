package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestAudClaimUnmarshal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want audClaim
	}{
		{"single string", `"https://mcp/"`, audClaim{"https://mcp/"}},
		{
			"array of strings",
			`["https://mcp/","https://other/"]`,
			audClaim{"https://mcp/", "https://other/"},
		},
		{"empty array", `[]`, audClaim{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got audClaim
			if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestAudClaimUnmarshalReject(t *testing.T) {
	t.Parallel()
	var got audClaim
	if err := json.Unmarshal([]byte(`42`), &got); err == nil {
		t.Fatal("expected error for non-string non-array, got nil")
	}
}

func TestIntrospectorCheckAudience(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		expectedAudience string
		requireBinding   bool
		aud              audClaim
		wantErr          bool
		wantInvalidToken bool
	}{
		{"no expected, anything ok", "", false, nil, false, false},
		{"match", "https://mcp/", false, audClaim{"https://mcp/"}, false, false},
		{
			"missing aud, binding not required",
			"https://mcp/",
			false,
			nil,
			false,
			false,
		},
		{
			"missing aud, binding required",
			"https://mcp/",
			true,
			nil,
			true,
			true,
		},
		{
			"mismatch",
			"https://mcp/",
			true,
			audClaim{"https://other/"},
			true,
			true,
		},
		{
			"contains match",
			"https://mcp/",
			true,
			audClaim{"https://other/", "https://mcp/"},
			false,
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ins := &introspector{
				expectedAudience:       tc.expectedAudience,
				requireResourceBinding: tc.requireBinding,
			}
			err := ins.checkAudience(context.Background(), tc.aud)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.wantInvalidToken && !errors.Is(err, auth.ErrInvalidToken) {
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
		{"abcd", "***"},
		{"abcde", "ab***de"},
	}
	for _, tc := range cases {
		if got := masked(tc.in); got != tc.want {
			t.Errorf("masked(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
