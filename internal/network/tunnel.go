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
	config.HostKeyCallback, err = knownhosts.New(t.KnownHostsPath)
	if err != nil {
		slog.Error("Known-Hosts-Datei für SSH-Tunnel konnte nicht geladen werden", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			t.establishConnection(config)
			slog.Warn("Tunnel-Verbindung abgebrochen. Versuche Reconnect in 10 Sekunden...")
			time.Sleep(10 * time.Second)
		}
	}
}

func (t *ReverseTunnel) establishConnection(config *ssh.ClientConfig) {
	client, err := ssh.Dial("tcp", t.HubSSHAddr, config)
	if err != nil {
		slog.Error("SSH Dial fehlgeschlagen", "error", err)
		return
	}
	defer client.Close()

	remoteListener, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", t.RemotePort))
	if err != nil {
		slog.Error("Remote Port Forwarding fehlgeschlagen", "port", t.RemotePort, "error", err)
		return
	}
	defer remoteListener.Close()

	slog.Info("Reverse SSH Tunnel etabliert", "remote_port", t.RemotePort)

	for {
		remoteConn, err := remoteListener.Accept()
		if err != nil {
			slog.Error("Fehler bei Tunnel-Accept", "error", err)
			break
		}
		go t.forwardLocal(remoteConn)
	}
}

func (t *ReverseTunnel) forwardLocal(remoteConn net.Conn) {
	localConn, err := net.Dial("tcp", t.LocalAddr)
	if err != nil {
		slog.Error("Lokaler Service nicht erreichbar", "addr", t.LocalAddr, "error", err)
		remoteConn.Close()
		return
	}

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
