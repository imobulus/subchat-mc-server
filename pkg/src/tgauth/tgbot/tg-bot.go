package tgbot

import (
	"context"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/permsengine"
	"go.uber.org/zap"
)

type InteractiveHandler interface {
	InitialHandle(update *tgbotapi.Update)
	HandleUpdate(update *tgbotapi.Update) InteractiveHandler
}

type ChatHandler struct {
	handlerMx      *sync.Mutex
	currentHandler InteractiveHandler
	lastUpdateTime time.Time
}

type TgBotConfig struct {
	Debug bool `yaml:"debug"`
}

type TgBotSecret struct {
	Token string `json:"token"`
}

type TgBot struct {
	api *tgbotapi.BotAPI

	chatHandlersMap map[int64]*ChatHandler
	permsEngine     *permsengine.ServerPermsEngine

	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTgBot(
	config TgBotConfig, secret TgBotSecret,
	logger *zap.Logger, ctx context.Context,
) (*TgBot, error) {
	api, err := tgbotapi.NewBotAPI(secret.Token)
	if err != nil {
		return nil, err
	}
	log.Printf("Authorized bot %s", api.Self.UserName)
	api.Debug = config.Debug
	ctx, cancel := context.WithCancel(ctx)
	tgBot := TgBot{
		api:             api,
		chatHandlersMap: make(map[int64]*ChatHandler),
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}
	return &tgBot, nil
}

func (bot *TgBot) Run() error {
	return bot.runUpdatesLoop()
}

func (bot *TgBot) runUpdatesLoop() error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.api.GetUpdatesChan(u)
	if err != nil {
		return err
	}

	time.Sleep(time.Millisecond * 500)
	updates.Clear()
	go func() {
		for {
			select {
			case <-bot.ctx.Done():
				return
			case update := <-updates:
				bot.handleUpdate(update)
			}
		}
	}()
	return nil
}

func (bot *TgBot) handleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		// bot.logger.Error("update.Message is nil")
		return
	}
	message := update.Message
	if message.Chat == nil {
		bot.logger.Error("update.Message.Chat is nil")
		return
	}
	chat := message.Chat
	chatHandler, ok := bot.chatHandlersMap[chat.ID]
	if !ok {
		chatHandler = bot.createChatHandler(chat)
		if chatHandler == nil {
			bot.logger.Error("chatHandler is nil")
			return
		}
		bot.chatHandlersMap[chat.ID] = chatHandler
	}
	chatHandler.handlerMx.Lock()
	go func() {
		defer chatHandler.handlerMx.Unlock()
		bot.handleChatMessageUpdate(chatHandler, &update)
	}()
}

func (bot *TgBot) createChatHandler(chat *tgbotapi.Chat) *ChatHandler {
	if chat.Type == "private" {
		return &ChatHandler{
			handlerMx: &sync.Mutex{},
		}
	}
	bot.logger.Error("chat.Type is not private, NOT IMPLEMENTED")
	return nil
}

func (bot *TgBot) handleChatMessageUpdate(chatHandler *ChatHandler, update *tgbotapi.Update) {
	chatHandler.lastUpdateTime = time.Now()
	if update.Message.From != nil {
		bot.permsEngine.UpdateTgUserInfo(*update.Message.From)
	}
	if chatHandler.currentHandler == nil {
		chatHandler.currentHandler = &DefaultHandler{bot: bot}
	}
	chatHandler.currentHandler = chatHandler.currentHandler.HandleUpdate(update)
}

func (bot *TgBot) SendLog(conf tgbotapi.MessageConfig) {
	_, err := bot.api.Send(conf)
	if err != nil {
		bot.logger.Error("Failed to send message", zap.Error(err))
	}
}
