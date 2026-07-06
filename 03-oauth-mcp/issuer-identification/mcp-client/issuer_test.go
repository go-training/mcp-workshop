//go:build !windows

package main

import "testing"

func TestValidateIssuerResponse(t *testing.T) {
	const expected = "http://localhost:8080"

	tests := []struct {
		name         string
		iss          string
		issSupported bool
		wantErr      bool
	}{
		{
			name:         "supported and iss matches expected",
			iss:          expected,
			issSupported: true,
			wantErr:      false,
		},
		{
			name:         "supported but iss missing",
			iss:          "",
			issSupported: true,
			wantErr:      true,
		},
		{
			name:         "supported but iss mismatched (the mix-up attack)",
			iss:          "http://localhost:9090",
			issSupported: true,
			wantErr:      true,
		},
		{
			name:         "not supported but iss present",
			iss:          expected,
			issSupported: false,
			wantErr:      true,
		},
		{
			name:         "not supported and iss absent (legacy AS)",
			iss:          "",
			issSupported: false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIssuerResponse(tt.iss, expected, tt.issSupported)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateIssuerResponse(%q, %q, %v) error = %v, wantErr %v",
					tt.iss, expected, tt.issSupported, err, tt.wantErr)
			}
		})
	}
}
