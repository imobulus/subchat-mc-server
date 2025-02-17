package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type PropertiesOverrides map[string]string

type Config struct {
	ServerProperties   PropertiesOverrides `yaml:"server properties"`
	MaxMemoryGigabytes int                 `yaml:"max memory gigabytes"`
	CommandsPort       int                 `yaml:"commands port"`
}

var DefaultConfig = Config{
	MaxMemoryGigabytes: 4,
	CommandsPort:       8080,
}

func updateProperties(propertiesPath string, overrides PropertiesOverrides) error {
	overridesCopy := make(map[string]string, len(overrides))
	for k, v := range overrides {
		overridesCopy[k] = v
	}
	contentsBytes, err := os.ReadFile(propertiesPath)
	if err != nil {
		return errors.Wrapf(err, "cannot read file %s", propertiesPath)
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
	err = os.WriteFile(propertiesPath, []byte(content), 0664)
	if err != nil {
		return errors.Wrapf(err, "cannot write file %s", propertiesPath)
	}
	return nil
}

func configure(cfg Config, propertiesPath string) error {
	err := updateProperties(propertiesPath, cfg.ServerProperties)
	return err
}

type serverHandler struct {
	commandsWriter *io.PipeWriter
	ctx            context.Context
	cancelFunc     context.CancelFunc
}

func constructCommand(commandBytes []byte) []byte {
	commandString := string(commandBytes)
	if !strings.HasSuffix(commandString, "\n") {
		commandString += "\n"
	}
	if strings.HasPrefix(commandString, "/") {
		return []byte(commandString)
	}
	return []byte("/say " + commandString)
}

func (handler *serverHandler) handleCommand(w http.ResponseWriter, r *http.Request) {
	commandBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("cannot read command: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	command := constructCommand(commandBytes)
	_, err = handler.commandsWriter.Write(command)
	if err != nil {
		log.Printf("cannot write command: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Println("Executing Command " + string(command))
	w.WriteHeader(http.StatusOK)
}

func (handler *serverHandler) handle(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "/command" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		handler.handleCommand(w, r)
		return
	}
	if r.RequestURI == "/shutdown" {
		w.WriteHeader(http.StatusOK)
		handler.cancelFunc()
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func serveCommands(cfg Config, commandsWriter *io.PipeWriter, ctx context.Context, cancel context.CancelFunc) {
	handler := &serverHandler{
		commandsWriter: commandsWriter,
		ctx:            ctx,
		cancelFunc:     cancel,
	}
	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.CommandsPort),
		Handler: http.HandlerFunc(handler.handle),
	}
	go func() {
		defer cancel()
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// unexpected error. port in use?
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Minute)
	defer cancelShutdown()
	err := srv.Shutdown(shutdownCtx)
	if err != nil {
		log.Fatalf("cannot shutdown server: %v", err)
	}
}

func interruptAndKill(cmd *exec.Cmd, timeout time.Duration) {
	endedChan := make(chan struct{})
	go func() {
		cmd.Wait()
		close(endedChan)
	}()
	cmd.Process.Signal(os.Interrupt)
	select {
	case <-endedChan:
		return
	case <-time.After(timeout):
	}
	cmd.Process.Kill()
}

func startProcessInput(commandsPipeIn *io.PipeWriter, ctx context.Context) {
	go func() {
		inbuf := bufio.NewReader(os.Stdin)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			command, err := inbuf.ReadString('\n')
			if err == io.EOF {
				fmt.Println("EOF")
				return
			}
			if err != nil {
				log.Fatalf("cannot read command: %v", err)
			}
			_, err = commandsPipeIn.Write(constructCommand([]byte(command)))
			if err != nil {
				log.Fatalf("cannot write to pipe: %v", err)
			}
		}
	}()
}

func runserver(cfg Config) {
	cmdArgs := []string{
		"java", fmt.Sprintf("-Xmx%dG", cfg.MaxMemoryGigabytes),
		"-jar", "fabric.jar", "--nogui",
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	commandsPipeOut, commandsPipeIn := io.Pipe()
	cmd.Stdin = commandsPipeOut

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		serveCommands(cfg, commandsPipeIn, ctx, cancel)
	}()
	startProcessInput(commandsPipeIn, ctx)
	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		err := cmd.Start()
		if err != nil {
			log.Fatalf("cannot start command: %v", err)
		}
		go func() {
			<-ctx.Done()
			interruptAndKill(cmd, time.Minute)
		}()
		err = cmd.Wait()
		if err != nil {
			log.Fatalf("command finished with error: %v", err)
		}
	}()
	wg.Wait()
}

func main() {
	confpath := flag.String("config", "config/server-config.yaml", "psth to server config file")
	propertiesPath := flag.String("properties", "server.properties", "psth to server properties file")
	norun := flag.Bool("norun", false, "do not run server")
	flag.Parse()
	configBytes, err := os.ReadFile(*confpath)
	if err != nil {
		log.Fatalf("cannot read file %s: %s", *confpath, err.Error())
	}
	var cfg Config
	err = yaml.Unmarshal(configBytes, &cfg)
	if err != nil {
		log.Fatalf("cannot unmarshal config %s: %s", *confpath, err.Error())
	}
	err = configure(cfg, *propertiesPath)
	if err != nil {
		log.Fatalf("cannot configure: %s", err.Error())
	}
	if *norun {
		return
	}
	runserver(cfg)
}
