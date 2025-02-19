package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/permsengine"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Config struct {
	Debug           bool                                `yaml:"debug"`
	TgBot           tgbot.TgBotConfig                   `yaml:"tg bot"`
	TgBotSecretPath string                              `yaml:"tg bot secret path"`
	Perms           permsengine.ServerPermsEngineConfig `yaml:"perms"`
	SqliteLocation  string                              `yaml:"sqlite location"`
}

var DefaultConfig = Config{
	TgBot:           tgbot.DefaultTgBotConfig,
	TgBotSecretPath: "/run/secrets/tg-bot.json",
	Perms:           permsengine.DefaultServerPermsEngineConfig,
	SqliteLocation:  "/sqlite/auth.db",
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalChan
		cancel()
	}()

	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	contents, err := os.ReadFile(*configPath)
	if err != nil {
		logger.Fatal("failed to read config file", zap.Error(err))
	}

	config := DefaultConfig
	err = yaml.Unmarshal(contents, &config)
	if err != nil {
		logger.Fatal("failed to parse config file", zap.Error(err))
	}

	var tgSecret tgbot.TgBotSecret
	secretContents, err := os.ReadFile(config.TgBotSecretPath)
	if err != nil {
		logger.Fatal("failed to read tg secret file", zap.Error(err))
	}
	err = yaml.Unmarshal(secretContents, &tgSecret)
	if err != nil {
		logger.Fatal("failed to parse tg secret file", zap.Error(err))
	}

	db, err := gorm.Open(sqlite.Open(config.SqliteLocation), &gorm.Config{})
	if err != nil {
		logger.Fatal("Failed to open db", zap.Error(err))
	}

	permsEngine := permsengine.NewServerPermsEngine(config.Perms, authdb.NewAuthDbExecutor(db))
	tgBot, err := tgbot.NewTgBot(config.TgBot, tgSecret, permsEngine, logger, ctx)
	if err != nil {
		logger.Fatal("Failed to create tg bot", zap.Error(err))
	}
	tgBot.Run()

	<-ctx.Done()
	time.Sleep(2 * time.Second)
}
