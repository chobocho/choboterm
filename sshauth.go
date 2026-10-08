package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// errKeyCancelled means the user cancelled the passphrase prompt of a key the server accepts.
var errKeyCancelled = errors.New("개인키 암호 입력을 취소했습니다")

// unlockedKeys keeps keys decrypted with a passphrase until choboterm exits,
// so reconnecting or opening another pane to the server doesn't ask again.
var unlockedKeys = struct {
	sync.Mutex
	m map[string]ssh.Signer // by cleaned, lower-case file path
}{m: map[string]ssh.Signer{}}

func keyID(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// sshAuth collects the authentication methods for one connection.
type sshAuth struct {
	methods []ssh.AuthMethod
	agents  []io.Closer // agent connections, open until the login is done
}

func (a *sshAuth) Close() {
	for _, c := range a.agents {
		_ = c.Close()
	}
}

// newSSHAuth offers public keys first: keys held by an SSH agent (OpenSSH for
// Windows, Pageant or SSH_AUTH_SOCK), then key files (keys, then the default
// ones in ~/.ssh). A file key protected by a passphrase is offered too, and
// the passphrase is asked only when the server accepts that key. After the
// keys come the password and keyboard-interactive.
func newSSHAuth(pass string, keys []string, ask asker) *sshAuth {
	a := &sshAuth{}
	var signers []ssh.Signer
	have := map[string]bool{} // public keys already offered
	add := func(s ssh.Signer) {
		k := string(s.PublicKey().Marshal())
		if !have[k] {
			have[k] = true
			signers = append(signers, s)
		}
	}

	for _, src := range agentSources() {
		conn, err := src.dial()
		if err != nil {
			continue
		}
		// Without Close the client reads only for each request (no reader left
		// waiting on the pipe), so closing the conn after the login can't block.
		list, err := agent.NewClient(struct{ io.ReadWriter }{conn}).Signers()
		if err != nil {
			debugf("ssh agent %s: %v", src.name, err)
			conn.Close()
			continue
		}
		debugf("ssh agent %s: %d key(s)", src.name, len(list))
		a.agents = append(a.agents, conn)
		for _, s := range list {
			add(s)
		}
	}

	for _, f := range keyFiles(keys) {
		if s := loadKeyFile(f, ask); s != nil {
			add(s)
		}
	}
	if len(signers) > 0 {
		a.methods = append(a.methods, ssh.PublicKeys(signers...))
	}

	if pass != "" {
		a.methods = append(a.methods,
			ssh.Password(pass),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					if !echos[i] {
						answers[i] = pass
					}
				}
				return answers, nil
			}),
		)
	}
	return a
}

// keyFiles returns keys followed by the default key files, without duplicates.
func keyFiles(keys []string) []string {
	files := slices.Clone(keys)
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			files = append(files, filepath.Join(home, ".ssh", name))
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range files {
		if id := keyID(f); !seen[id] {
			seen[id] = true
			out = append(out, f)
		}
	}
	return out
}

// loadKeyFile returns a signer for a private key file, or nil if it can't be used.
func loadKeyFile(path string, ask asker) ssh.Signer {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return s
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		debugf("key %s: %v", path, err)
		return nil
	}
	unlockedKeys.Lock()
	s = unlockedKeys.m[keyID(path)]
	unlockedKeys.Unlock()
	if s != nil {
		return s
	}
	lazy := &lockedKey{path: path, data: data, ask: ask, pub: missing.PublicKey}
	if lazy.pub == nil {
		// Old PEM keys don't carry the public key; it may be in the .pub file.
		if pubData, err := os.ReadFile(path + ".pub"); err == nil {
			if pub, _, _, _, err := ssh.ParseAuthorizedKey(pubData); err == nil {
				lazy.pub = pub
			}
		}
	}
	if lazy.pub != nil {
		return lazy
	}
	// Without its public key the key can't be offered first: unlock it now.
	// Cancelling here just leaves the key out.
	s, err = lazy.unlock()
	if err != nil {
		return nil
	}
	return s
}

// lockedKey is a passphrase-protected key file. It is offered by its public
// key; the passphrase is asked the first time the server wants a signature.
type lockedKey struct {
	path string
	data []byte
	pub  ssh.PublicKey
	ask  asker

	mu     sync.Mutex
	signer ssh.Signer
}

func (k *lockedKey) PublicKey() ssh.PublicKey { return k.pub }

func (k *lockedKey) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	s, err := k.unlock()
	if err != nil {
		return nil, err
	}
	return s.Sign(rand, data)
}

// SignWithAlgorithm lets RSA keys use rsa-sha2-256/512, which servers require today.
func (k *lockedKey) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*ssh.Signature, error) {
	s, err := k.unlock()
	if err != nil {
		return nil, err
	}
	as, ok := s.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("%s: 서명 방식 %s을 쓸 수 없습니다", k.path, algorithm)
	}
	return as.SignWithAlgorithm(rand, data, algorithm)
}

// unlock asks for the passphrase (up to three times) and decrypts the key.
func (k *lockedKey) unlock() (ssh.Signer, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.signer != nil {
		return k.signer, nil
	}
	if k.ask == nil {
		return nil, errKeyCancelled
	}
	msg := fmt.Sprintf("%s 키의 암호를 입력하세요.", k.path)
	for try := 0; try < 3; try++ {
		vals, ok := k.ask("SSH 키 암호", msg, []PromptField{{Label: "암호", Secret: true}})
		if !ok {
			return nil, errKeyCancelled
		}
		s, err := ssh.ParsePrivateKeyWithPassphrase(k.data, []byte(vals[0]))
		if err != nil {
			debugf("key %s: %v", k.path, err)
			msg = fmt.Sprintf("암호가 틀렸습니다. 다시 입력하세요.\n%s", k.path)
			continue
		}
		k.signer = s
		unlockedKeys.Lock()
		unlockedKeys.m[keyID(k.path)] = s
		unlockedKeys.Unlock()
		return s, nil
	}
	return nil, fmt.Errorf("%s 키의 암호가 3번 틀렸습니다", k.path)
}

// agentSource is somewhere an SSH agent may be listening.
type agentSource struct {
	name string
	dial func() (io.ReadWriteCloser, error)
}

// agentSources lists the agents to ask for keys; tests replace it.
var agentSources = func() []agentSource {
	var list []agentSource
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		list = append(list, agentSource{"SSH_AUTH_SOCK", func() (io.ReadWriteCloser, error) { return dialAgentPath(sock) }})
	}
	return append(list, platformAgentSources()...)
}

// dialAgentPath connects to an agent at a Windows named pipe (\\.\pipe\...) or a Unix socket.
func dialAgentPath(path string) (io.ReadWriteCloser, error) {
	p := strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(p, `\\.\pipe\`) {
		return os.OpenFile(p, os.O_RDWR, 0)
	}
	return net.Dial("unix", path)
}
