package tgbot

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/permsengine"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
	"go.uber.org/zap"
)

type InteractiveHandler interface {
	InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error)
	HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error)
	GetCommands() []tgtypes.BotCommand
	GetHelpDescription() string
	GetBot() *TgBot
}

type InteractiveSessionId struct {
	ChatId int64
	UserId int64
}

type ChatHandler struct {
	id             InteractiveSessionId
	isPrivate      bool
	lastActor      *authdb.Actor
	handlerMx      *sync.Mutex
	currentHandler InteractiveHandler
	lastUpdateTime time.Time
}

func (handler *ChatHandler) GetScope() *tgtypes.BotCommandScope {
	if handler.isPrivate {
		scopeEntry := tgtypes.NewBotCommandScopeChat(handler.id.ChatId)
		return &tgtypes.BotCommandScope{
			Chat: scopeEntry,
		}
	}
	scopeEntry := tgtypes.NewBotCommandScopeChatMember(handler.id.ChatId, handler.id.UserId)
	return &tgtypes.BotCommandScope{
		ChatMember: scopeEntry,
	}
}

type TgBotConfig struct {
	Debug                 bool          `yaml:"debug"`
	SetWhitelistFrequency time.Duration `yaml:"set whitelist frequency"`
}

var DefaultTgBotConfig = TgBotConfig{
	Debug:                 false,
	SetWhitelistFrequency: time.Second,
}

type TgBotSecret struct {
	Token          string `json:"token"`
	AccessPassword string `json:"access_password"`
}

type TgBot struct {
	api    *tgbotapi.BotAPI
	aux    *tgtypes.AuxTgApi
	secret TgBotSecret

	chatHandlersMap map[InteractiveSessionId]*ChatHandler
	chatHandlersMx  *sync.Mutex
	permsEngine     *permsengine.ServerPermsEngine

	doneC chan struct{}
	wg    *sync.WaitGroup

	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTgBot(
	config TgBotConfig, secret TgBotSecret,
	permsEngine *permsengine.ServerPermsEngine,
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
		aux:             tgtypes.NewAuxTgApi(api, logger),
		secret:          secret,
		chatHandlersMap: make(map[InteractiveSessionId]*ChatHandler),
		chatHandlersMx:  &sync.Mutex{},
		permsEngine:     permsEngine,
		doneC:           make(chan struct{}),
		wg:              &sync.WaitGroup{},
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
	}
	return &tgBot, nil
}

func (bot *TgBot) Run() error {
	err := bot.initCommands()
	if err != nil {
		return err
	}
	bot.runWhitelistSetter()
	bot.runUpdatesLoop()
	return nil
}

func (bot *TgBot) Done() <-chan struct{} {
	return bot.doneC
}

func (bot *TgBot) initCommands() error {
	bot.aux.DeleteMyCommands(nil, nil)
	privateHandler := MakePrivateChatHandler(bot, nil)
	privateCommands := privateHandler.GetCommands()
	err := bot.aux.SetMyCommands(privateCommands, &tgtypes.BotCommandScope{
		AllPrivateChats: tgtypes.NewBotCommandScopeAllPrivateChats(),
	}, nil)
	if err != nil {
		return err
	}
	return nil
}

func (bot *TgBot) runWhitelistSetter() {
	go func() {
		ticker := time.NewTicker(DefaultTgBotConfig.SetWhitelistFrequency)
		defer ticker.Stop()
		for {
			select {
			case <-bot.ctx.Done():
				return
			case <-ticker.C:
				bot.setWhitelist()
			}
		}
	}()
}

func (bot *TgBot) setWhitelist() {
	err := bot.permsEngine.UpdateWhitelist()
	if err != nil {
		bot.logger.Error("Failed to update whitelist", zap.Error(err))
	}
}

