package gitconnector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type gitRunner interface {
	Run(ctx context.Context, env []string, args ...string) ([]byte, error)
}

type nativeGitRunner struct{}

func (nativeGitRunner) Run(ctx context.Context, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		const maxOutput = 2048
		message := strings.TrimSpace(string(output))
		if len(message) > maxOutput {
			message = message[:maxOutput] + "..."
		}
		return nil, fmt.Errorf("git command failed: %w: %s", err, message)
	}
	return output, nil
}

type authSession struct {
	env     []string
	root    string
	cleanup func()
}

func newAuthSession(cfg *Config) (*authSession, error) {
	root, err := os.MkdirTemp("", "weknora-git-auth-*")
	if err != nil {
		return nil, fmt.Errorf("create git auth directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	session := &authSession{
		root: root,
		env: []string{
			"GIT_TERMINAL_PROMPT=0",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_ALLOW_PROTOCOL=https:ssh",
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.followRedirects",
			"GIT_CONFIG_VALUE_0=false",
		},
		cleanup: cleanup,
	}

	if isSSHRepo(cfg.RepoURL) {
		knownHostsPath := filepath.Join(root, "known_hosts")
		if err := os.WriteFile(knownHostsPath, []byte(cfg.KnownHosts+"\n"), 0o600); err != nil {
			cleanup()
			return nil, fmt.Errorf("write known_hosts: %w", err)
		}
		sshArgs := []string{
			"ssh",
			"-o", "BatchMode=yes",
			"-o", "IdentitiesOnly=yes",
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile=" + knownHostsPath,
		}
		if cfg.PrivateKey != "" {
			keyPath := filepath.Join(root, "identity")
			if err := os.WriteFile(keyPath, []byte(cfg.PrivateKey+"\n"), 0o600); err != nil {
				cleanup()
				return nil, fmt.Errorf("write SSH private key: %w", err)
			}
			sshArgs = append(sshArgs, "-i", keyPath)
		}
		session.env = append(session.env, "GIT_SSH_COMMAND="+strings.Join(sshArgs, " "))
	}

	if cfg.Token != "" && strings.HasPrefix(cfg.RepoURL, "https://") {
		askPassPath := filepath.Join(root, "askpass.sh")
		script := "#!/bin/sh\ncase \"$1\" in\n*Username*) printf '%s' \"$WEKNORA_GIT_USERNAME\" ;;\n*) printf '%s' \"$WEKNORA_GIT_TOKEN\" ;;\nesac\n"
		if err := os.WriteFile(askPassPath, []byte(script), 0o700); err != nil {
			cleanup()
			return nil, fmt.Errorf("write git askpass helper: %w", err)
		}
		username := cfg.Username
		if username == "" {
			username = "oauth2"
		}
		session.env = append(
			session.env,
			"GIT_ASKPASS="+askPassPath,
			"WEKNORA_GIT_USERNAME="+username,
			"WEKNORA_GIT_TOKEN="+cfg.Token,
		)
	}
	return session, nil
}

type gitCheckout struct {
	dir     string
	env     []string
	cleanup func()
}

func cloneRepository(
	ctx context.Context,
	runner gitRunner,
	cfg *Config,
) (*gitCheckout, error) {
	auth, err := newAuthSession(cfg)
	if err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp("", "weknora-git-checkout-*")
	if err != nil {
		auth.cleanup()
		return nil, fmt.Errorf("create git checkout directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(root)
		auth.cleanup()
	}
	destination := filepath.Join(root, "repo")
	_, err = runner.Run(
		ctx,
		auth.env,
		"clone",
		"--quiet",
		"--no-checkout",
		"--filter=blob:none",
		"--depth=1",
		"--single-branch",
		"--branch", cfg.Branch,
		"--",
		cfg.RepoURL,
		destination,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("clone repository: %w", err)
	}
	return &gitCheckout{dir: destination, env: auth.env, cleanup: cleanup}, nil
}
