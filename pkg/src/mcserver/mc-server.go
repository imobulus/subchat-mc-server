package mcserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/imobulus/subchat-mc-server/src/mcprocess"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type PropertiesOverrides map[string]string

type Config struct {
	PropertiesPath    string                    `yaml:"properties path"`
	ServerProperties  PropertiesOverrides       `yaml:"server properties"`
	CommandsPort      int                       `yaml:"commands port"`
	JavaProcessConfig mcprocess.McProcessConfig `yaml:"java process config"`
}

var DefaultConfig = Config{
	PropertiesPath:    "server.properties",
	CommandsPort:      8080,
	JavaProcessConfig: mcprocess.DefaultMcProcessConfig,
}

type Server struct {
	config      Config
	javaProcess *mcprocess.McProcessHolder
	wg          *sync.WaitGroup
	doneC       chan struct{}
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
}

func NewServer(config Config, logger *zap.Logger) *Server {
	return &Server{
		config:      config,
		javaProcess: mcprocess.NewMcProcessHolder(config.JavaProcessConfig, logger),
		wg:          &sync.WaitGroup{},
		doneC:       make(chan struct{}),
		logger:      logger,
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	success := false
	defer func() {
		if !success {
			s.cancel()
		}
	}()
	err := s.configure()
	if err != nil {
		return errors.Wrap(err, "cannot configure server")
	}
	err = s.javaProcess.Start(s.ctx)
	if err != nil {
		return errors.Wrap(err, "cannot start java process")
	}
	s.watchJava()
	s.runServer()
	go s.watchWg()
	success = true
	return nil
}

func (s *Server) watchWg() {
	s.wg.Wait()
	s.cancel()
	close(s.doneC)
}

func (s *Server) watchJava() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-s.javaProcess.Done()
		s.cancel()
	}()
}

func (s *Server) Done() <-chan struct{} {
	return s.doneC
}

func (s *Server) GetMcProcess() *mcprocess.McProcessHolder {
	return s.javaProcess
}

func (s *Server) configure() error {
	err := s.updateProperties()
	return err
}

func (s *Server) updateProperties() error {
	overridesCopy := make(map[string]string, len(s.config.ServerProperties))
	for k, v := range s.config.ServerProperties {
		overridesCopy[k] = v
	}
	contentsBytes, err := os.ReadFile(s.config.PropertiesPath)
	if err != nil {
		return errors.Wrapf(err, "cannot read file %s", s.config.PropertiesPath)
	}
	contentLines := strings.Split(string(contentsBytes), "\n")
	for i := range contentLines {
		allParts := strings.Split(contentLines[i], "=")
		if len(allParts) != 2 {
			continue
		}
		newValue, ok := overridesCopy[allParts[0]]
		if !ok {
			continue
		}
		contentLines[i] = allParts[0] + "=" + newValue
		delete(overridesCopy, allParts[0])
	}
	for k, v := range overridesCopy {
		contentLines = append(contentLines, k+"="+v)
	}
	content := strings.Join(contentLines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	err = os.WriteFile(s.config.PropertiesPath, []byte(content), 0664)
	if err != nil {
		return errors.Wrapf(err, "cannot write file %s", s.config.PropertiesPath)
	}
	return nil
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	commandBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("cannot read command", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.logger.Info("executing command " + string(commandBytes))
	err = s.javaProcess.Exec(string(commandBytes))
	if err != nil {
		s.logger.Error("cannot exec command "+string(commandBytes), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "/command" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleCommand(w, r)
		return
	}
	if r.RequestURI == "/shutdown" {
		s.cancel()
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *Server) runServer() {
	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.CommandsPort),
		Handler: http.HandlerFunc(s.handle),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// unexpected error. port in use?
			s.logger.Error("failed to listen", zap.Error(err))
		} else {
			s.logger.Info("server closed")
		}
	}()
	go func() {
		<-s.ctx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Minute)
		defer cancelShutdown()
		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			s.logger.Error("cannot shutdown server", zap.Error(err))
		}
	}()
}
