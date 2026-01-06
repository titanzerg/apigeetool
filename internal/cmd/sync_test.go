package cmd

import "testing"

func TestParseSyncSelection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected SyncSelection
		wantErr  bool
	}{
		{
			name:  "all",
			input: "all",
			expected: SyncSelection{
				ProxyEndpoints: true,
				TargetServers:  true,
				APIProducts:    true,
				Apps:           true,
			},
		},
		{
			name:  "apiproxy only",
			input: "apiproxy",
			expected: SyncSelection{
				ProxyEndpoints: true,
			},
		},
		{
			name:  "target server with underscore",
			input: "target_server",
			expected: SyncSelection{
				TargetServers: true,
			},
		},
		{
			name:  "api product hyphen",
			input: "api-product",
			expected: SyncSelection{
				APIProducts: true,
			},
		},
		{
			name:  "multiple comma separated",
			input: "apiproxy,api_product",
			expected: SyncSelection{
				ProxyEndpoints: true,
				APIProducts:    true,
			},
		},
		{
			name:  "apps only",
			input: "apps",
			expected: SyncSelection{
				Apps: true,
			},
		},
		{
			name:    "unknown token",
			input:   "something",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selection, err := ParseSyncSelection(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSyncSelection(%q) returned error: %v", tt.input, err)
			}
			if selection != tt.expected {
				t.Fatalf("unexpected selection: %+v (expected %+v)", selection, tt.expected)
			}
		})
	}
}
