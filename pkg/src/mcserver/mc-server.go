package mcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/imobulus/subchat-mc-server/src/mcprocess"
	"github.com/imobulus/subchat-mc-server/src/mojang"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type PropertiesOverrides map[string]string

type Config struct {
	PropertiesPath         string                    `yaml:"properties path"`
	ServerProperties       PropertiesOverrides       `yaml:"server properties"`
	CommandsPort           int                       `yaml:"commands port"`
	AuthDbPath             string                    `yaml:"auth db path"`
	UserCachePath          string                    `yaml:"user cache path"`
	WhitelistPath          string                    `yaml:"whitelist path"`
	JavaProcessConfig      mcprocess.McProcessConfig `yaml:"java process config"`
	CheckAccountsFrequency time.Duration             `yaml:"check accounts frequency"`
}

var DefaultConfig = Config{
	PropertiesPath:         "server.properties",
	CommandsPort:           8080,
	AuthDbPath:             "mods/EasyAuth/levelDBStore",
	UserCachePath:          "usercache.json",
	WhitelistPath:          "whitelist.json",
	JavaProcessConfig:      mcprocess.DefaultMcProcessConfig,
	CheckAccountsFrequency: 2 * time.Second,
}

type Server struct {
	config         Config
	javaProcess    *mcprocess.McProcessHolder
	accountManager *AccountManager
	wg             *sync.WaitGroup
	doneC          chan struct{}
	logger         *zap.Logger
	ctx            context.Context
	cancel         context.CancelFunc
}

func NewServer(config Config, logger *zap.Logger) (*Server, error) {
	javaProcess := mcprocess.NewMcProcessHolder(config.JavaProcessConfig, logger)
	accountManager := NewAccountManager(
		config.WhitelistPath,
		config.CheckAccountsFrequency,
		javaProcess.Exec,
		logger,
	)
	s := &Server{
		config:         config,
		javaProcess:    javaProcess,
		accountManager: accountManager,
		wg:             &sync.WaitGroup{},
		doneC:          make(chan struct{}),
		logger:         logger,
	}
	return s, nil
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
	s.accountManager.runAccountManager(ctx)
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
	err = s.javaProcess.Exec(string(commandBytes))
	if err != nil {
		s.logger.Error("cannot exec command "+string(commandBytes), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type MinecraftAccountSpec struct {
	Name     mojang.MinecraftLogin `json:"name"`
	PlayerId string                `json:"player_id"`
}

func (s *Server) handleSetWhitelist(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("cannot read body", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var accounts []MinecraftAccountSpec
	err = json.Unmarshal(bodyBytes, &accounts)
	if err != nil {
		s.logger.Error("cannot unmarshal accounts", zap.Error(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	err = s.accountManager.SetNeededAccounts(accounts)
	if err != nil {
		s.logger.Error("cannot set needed accounts", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSetPasswords(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("cannot read body", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var accountPasswords map[mojang.MinecraftLogin]string
	err = json.Unmarshal(bodyBytes, &accountPasswords)
	if err != nil {
		s.logger.Error("cannot unmarshal account passwords", zap.Error(err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	err = s.accountManager.SetAccountPasswords(accountPasswords)
	if err != nil {
		s.logger.Error("cannot set passwords", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	switch r.RequestURI {
	case "/command":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleCommand(w, r)
	case "/set-whitelist":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleSetWhitelist(w, r)
	case "/set-passwords":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleSetPasswords(w, r)
	case "/offline-uuid":
		defer r.Body.Close()
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			s.logger.Error("cannot read body", zap.Error(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		playerUuid := mojang.GetOfflineUuid(mojang.MinecraftLogin(bodyBytes))
		w.Write([]byte(playerUuid.String()))
	case "/shutdown":
		s.cancel()
		w.WriteHeader(http.StatusOK)
	default:
		if strings.HasPrefix(r.RequestURI, "/mods/") {
			http.StripPrefix("/mods", http.FileServer(http.Dir("clientmods"))).ServeHTTP(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
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
