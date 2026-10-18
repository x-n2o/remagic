// Package device speaks SSH to a reMarkable in developer mode. Pure Go —
// no ssh binary, no ControlMaster, works the same on macOS/Linux/Windows.
package device

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// The tablet's fixed address on the USB ethernet gadget.
const DefaultUSBAddr = "10.11.99.1"

const ApploadDir = "/home/root/xovi/exthome/appload"

type Device struct {
	Addr        string
	client      *ssh.Client
	config      *ssh.ClientConfig
	password    string
	hasPassword bool
}

// nonInteractiveAuths collects the auth methods that never prompt: the
// ssh-agent plus the usual key files.
func nonInteractiveAuths() ([]ssh.AuthMethod, func()) {
	var signers []ssh.Signer
	var agentConns []net.Conn
	home, _ := os.UserHomeDir()
	// OpenSSH reads IdentityAgent from ssh_config; the pure-Go CLI does not.
	// Also try 1Password's standard Unix sockets without changing ssh_config.
	sockets := []string{os.Getenv("SSH_AUTH_SOCK"), filepath.Join(home, ".1password", "agent.sock"),
		filepath.Join(home, "Library", "Group Containers", "2BUA8C4S2C.com.1password", "t", "agent.sock")}
	seen := map[string]bool{}
	for _, sock := range sockets {
		if sock == "" || seen[sock] {
			continue
		}
		seen[sock] = true
		if conn, err := net.Dial("unix", sock); err == nil {
			agentConns = append(agentConns, conn)
			if keys, err := agent.NewClient(conn).Signers(); err == nil {
				signers = append(signers, keys...)
			}
		}
	}
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		b, err := os.ReadFile(filepath.Join(home, ".ssh", name))
		if err != nil {
			continue
		}
		if signer, err := ssh.ParsePrivateKey(b); err == nil {
			signers = append(signers, signer)
		}
	}
	var auths []ssh.AuthMethod
	if len(signers) > 0 {
		auths = append(auths, ssh.PublicKeys(signers...))
	}
	return auths, func() {
		for _, conn := range agentConns {
			conn.Close()
		}
	}
}

// ConnectKeyOnly dials with only non-interactive auth (agent + key files) on
// a genuinely fresh connection. Use it to prove the NEXT person to connect
// will get in — e.g. after an installer step that can wedge the SSH server.
func ConnectKeyOnly(addr string) (*Device, error) {
	auths, closeAgent := nonInteractiveAuths()
	defer closeAgent()
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	}
	client, err := dialSSH(net.JoinHostPort(addr, "22"), cfg)
	if err != nil {
		return nil, fmt.Errorf("connect root@%s: %w", addr, err)
	}
	return &Device{Addr: addr, client: client, config: cfg}, nil
}

