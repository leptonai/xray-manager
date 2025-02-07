package ssh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/utils/exec"

	. "github.com/leptonai/xray-manager/pkg/logger"
	"github.com/leptonai/xray-manager/pkg/util"
)

type SSH struct {
	User           string
	Pass           string
	Host           string
	Port           int
	PrivateKey     string
	PrivateKeyFile string
}

type Op struct {
	sudo            bool
	retries         int
	retryInterval   time.Duration
	retryErrHandler func(error) bool
}

type OpOption func(*Op)

func (op *Op) applyOpts(opts []OpOption) {
	for _, opt := range opts {
		opt(op)
	}
}

func WithSudo(b bool) OpOption {
	return func(op *Op) { op.sudo = b }
}

func (s *SSH) setup(opts ...OpOption) (*Op, error) {
	if s.User == "" {
		return nil, errors.New("empty username")
	}

	if !s.PrivateKeyOrPassSet() {
		return nil, errors.New("empty password, both key and key file path are also empty")
	}

	if s.Pass != "" {
		// ref. https://serverfault.com/questions/241588/how-to-automate-ssh-login-with-password
		// ref. https://github.com/clarkwang/passh
		p, err := exec.New().LookPath("passh")
		if err != nil {
			return nil, fmt.Errorf("password authentication requires passh (%w)", err)
		}
		Logger.Infow("using passh to connect to remote host with password", "path", p)
	}

	// "PrivateKey" is not easy to use directly with "ssh" commands
	// so we write it to a file and use it with "-i" flag
	if s.PrivateKey != "" {
		alreadyExists := false
		if s.PrivateKeyFile != "" {
			var err error
			alreadyExists, err = util.CheckPathExists(s.PrivateKeyFile)
			if err != nil {
				return nil, err
			}
		}
		if !alreadyExists {
			keyFile := filepath.Join(os.TempDir(), util.AlphabetsLowerCase(32))
			if err := os.WriteFile(keyFile, []byte(s.PrivateKey), 0600); err != nil {
				return nil, err
			}
			s.PrivateKeyFile = keyFile
		}
	}

	if s.PrivateKeyFile != "" {
		if err := os.Chmod(s.PrivateKeyFile, 0644); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := exec.New().CommandContext(ctx, "chmod", "0600", s.PrivateKeyFile).CombinedOutput()
		cancel()
		if err != nil {
			return nil, err
		}
	}

	ret := &Op{
		sudo:            false,
		retryErrHandler: func(error) bool { return false },
	}
	ret.applyOpts(opts)

	return ret, nil
}

func (s *SSH) PrivateKeyOrPassSet() bool {
	return s.PrivateKey != "" || s.PrivateKeyFile != "" || s.Pass != ""
}

func (s *SSH) Cleanup() {
	if s.PrivateKeyFile != "" {
		Logger.Infow("removing key file", "path", s.PrivateKeyFile)
		if err := os.RemoveAll(s.PrivateKeyFile); err != nil {
			Logger.Warnw("failed to remove key file", "path", s.PrivateKeyFile, "err", err)
		}
	}
	s.PrivateKey = ""
	s.PrivateKeyFile = ""
	s.Pass = ""
}

func runRetries(ctx context.Context, f func() ([]byte, bool, error), opts ...OpOption) ([]byte, error) {
	ret := &Op{
		retries:       1,
		retryInterval: 2 * time.Second,
	}
	ret.applyOpts(opts)

	var b []byte
	var retryable bool
	var err error
	for i := 0; i < ret.retries; i++ {
		b, retryable, err = f()
		if err == nil {
			break
		}
		if !retryable {
			break
		}

		Logger.Warnw("retryable error", "retries", i, "totalRetries", ret.retries, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(ret.retryInterval):
		}
	}
	return b, err
}

func (s *SSH) Run(ctx context.Context, cmds []string, opts ...OpOption) ([]byte, error) {
	return runRetries(
		ctx,
		func() ([]byte, bool, error) {
			return s.run(ctx, cmds, opts...)
		},
		opts...,
	)
}

func (s *SSH) usePassh() bool {
	// only use "passh" when password is set and private key is not set
	return s.Pass != "" && (s.PrivateKey == "" && s.PrivateKeyFile == "")
}

