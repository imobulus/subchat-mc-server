package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/imobulus/subchat-mc-server/src/mcserver"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

func processInput(s *mcserver.Server, logger *zap.Logger, ctx context.Context) {
	inbuf := bufio.NewReader(os.Stdin)
	for {
		var command string
		var err error
		waitRead := make(chan struct{})
		go func() {
			command, err = inbuf.ReadString('\n')
			close(waitRead)
		}()
		select {
		case <-ctx.Done():
			return
		case <-waitRead:
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			logger.Error("cannot read input", zap.Error(err))
			return
		}
		command = command[:len(command)-1]
		err = s.GetMcProcess().Exec(command)
		if err != nil {
			logger.Error("cannot execute command", zap.Error(err))
		}
	}
}

func runserver(config mcserver.Config, logger *zap.Logger) error {
	logger.Info("starting server")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := mcserver.NewServer(config, logger)
	if err != nil {
		return errors.Wrapf(err, "cannot create server")
	}
	err = server.Start(ctx)
	if err != nil {
		return errors.Wrapf(err, "cannot start server")
	}
	go processInput(server, logger, ctx)
	<-server.Done()
	return nil
}

func main() {
	confpath := flag.String("config", "config/server-config.yaml", "psth to server config file")
	flag.Parse()
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("cannot create logger: %s", err.Error())
	}
	configBytes, err := os.ReadFile(*confpath)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot read file %s: %s", *confpath, err.Error()))
		os.Exit(1)
	}
	config := mcserver.DefaultConfig
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot unmarshal config %s: %s", *confpath, err.Error()))
		os.Exit(1)
	}
	err = runserver(config, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot run server: %s", err.Error()))
		os.Exit(1)
	}
}
