package network

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ReverseTunnel struct {
	HubSSHAddr     string
	LocalAddr      string
	RemotePort     int
	PrivKeyPath    string
	KnownHostsPath string
}

const (
	initialReconnectBackoff = time.Second
	maxReconnectBackoff     = 5 * time.Minute
)

func NewReverseTunnel(hubAddr, localAddr string, remotePort int, privKeyPath string) *ReverseTunnel {
	return &ReverseTunnel{
		HubSSHAddr:     hubAddr,
		LocalAddr:      localAddr,
		RemotePort:     remotePort,
		PrivKeyPath:    privKeyPath,
		KnownHostsPath: os.Getenv("AGENT_SSH_KNOWN_HOSTS"),
	}
}

func (t *ReverseTunnel) Start(ctx context.Context) {
	key, err := os.ReadFile(t.PrivKeyPath)
	if err != nil {
		slog.Error("Kritisch: SSH-Private-Key für Tunnel nicht gefunden", "error", err)
		return
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		slog.Error("Kritisch: Ungültiger SSH-Private-Key", "error", err)
		return
	}

	config := &ssh.ClientConfig{
		User: "sentinel-tunnel",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		Timeout: 15 * time.Second,
	}
	if t.KnownHostsPath == "" {
		slog.Error("SSH-Tunnel wird aus Sicherheitsgründen abgebrochen: Known-Hosts-Datei fehlt")
		return
	}
	knownHostsInfo, err := os.Stat(t.KnownHostsPath)
	if err != nil || !knownHostsInfo.Mode().IsRegular() {
		slog.Error("SSH-Tunnel wird aus Sicherheitsgründen abgebrochen: Known-Hosts-Datei ist nicht verfügbar", "path", t.KnownHostsPath, "error", err)
		return
	}
	config.HostKeyCallback, err = knownhosts.New(t.KnownHostsPath)
	if err != nil {
		slog.Error("Known-Hosts-Datei für SSH-Tunnel konnte nicht geladen werden", "error", err)
		return
	}

	backoff := initialReconnectBackoff
	for {
		select {
		case <-ctx.Done():
			return
		default:
			connected := t.establishConnection(ctx, config)
			if connected {
				backoff = initialReconnectBackoff
			} else {
				slog.Warn("Tunnel-Verbindung abgebrochen; Reconnect wird geplant", "backoff", backoff)
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if !connected && backoff < maxReconnectBackoff {
				backoff *= 2
				if backoff > maxReconnectBackoff {
					backoff = maxReconnectBackoff
				}
			}
		}
	}
}

func (t *ReverseTunnel) establishConnection(ctx context.Context, config *ssh.ClientConfig) bool {
	client, err := dialSSHContext(ctx, t.HubSSHAddr, config)
	if err != nil {
		slog.Error("SSH Dial fehlgeschlagen", "error", err)
		return false
	}
	defer client.Close()
	stopClose := make(chan struct{})
	defer close(stopClose)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-stopClose:
		}
	}()

	remoteListener, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", t.RemotePort))
	if err != nil {
		slog.Error("Remote Port Forwarding fehlgeschlagen", "port", t.RemotePort, "error", err)
		return false
	}
	defer remoteListener.Close()

	slog.Info("Reverse SSH Tunnel etabliert", "remote_port", t.RemotePort)

	for {
		remoteConn, err := remoteListener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("Fehler bei Tunnel-Accept", "error", err)
			}
			break
		}
		go t.forwardLocal(ctx, remoteConn)
	}
	return ctx.Err() == nil
}

func dialSSHContext(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: config.Timeout}
	rawConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	conn, channels, requests, err := ssh.NewClientConn(rawConn, address, config)
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return ssh.NewClient(conn, channels, requests), nil
}

func (t *ReverseTunnel) forwardLocal(ctx context.Context, remoteConn net.Conn) {
	dialer := net.Dialer{}
	localConn, err := dialer.DialContext(ctx, "tcp", t.LocalAddr)
	if err != nil {
		slog.Error("Lokaler Service nicht erreichbar", "addr", t.LocalAddr, "error", err)
		remoteConn.Close()
		return
	}

	stopClose := make(chan struct{})
	defer close(stopClose)
	go func() {
		select {
		case <-ctx.Done():
			_ = localConn.Close()
			_ = remoteConn.Close()
		case <-stopClose:
		}
	}()
	go copyConn(localConn, remoteConn)
	go copyConn(remoteConn, localConn)
}

func copyConn(writer, reader net.Conn) {
	defer writer.Close()
	defer reader.Close()
	buffer := make([]byte, 32768)
	for {
		bytesRead, err := reader.Read(buffer)
		if err != nil {
			break
		}
		_, err = writer.Write(buffer[:bytesRead])
		if err != nil {
			break
		}
	}
}