func (s *SSH) args(cmd string, portFlag string) []string {
	args := make([]string, 0)
	if s.usePassh() {
		// single/double quote on password returns "exit status 255"
		args = append(args, "passh", "-p", s.Pass)
	}

	args = append(args, cmd)

	// retain permission bits
	// ref. "scp -p Preserves modification times, access times, and file mode bits from the source file."
	if cmd == "scp" {
		args = append(args, "-p")
	}

	args = append(args,
		"-o", "LogLevel=ERROR",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
	)
	if s.PrivateKeyFile != "" {
		args = append(args, "-i", s.PrivateKeyFile)
	}
	if s.Port != 0 {
		args = append(args, portFlag, fmt.Sprintf("%d", s.Port))
	}
	return args
}

func (s *SSH) sshArgs() []string {
	return s.args("ssh", "-p")
}

func (s *SSH) scpArgs() []string {
	return s.args("scp", "-P")
}

func (s *SSH) run(ctx context.Context, cmds []string, opts ...OpOption) ([]byte, bool, error) {
	ret, err := s.setup(opts...)
	if err != nil {
		return nil, false, err
	}

	args := s.sshArgs()
	args = append(args, s.User+"@"+s.Host)

	if s.usePassh() && (ret.sudo || strings.HasPrefix(cmds[0], "sudo")) && !strings.Contains(cmds[0], "echo ") {
		pfx := fmt.Sprintf("echo '%s' | sudo -S", s.Pass)
		cmds[0] = strings.Replace(cmds[0], "sudo", pfx, 1)
	}
	args = append(args, cmds...)

	Logger.Infow("run", "args", strings.Join(args, " "))
	b, err := exec.New().CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err == nil {
		return b, false, nil
	}
	err = fmt.Errorf("command failed with error: %w and output: %s", err, string(b))
	return b, ret.retryErrHandler(err), err
}

const DefaultBashScriptHeader = `
#!/bin/bash

# do not mask errors in a pipeline
set -o pipefail

# treat unset variables as an error
set -o nounset

# exit script whenever it errs
set -o errexit

### SCRIPT BEGINS HERE
`

func CommandsToBashScript(cmds ...string) string {
	return DefaultBashScriptHeader + strings.Join(cmds, "\n\n#####\n")
}

func (s *SSH) RunScript(ctx context.Context, sc string, opts ...OpOption) ([]byte, error) {
	options := &Op{}
	options.applyOpts(opts)

	file := filepath.Join(os.TempDir(), util.AlphabetsLowerCase(32))
	if err := os.WriteFile(file, []byte(sc), 0600); err != nil {
		return nil, err
	}
	defer os.RemoveAll(file)

	Logger.Infow("created temp file to run the script", "file", file)
	return runRetries(
		ctx,
		func() ([]byte, bool, error) {
			return s.runScriptFromFile(ctx, file, opts...)
		},
		opts...,
	)
}

func (s *SSH) runScriptFromFile(ctx context.Context, file string, opts ...OpOption) ([]byte, bool, error) {
	remoteScriptFile := "/tmp/" + filepath.Base(file) + ".tmp." + util.AlphabetsLowerCase(16)
	retryable, err := s.sendFile(ctx, file, remoteScriptFile, opts...)
	if err != nil {
		return nil, retryable, err
	}

	ret, err := s.setup(opts...)
	if err != nil {
		return nil, false, err
	}

	cmdToRun := "bash " + remoteScriptFile + " 2>&1"
	if ret.sudo {
		cmdToRun = "sudo " + cmdToRun
	}
	b, retryable, err := s.run(ctx, []string{cmdToRun + " && rm -f " + remoteScriptFile}, opts...)
	if err != nil {
		return nil, retryable, err
	}
	return b, false, nil
}

// Returns true if the error is retryable.
func (s *SSH) sendFile(ctx context.Context, local string, remote string, opts ...OpOption) (bool, error) {
	ret, err := s.setup(opts...)
	if err != nil {
		return false, err
	}

	tmpFile := "/" + filepath.Join("tmp", util.AlphabetsLowerCase(32))

	args := s.scpArgs()
	args = append(args, local, s.User+"@"+s.Host+":"+tmpFile)

	Logger.Infow("sending file", "local", local, "remote", remote, "sudo", ret.sudo, "command", strings.Join(args, " "))
	if _, err := exec.New().CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		return ret.retryErrHandler(err), err
	}
	Logger.Infow("successfully sent file", "local", local)

	var mvArgs []string
	if ret.sudo {
		mvArgs = append(mvArgs, "sudo")
	}
	mvArgs = append(mvArgs, "mv", tmpFile, remote)
	if _, err := s.Run(ctx, mvArgs, opts...); err != nil {
		return ret.retryErrHandler(err), err
	}

	Logger.Infow("successfully moved sent file", "tmpFile", tmpFile, "remote", remote)
	return false, nil
}
