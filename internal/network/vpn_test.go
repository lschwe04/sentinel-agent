package network

import "testing"

func TestValidateVPNConnection(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		wantErr    bool
	}{
		{"Valid Loopback", "127.0.0.1:12345", false},
		{"Valid WireGuard IP", "10.0.0.15:54321", false},
		{"Invalid Public IP", "8.8.8.8:443", true},
		{"Malformed IP", "not-an-ip", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVPNConnection(tt.remoteAddr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVPNConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
