// sentinel-agent: internal/collector/reporter.go (Neu)
package collector

import (
	"context"
	"math/rand"
	"net/http"
	"time"
)

func StartResilientReporter(ctx context.Context, client *http.Client, reportFunc func()) {
	// Basis-Intervall von 30 Sekunden
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Jitter: Zufällige Verzögerung von 0 bis 5 Sekunden hinzufügen
			// Verhindert den "Thundering Herd"-Effekt bei tausenden Agenten
			jitter := time.Duration(rand.Int63n(5000)) * time.Millisecond
			time.Sleep(jitter)

			// Führe den eigentlichen Report mit Retry-Backoff aus
			go executeWithRetry(reportFunc, 3)
		}
	}
}

func executeWithRetry(operation func(), maxRetries int) {
	for i := 0; i < maxRetries; i++ {
		// operation() gibt in der echten Implementierung einen Error zurück
		// Hier als Dummy-Struktur für den Architektur-Ansatz
		operation()
		return // Bei Erfolg sofort abbrechen
		// Bei Fehler: time.Sleep(time.Duration(i*2) * time.Second)
	}
}
