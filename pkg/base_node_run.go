package gont

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/gopacket/gopacket/pcapgo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapio"
)

func (n *BaseNode) Command(name string, args ...string) *exec.Cmd {
	return n.CommandWith(name, nil, "", args...)
}

func (n *BaseNode) CommandWith(name string, env []string, dir string, args ...string) *exec.Cmd {
	// Actual namespace switching is done similar to Docker's reexec
	// in a forked version of ourself by passing all required details
	// in environment variables.

	c := exec.Command(name, args...)

	if !n.NsHandle.Equal(n.network.HostNode.NsHandle) {
		if n.ExistingDockerContainer == "" {
			c.Path = "/proc/self/exe"
			c.Env = append(os.Environ(),
				"GONT_UNSHARE=exec",
				"GONT_NODE="+n.name,
				"GONT_NETWORK="+n.network.Name)
		} else {
			c.Path = "/usr/bin/docker"
			c.Args = append([]string{"docker", "exec", n.ExistingDockerContainer, name}, args...)
		}
	}

	for k, v := range n.Env {
		env := fmt.Sprintf("%s=%v", k, v)
		c.Env = append(c.Env, env)
	}

	c.Env = append(c.Env, env...)
	c.Dir = dir

	return c
}

func (n *BaseNode) Run(cmd string, args ...any) ([]byte, *exec.Cmd, error) {
	return n.RunWith(cmd, nil, "", args...)
}

func (n *BaseNode) RunWith(cmd string, env []string, dir string, args ...any) ([]byte, *exec.Cmd, error) {
	stdout, stderr, c, err := n.StartWith(cmd, env, dir, args...)
	if err != nil {
		return nil, nil, err
	}

	combined := io.MultiReader(stdout, stderr)
	buf, err := io.ReadAll(combined)
	if err != nil {
		return nil, nil, err
	}

	if err = c.Wait(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, nil, err
		}
	}

	rlogger := n.logger.With(
		zap.Any("node", n),
		zap.String("cmd", cmd),
		zap.Any("cmd_args", args),
		zap.Int("pid", c.Process.Pid),
		zap.Int("rc", c.ProcessState.ExitCode()),
		zap.Duration("sys_time", c.ProcessState.SystemTime()),
	)

	if c.ProcessState.Success() {
		rlogger.Info("Process terminated successfully")
	} else {
		rlogger.Error("Process terminated with error code")
	}

	return buf, c, err
}

func (n *BaseNode) Start(cmd string, args ...any) (io.Reader, io.Reader, *exec.Cmd, error) {
	return n.StartWith(cmd, nil, "", args...)
}

func (n *BaseNode) StartWith(cmd string, env []string, dir string, args ...any) (io.Reader, io.Reader, *exec.Cmd, error) {
	var err error
	var stdout, stderr io.Reader

	strargs := []string{}
	for _, arg := range args {
		var strarg string
		switch arg := arg.(type) {
		case Node:
			strarg = arg.Name()
		case fmt.Stringer:
			strarg = arg.String()
		case string:
			strarg = arg
		case int:
			strarg = strconv.FormatInt(int64(arg), 10)
		case uint:
			strarg = strconv.FormatUint(uint64(arg), 10)
		case int32:
			strarg = strconv.FormatInt(int64(arg), 10)
		case uint32:
			strarg = strconv.FormatUint(uint64(arg), 10)
		case int64:
			strarg = strconv.FormatInt(arg, 10)
		case uint64:
			strarg = strconv.FormatUint(arg, 10)
		case float32:
			strarg = strconv.FormatFloat(float64(arg), 'f', -1, 32)
		case float64:
			strarg = strconv.FormatFloat(arg, 'f', -1, 64)
		case bool:
			strarg = strconv.FormatBool(arg)
		default:
			return nil, nil, nil, fmt.Errorf("invalid argument: %v", arg)
		}

		strargs = append(strargs, strarg)
	}

	c := n.CommandWith(cmd, env, dir, strargs...)

	if stdout, err = c.StdoutPipe(); err != nil {
		return nil, nil, nil, err
	}

	if stderr, err = c.StderrPipe(); err != nil {
		return nil, nil, nil, err
	}

	// Add some IPC pipes to capture decryption secrets
	for envName, secretsType := range map[string]uint32{
		"SSLKEYLOGFILE": pcapgo.DSB_SECRETS_TYPE_TLS,
		"WG_KEYLOGFILE": pcapgo.DSB_SECRETS_TYPE_WIREGUARD,
	} {
		if pipe, err := n.network.KeyLogPipe(secretsType); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to open TLS key log pipe: %w", err)
		} else if pipe != nil {
			fd := len(c.ExtraFiles) + 3

			c.ExtraFiles = append(c.ExtraFiles, pipe)
			c.Env = append(c.Env, fmt.Sprintf("%s=/proc/self/fd/%d", envName, fd))
		}
	}

	logger := n.logger.With(
		zap.String("cmd", cmd),
		zap.Any("cmd_args", strargs),
	)

	if err = c.Start(); err != nil {
		logger.Error("Failed to start", zap.Error(err))

		return nil, nil, c, err
	}

	logger = logger.With(
		zap.Int("pid", c.Process.Pid),
	)

	logger.Info("Process started")

	if n.LogToDebug {
		slogger := zap.L().With(zap.Int("pid", c.Process.Pid))

		logStdout := &zapio.Writer{
			Log:   slogger,
			Level: zap.InfoLevel,
		}

		logStderr := &zapio.Writer{
			Log:   slogger,
			Level: zap.WarnLevel,
		}

		outReader, outWriter := io.Pipe()
		errReader, errWriter := io.Pipe()

		stdout = io.TeeReader(stdout, outWriter)
		stderr = io.TeeReader(stderr, errWriter)

		go io.Copy(logStdout, outReader)
		go io.Copy(logStderr, errReader)
	}

	return stdout, stderr, c, nil
}

func (n *BaseNode) StartGo(script string, args ...any) (io.Reader, io.Reader, *exec.Cmd, error) {
	tmp := filepath.Join(n.network.VarPath, fmt.Sprintf("go-build-%d", rand.Intn(1<<16)))

	if out, _, err := n.network.HostNode.Run("go", "build", "-o", tmp, script); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to compile Go code: %w\n%s", err, string(out))
	}

	return n.Start(tmp, args...)
}

func (n *BaseNode) RunGo(script string, args ...any) ([]byte, *exec.Cmd, error) {
	tmp := filepath.Join(n.network.TmpPath, fmt.Sprintf("go-build-%d", rand.Intn(1<<16)))

	if _, _, err := n.network.HostNode.Run("go", "build", "-o", tmp, script); err != nil {
		return nil, nil, fmt.Errorf("failed to compile Go code: %w", err)
	}

	return n.Run(tmp, args...)
}
