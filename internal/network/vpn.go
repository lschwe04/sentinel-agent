package network

import (
	"errors"
	"net"
)

func ValidateVPNConnection(remoteAddr string) error {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("invalid IP address format")
	}

	// Lokale Verbindungen für Tests erlauben
	if ip.IsLoopback() {
		return nil
	}

	// Beispiel: Erlaube nur IPs aus dem internen WireGuard-Netz (10.0.0.0/8)
	_, trustedNet, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		return err
	}

	if trustedNet.Contains(ip) {
		return nil
	}

	return errors.New("IP is outside the trusted VPN range")
}