func (bot *TgBot) runUpdatesLoop() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.api.GetUpdatesChan(u)

	updatesProxy := make(chan tgbotapi.Update)
	go func() {
		defer close(updatesProxy)
		for {
			select {
			case <-bot.ctx.Done():
				return
			case update := <-updates:
				updatesProxy <- update
			}
		}
	}()

	time.Sleep(time.Millisecond * 500)
	updates.Clear()
	bot.wg.Add(1)
	go func() {
		defer bot.wg.Done()
		for update := range updatesProxy {
			bot.logger.Debug("Received update", zap.Any("update", update))
			bot.handleUpdate(update)
		}
	}()
	go func() {
		bot.wg.Wait()
		close(bot.doneC)
	}()
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
	if message.From == nil {
		return
	}
	from := message.From
	sessionId := InteractiveSessionId{
		ChatId: chat.ID,
		UserId: int64(from.ID),
	}
	bot.chatHandlersMx.Lock()
	chatHandler, ok := bot.chatHandlersMap[sessionId]
	if !ok {
		chatHandler = bot.createChatHandler(chat, sessionId)
		if chatHandler == nil {
			bot.logger.Error("chatHandler is nil")
			bot.chatHandlersMx.Unlock()
			return
		}
		bot.chatHandlersMap[sessionId] = chatHandler
	}
	chatHandler.handlerMx.Lock()
	bot.chatHandlersMx.Unlock()
	bot.wg.Add(1)
	go func() {
		defer bot.wg.Done()
		bot.handleChatMessageUpdate(chatHandler, &update)
		bot.handleNewInteractiveCommands(chatHandler, &update)
		chatHandler.handlerMx.Unlock()
		bot.handleCleanup(chatHandler)
	}()
}

func (bot *TgBot) handleCleanup(chatHandler *ChatHandler) {
	bot.chatHandlersMx.Lock()
	defer bot.chatHandlersMx.Unlock()
	// we locked the map. If this lock succeeds it means that handler is not in use and we can check if it has nil handler
	isFree := chatHandler.handlerMx.TryLock()
	if isFree {
		defer chatHandler.handlerMx.Unlock()
		if chatHandler.currentHandler == nil {
			delete(bot.chatHandlersMap, chatHandler.id)
		}
	}
}

func (bot *TgBot) createChatHandler(chat *tgbotapi.Chat, id InteractiveSessionId) *ChatHandler {
	if chat.Type == "private" {
		return &ChatHandler{
			id:        id,
			handlerMx: &sync.Mutex{},
			isPrivate: true,
		}
	}
	return &ChatHandler{
		id:        id,
		handlerMx: &sync.Mutex{},
		isPrivate: false,
	}
}

func (bot *TgBot) HandleUpdateError(update *tgbotapi.Update, err error) {
	bot.logger.Error(fmt.Sprintf("Failed to handle update id %d", update.UpdateID), zap.Error(err))
	if update.Message != nil && update.Message.Chat != nil && update.Message.Chat.Type == tgtypes.PrivateChatType {
		bot.SendLog(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf(
			"Что-то пошло не так, отправьте ID %d администратору", update.UpdateID,
		)))
	}
}

func MakePrivateChatHandler(bot *TgBot, initialActor *authdb.Actor) InteractiveHandler {
	return NewCommonHandleWrapper(NewPrivateChatHandler(bot, initialActor))
}

func MakePublicChatHandler(bot *TgBot) InteractiveHandler {
	return NewPublicChatHandler(bot)
}

func (bot *TgBot) getFirstHandler(actor *authdb.Actor, update *tgbotapi.Update) InteractiveHandler {
	switch update.Message.Chat.Type {
	case tgtypes.PrivateChatType:
		return MakePrivateChatHandler(bot, actor)
	case tgtypes.GroupChatType, tgtypes.SupergroupChatType:
		return MakePublicChatHandler(bot)
	default:
		bot.logger.Debug("chat type is not implemented", zap.String("chatType", update.Message.Chat.Type))
		return nil
	}
}