// Connect dials root@addr trying, in order: the ssh-agent, the usual key
// files, then an interactive password prompt (the code on the tablet under
// Settings → Help → Copyrights and licenses).
func Connect(addr string) (*Device, error) {
	auths, closeAgent := nonInteractiveAuths()
	defer closeAgent()
	var password string
	hasPassword := false
	auths = append(auths, ssh.RetryableAuthMethod(ssh.PasswordCallback(func() (string, error) {
		fmt.Fprintf(os.Stderr, "password for root@%s (tablet: Settings → Help → Copyrights and licenses): ", addr)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		password = strings.TrimSpace(string(b))
		hasPassword = err == nil
		return password, err
	}), 3))
	cfg := &ssh.ClientConfig{
		User: "root",
		Auth: auths,
		// The device regenerates its host key on every factory reset, and
		// first contact is exactly when this tool runs — pinning would strand
		// users at the moment they need it. Trust-on-connect, like the
		// installer always has.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	}
	// Interactive password/agent approval time must not count as a stalled
	// handshake. Reconnects below are non-interactive and have a deadline.
	client, err := ssh.Dial("tcp", net.JoinHostPort(addr, "22"), cfg)
	if err != nil {
		return nil, fmt.Errorf("connect root@%s: %w", addr, err)
	}
	return &Device{Addr: addr, client: client, config: cfg, password: password, hasPassword: hasPassword}, nil
}

// Fresh reconnects using the successful authentication configuration. Passwords
// are retained only in memory, so skipping key installation also works when
// setup temporarily interrupts the connection.
func (d *Device) Fresh() (*Device, error) {
	if d.config == nil {
		return ConnectKeyOnly(d.Addr)
	}
	// Agents can close idle connections while the device rebuilds resources.
	// Open a new agent connection for every handshake; never reuse stale signers.
	auths, closeAgent := nonInteractiveAuths()
	defer closeAgent()
	if d.hasPassword {
		auths = append(auths, ssh.Password(d.password))
	}
	cfg := *d.config
	cfg.Auth = auths
	endpoint := net.JoinHostPort(d.Addr, "22")
	if d.client != nil {
		endpoint = d.client.RemoteAddr().String()
	}
	client, err := dialSSH(endpoint, &cfg)
	if err != nil {
		return nil, fmt.Errorf("reconnect root@%s: %w", d.Addr, err)
	}
	return &Device{Addr: d.Addr, client: client, config: &cfg, password: d.password, hasPassword: d.hasPassword}, nil
}

// ClientConfig.Timeout only bounds the TCP dial. Bound the SSH handshake too:
// xochitl restarts can leave an accepted USB connection silent indefinitely.
func dialSSH(endpoint string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := net.DialTimeout("tcp", endpoint, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(cfg.Timeout))
	transport, channels, requests, err := ssh.NewClientConn(conn, endpoint, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return ssh.NewClient(transport, channels, requests), nil
}

func (d *Device) Close() {
	if d.client != nil {
		d.client.Close()
	}
}

// Run executes one command; returns combined output.
func (d *Device) Run(cmd string) (string, error) {
	return d.RunIn(cmd, nil)
}

// RunIn executes one command with stdin streamed from r.
func (d *Device) RunIn(cmd string, r io.Reader) (string, error) {
	sess, err := d.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	// stdout and stderr are copied by separate goroutines; feeding them the
	// same bare buffer is a data race that intermittently loses output.
	var out bytes.Buffer
	lw := &lockedWriter{w: &out}
	sess.Stdout = lw
	sess.Stderr = lw
	if r != nil {
		sess.Stdin = r
	}
	err = sess.Run(cmd)
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return out.String(), err
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Push writes content to a remote path with the given chmod mode. Streams
// through cat — the BusyBox device needs no sftp subsystem.
func (d *Device) Push(content []byte, remote, mode string) error {
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s && chmod %s %s",
		shq(filepath.Dir(remote)), shq(remote), mode, shq(remote))
	if out, err := d.RunIn(cmd, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("push %s: %w: %s", remote, err, strings.TrimSpace(out))
	}
	return nil
}

// PushDir mirrors a local directory to remoteDir (created if needed) by
// streaming a tar.gz — one round-trip regardless of file count.
func (d *Device) PushDir(localDir, remoteDir string) error {
	pr, pw := io.Pipe()
	go func() {
		gz := gzip.NewWriter(pw)
		tw := tar.NewWriter(gz)
		err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(localDir, path)
			if err != nil || rel == "." {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			// Everything on the device is root's; don't leak local uids.
			hdr.Uid, hdr.Gid = 0, 0
			hdr.Uname, hdr.Gname = "root", "root"
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				if _, err := io.Copy(tw, f); err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			err = tw.Close()
		} else {
			tw.Close()
		}
		if e := gz.Close(); err == nil {
			err = e
		}
		pw.CloseWithError(err)
	}()
	cmd := fmt.Sprintf("mkdir -p %s && tar -xzf - -C %s", shq(remoteDir), shq(remoteDir))
	if out, err := d.RunIn(cmd, pr); err != nil {
		return fmt.Errorf("push dir %s: %w: %s", remoteDir, err, strings.TrimSpace(out))
	}
	return nil
}

// shq single-quotes s for a POSIX shell.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
