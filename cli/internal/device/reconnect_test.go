package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestFreshRetainsPasswordWithoutInstallingKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	serverCfg := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) != "test-password" {
			return nil, fmt.Errorf("wrong password")
		}
		return nil, nil
	}}
	serverCfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	errors := make(chan error, 2)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				errors <- err
				return
			}
			server, channels, requests, err := ssh.NewServerConn(conn, serverCfg)
			if err != nil {
				errors <- err
				return
			}
			go ssh.DiscardRequests(requests)
			go func() {
				for channel := range channels {
					channel.Reject(ssh.Prohibited, "no sessions needed")
				}
			}()
			go func() { server.Wait(); server.Close() }()
			errors <- nil
		}
	}()
	cfg := &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.Password("test-password")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second}
	client, err := dialSSH(listener.Addr().String(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	d := &Device{Addr: "127.0.0.1", client: client, config: cfg, password: "test-password", hasPassword: true}
	defer d.Close()
	fresh, err := d.Fresh()
	if err != nil {
		t.Fatalf("password reconnect: %v", err)
	}
	fresh.Close()
	for i := 0; i < 2; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh")); !os.IsNotExist(err) {
		t.Fatal("password reconnect wrote SSH keys")
	}
}

func TestSSHHandshakeHasDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	start := time.Now()
	_, err = dialSSH(listener.Addr().String(), &ssh.ClientConfig{User: "root", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("accepted a silent SSH peer")
	}
	if time.Since(start) > time.Second {
		t.Fatal("SSH handshake did not respect its deadline")
	}
	select {
	case conn := <-accepted:
		conn.Close()
	default:
	}
}
