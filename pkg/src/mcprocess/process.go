package mcprocess

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/imobulus/subchat-mc-server/src/util/processutil"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type McProcessConfig struct {
	MaxMemoryGigabytes  int           `yaml:"max memory gigabytes"`
	KillJavaTimeout     time.Duration `yaml:"kill java timeout"`
	StartupCommandsPath string        `yaml:"startup commands path"`
}

var DefaultMcProcessConfig = McProcessConfig{
	MaxMemoryGigabytes:  4,
	KillJavaTimeout:     time.Minute,
	StartupCommandsPath: "",
}

type McProcessHolder struct {
	config  McProcessConfig
	command *exec.Cmd

	commandsPipe io.Writer

	cmdMu   *sync.Mutex
	cmdDone chan struct{}
	logger  *zap.Logger
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewMcProcessHolder(config McProcessConfig, logger *zap.Logger) *McProcessHolder {
	return &McProcessHolder{
		config:  config,
		cmdMu:   &sync.Mutex{},
		cmdDone: make(chan struct{}),
		logger:  logger,
	}
}

func (m *McProcessHolder) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	if m.command != nil {
		return fmt.Errorf("process already started")
	}
	return m.start()
}

func (m *McProcessHolder) Done() <-chan struct{} {
	return m.cmdDone
}

func (m *McProcessHolder) start() error {
	startupCommands, err := os.ReadFile(m.config.StartupCommandsPath)
	if err != nil {
		return errors.Wrap(err, "cannot read startup commands file")
	}
	cmdArgs := []string{
		"java", fmt.Sprintf("-Xmx%dG", m.config.MaxMemoryGigabytes),
		"-jar", "fabric.jar", "--nogui",
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	pipeOut, pipeIn := io.Pipe()
	cmd.Stdin = pipeOut
	m.commandsPipe = pipeIn
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return err
	}
	m.command = cmd
	go m.waitEnd()
	go m.watchContext()
	go m.scheduleStartupCommands(string(startupCommands))
	return nil
}

func (m *McProcessHolder) waitEnd() {
	err := m.command.Wait()
	if err != nil {
		m.logger.Error("command finished with error", zap.Error(err))
	}
	close(m.cmdDone)
}

func (m *McProcessHolder) watchContext() {
	select {
	case <-m.Done():
		return
	case <-m.ctx.Done():
	}
	processutil.InterruptAndKill(m.command, m.config.KillJavaTimeout)
}

func (m *McProcessHolder) scheduleStartupCommands(startupCommands string) {
	err := m.Exec(startupCommands)
	if err != nil {
		m.logger.Error("cannot execute startup commands", zap.Error(err))
		m.cancel()
	}
}

// Executes command. If command does not start with "/", it is prefixed with "/say "
func (m *McProcessHolder) Exec(commands string) error {
	m.cmdMu.Lock()
	defer m.cmdMu.Unlock()
	for _, command := range strings.Split(commands, "\n") {
		if strings.TrimSpace(command) == "" {
			continue
		}
		if !strings.HasPrefix(command, "/") {
			command = "/say " + command
		}
		if !strings.HasSuffix(command, "\n") {
			command += "\n"
		}
		m.logger.Info("executing command " + command)
		writeWaiter := make(chan struct{})
		var err error
		go func() {
			_, err = m.commandsPipe.Write([]byte(command))
			close(writeWaiter)
		}()
		select {
		case <-writeWaiter:
		case <-m.ctx.Done():
			return m.ctx.Err()
		}
		if err != nil {
			return errors.Wrap(err, "cannot write to pipe")
		}
	}
	return nil
}
