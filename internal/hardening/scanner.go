package hardening

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type HardeningReport struct {
	NodeID             string `json:"node_id"`
	CustomerID         int    `json:"customer_id"`
	CisLevel1Compliant bool   `json:"cis_level_1_compliant"`
	OpenIssues         int    `json:"open_issues"`
}

func RunComplianceCheck(hubBaseURL, nodeID string, customerID int, authToken string) {
	// Beispielhafter Check: Ist SSH Root-Login in /etc/ssh/sshd_config verboten?
	compliant := true
	openIssues := 0

	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err == nil {
		content := string(data)
		// Einfache Prüfung auf PermitRootLogin no
		if strings.Contains(content, "PermitRootLogin yes") {
			compliant = false
			openIssues++
		}
	}

	report := HardeningReport{
		NodeID:             nodeID,
		CustomerID:         customerID,
		CisLevel1Compliant: compliant,
		OpenIssues:         openIssues,
	}

	jsonData, err := json.Marshal(report)
	if err != nil {
		return
	}

	// An Hub senden
	req, err := http.NewRequest(http.MethodPost, hubBaseURL+"/hardening", bytes.NewBuffer(jsonData))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
