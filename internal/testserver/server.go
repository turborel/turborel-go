// Package testserver runs a turborel server for the driver's live tests
// (RULES.md R-TEST-8): the server named by the environment's
// TURBOREL_TEST_SERVER, a turboreld program's path, or `docker:` and an
// image such as ghcr.io/turborel/turborel:nightly, run with the host's
// network. It listens on turborel-go's own port, 17433, and a Unix socket
// in a directory of its own, with TLS by certificates made as it starts,
// a client certificate authority, SERVER.TRUST_LOCAL on, and an owner of
// a password. It is test support, no part of the driver.
package testserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The live suite's set-up.
const (
	// EnvServer names the server the live tests run against; unset, they
	// are skipped.
	EnvServer = "TURBOREL_TEST_SERVER"
	// Port is turborel-go's own port for live servers.
	Port = 17433
	// Owner and OwnerPassword are the database's owner, its first user.
	Owner         = "ANN"
	OwnerPassword = "ann's secret"
	// dockerPrefix begins a server named by its image.
	dockerPrefix = "docker:"
	// startWithin is how long the server has to listen.
	startWithin = 60 * time.Second
	// listening begins the line the server prints once it listens.
	listening = "turboreld: listening at"
	// open lets a server of another user, in a container, read the
	// directory and make its socket there.
	open = 0o777
	// iterations makes logins quick; the tests prove passwords often.
	iterations = "10000"
)

// errNotListening is a server that stopped, or never said it listens.
var errNotListening = errors.New("the server did not start listening")

// Files are the certificates and keys a server and its client use.
type Files struct {
	CA         string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// Server is a running server.
type Server struct {
	// Address is its TCP address; Socket its Unix socket, empty on
	// Windows.
	Address string
	Socket  string
	Files   *Files
	cmd     *exec.Cmd
	dir     string
}

// Spec gives the server the environment names, and whether it names
// one.
func Spec() (string, bool) {
	spec := os.Getenv(EnvServer)

	return spec, spec != ""
}

// Start starts the server spec names, with a directory of its own.
func Start(spec string) (*Server, error) {
	dir, err := os.MkdirTemp("", "tgo")
	if err != nil {
		return nil, fmt.Errorf("a directory: %w", err)
	}

	if err := os.Chmod(dir, open); err != nil {
		return nil, fmt.Errorf("a directory: %w", err)
	}

	files, err := writeCerts(dir, strings.ToLower(Owner))
	if err != nil {
		return nil, err
	}

	s := &Server{Address: "127.0.0.1:" + strconv.Itoa(Port), Files: files, dir: dir}
	if runtime.GOOS != "windows" {
		s.Socket = filepath.Join(dir, "s.sock")
	}

	if err := s.run(spec); err != nil {
		_ = os.RemoveAll(dir) // The start's failure is the one given.

		return nil, err
	}

	return s, nil
}

// waitListening reads the server's output until it says it listens.
func waitListening(out io.Reader) error {
	said := make(chan bool, 1)

	go func() {
		lines := bufio.NewScanner(out)
		for lines.Scan() {
			if strings.HasPrefix(lines.Text(), listening) {
				said <- true

				return
			}
		}
		said <- false
	}()

	select {
	case ok := <-said:
		if ok {
			return nil
		}
	case <-time.After(startWithin):
	}

	return errNotListening
}

// Stop stops the server, waiting for its process alone, and removes its
// directory.
func (s *Server) Stop() error {
	defer func() { _ = os.RemoveAll(s.dir) }() // A directory left behind harms nothing.

	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		_ = s.cmd.Process.Kill() // Windows takes no interrupt; a process already gone needs no kill.
	} else {
		_ = s.cmd.Process.Signal(os.Interrupt) // A process already gone needs no signal.
	}

	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait() // The server's exit status is no test's concern.
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(startWithin):
		_ = s.cmd.Process.Kill() // A server that will not stop is killed.
		<-done
	}

	return nil
}

// settings are the server's settings, as --set takes them.
func (s *Server) settings() []string {
	set := []string{
		"SERVER.PORT=" + strconv.Itoa(Port),
		"SERVER.TRUST_LOCAL=ON",
		"SERVER.TLS=ON",
		"SERVER.TLS_CERTIFICATE=" + s.Files.ServerCert,
		"SERVER.TLS_KEY=" + s.Files.ServerKey,
		"SERVER.TLS_CLIENT_CA=" + s.Files.CA,
		"SERVER.PASSWORD_ITERATIONS=" + iterations,
	}
	if s.Socket != "" {
		set = append(set, "SERVER.SOCKET="+s.Socket)
	}

	// Two arguments name the database, two each setting.
	const fixed, perSetting = 2, 2

	args := make([]string, 0, fixed+perSetting*len(set))
	args = append(args, "--dir", s.data())

	for _, setting := range set {
		args = append(args, "--set", setting)
	}

	return args
}

// data is the server's data directory: a database kept in files, as a
// server keeps it, so its commits have log positions.
func (s *Server) data() string { return filepath.Join(s.dir, "data") }

// command is the command of spec, a program or an image, run with args.
func (s *Server) command(spec string, args ...string) *exec.Cmd {
	if image, ok := strings.CutPrefix(spec, dockerPrefix); ok {
		// Run as the tests' own user, so the socket it makes is theirs to
		// connect to and its files theirs to remove.
		user := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
		run := []string{"run", "--rm", "-i", "--network", "host", "--user", user, "-v", s.dir + ":" + s.dir, image}

		return exec.CommandContext(context.Background(), "docker", slices.Concat(run, args)...)
	}

	return exec.CommandContext(context.Background(), spec, args...)
}

// run makes the database's owner, then starts the server and waits for
// it to listen.
func (s *Server) run(spec string) error {
	owner := s.command(spec, "--dir", s.data(), "user", "add", Owner, "--owner")
	owner.Stdin = strings.NewReader(OwnerPassword + "\n")

	if out, err := owner.CombinedOutput(); err != nil {
		return fmt.Errorf("making the owner: %w: %s", err, out)
	}

	s.cmd = s.command(spec, s.settings()...)

	// The server says where it listens on standard error, its log too.
	out, err := s.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("the server's output: %w", err)
	}

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("starting the server: %w", err)
	}

	if err := waitListening(out); err != nil {
		_ = s.Stop() // The start's failure is the one given.

		return err
	}

	go func() { _, _ = io.Copy(io.Discard, out) }() // Drained so the server never blocks on its output.

	return nil
}
