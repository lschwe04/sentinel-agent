package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// TelemetryPayload repräsentiert die zu übertragenden Sicherheitsdaten
type TelemetryPayload struct {
	NodeID       string            `json:"node_id"`
	Timestamp    int64             `json:"timestamp"`
	Metrics      map[string]string `json:"metrics"`
	FIMChecksums map[string]string `json:"fim_checksums"`
}

// SignedPacket bündelt die Daten mit ihrer kryptografischen Signatur
type SignedPacket struct {
	Payload   TelemetryPayload `json:"payload"`
	Signature string           `json:"signature"`
}

// CreateSignedPayload serialisiert die Telemetrie und signiert sie per HMAC-SHA256
func CreateSignedPayload(nodeID string, metrics map[string]string, fim map[string]string, sharedSecret string) ([]byte, error) {
	payload := TelemetryPayload{
		NodeID:       nodeID,
		Timestamp:    time.Now().Unix(),
		Metrics:      metrics,
		FIMChecksums: fim,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("fehler beim marshallen der telemetrie: %w", err)
	}

	// HMAC-SHA256 Signaturerstellung mit dem agentenspezifischen Shared Secret
	mac := hmac.New(sha256.New, []byte(sharedSecret))
	mac.Write(data)
	signature := hex.EncodeToString(mac.Sum(nil))

	packet := SignedPacket{
		Payload:   payload,
		Signature: signature,
	}

	return json.Marshal(packet)
}