func (bot *TgBot) handleChatMessageUpdate(chatHandler *ChatHandler, update *tgbotapi.Update) {
	chatHandler.lastUpdateTime = time.Now()
	if update.Message.From == nil {
		// don't handle fromless updates yet
		bot.logger.Debug("update.Message.From is nil")
		return
	}
	actor := &authdb.Actor{
		SeenInChats:       []authdb.TgChat{},
		VerifiedByAdmins:  []*authdb.Actor{},
		Bans:              []authdb.Ban{},
		TgAccounts:        []authdb.TgUser{},
		MinecraftAccounts: []authdb.MinecraftAccount{},
	}
	err := bot.permsEngine.UpdateTgUserInfo(*update.Message.From)
	if err != nil {
		bot.HandleUpdateError(update, err)
		return
	}
	err = bot.permsEngine.GetActorByTgUser(authdb.TgUserId(update.Message.From.ID), actor)
	if err != nil {
		bot.HandleUpdateError(update, err)
		return
	}
	chatHandler.lastActor = actor
	if update.Message.Chat.Type == tgtypes.GroupChatType || update.Message.Chat.Type == tgtypes.SupergroupChatType {
		err := bot.permsEngine.SeenInChat(actor.ID, authdb.TgChatId(update.Message.Chat.ID))
		if err != nil {
			bot.HandleUpdateError(update, err)
			return
		}
	}
	err = bot.permsEngine.UpdateActorStatus(actor.ID, false)
	if err != nil {
		bot.HandleUpdateError(update, err)
	}
	defer func() {
		err := bot.permsEngine.UpdateActorStatus(actor.ID, false)
		if err != nil {
			bot.HandleUpdateError(update, err)
		}
	}()
	if chatHandler.currentHandler == nil {
		handler := bot.getFirstHandler(actor, update)
		if handler == nil {
			return
		}
		chatHandler.currentHandler = handler
	}
	newHandler, err := chatHandler.currentHandler.HandleUpdate(update, actor)
	if err != nil {
		bot.HandleUnexpectedError(update, err)
		return
	}
	chatHandler.currentHandler = newHandler
	if newHandler == nil {
		return
	}
	newHandler, err = newHandler.InitialHandle(update, actor)
	if err != nil {
		bot.HandleUnexpectedError(update, err)
		return
	}
	chatHandler.currentHandler = newHandler
	if newHandler == nil {
		return
	}
}

func (bot *TgBot) handleNewInteractiveCommands(chatHandler *ChatHandler, update *tgbotapi.Update) {
	if chatHandler.currentHandler == nil {
		handler := bot.getFirstHandler(chatHandler.lastActor, update)
		if handler == nil {
			err := bot.aux.DeleteMyCommands(chatHandler.GetScope(), nil)
			if err != nil {
				bot.logger.Error("Failed to delete commands", zap.Error(err))
			}
			return
		}
		err := bot.aux.SetMyCommands(handler.GetCommands(), chatHandler.GetScope(), nil)
		if err != nil {
			bot.logger.Error("Failed to set commands", zap.Error(err))
		}
		return
	}
	commands := chatHandler.currentHandler.GetCommands()
	err := bot.aux.SetMyCommands(commands, chatHandler.GetScope(), nil)
	if err != nil {
		bot.logger.Error("Failed to set commands", zap.Error(err))
	}
}

func (bot *TgBot) SendLog(conf tgbotapi.MessageConfig) {
	conf.ParseMode = tgbotapi.ModeHTML
	_, err := bot.api.Send(conf)
	if err != nil {
		bot.logger.Error("Failed to send message", zap.Error(err))
	}
}

func (bot *TgBot) HandleUnexpectedError(update *tgbotapi.Update, err error) {
	bot.logger.Error("Unexpected error", zap.Error(err), zap.Any("update_id", update.UpdateID))
	if update.Message != nil && update.Message.Chat != nil && update.Message.Chat.Type == tgtypes.PrivateChatType {
		bot.SendLog(tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf(
			"Что-то пошло не так, отправьте код %d администратору", update.UpdateID,
		)))
	}
}
