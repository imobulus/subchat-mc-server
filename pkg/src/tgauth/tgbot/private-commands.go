package tgbot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
)

type AbortHandleWrapper struct {
	Handler InteractiveHandler
}

func (handler *AbortHandleWrapper) InitialHandle(update *tgbotapi.Update) {
	handler.Handler.InitialHandle(update)
}

func (handler *AbortHandleWrapper) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) InteractiveHandler {
	if update.Message.Command() == "/abort" {
		return nil
	}
	return handler.Handler.HandleUpdate(update, actor)
}

type PrivateChatHandler struct {
	bot *TgBot
}

func NewPrivateChatHandler(bot *TgBot) *PrivateChatHandler {
	return &PrivateChatHandler{bot: bot}
}

func (handler *PrivateChatHandler) InitialHandle(update *tgbotapi.Update) {
}

func (handler *PrivateChatHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) InteractiveHandler {
	command := update.Message.Command()
	switch command {
	case "/add_minecraft_login":
		return &AddMinecraftLoginHandler{bot: handler.bot}
	default:
		handler.bot.SendLog(tgbotapi.NewMessage(update.Message.Chat.ID, "Unimplemented Help"))
	}
	return handler
}

type AddMinecraftLoginHandler struct {
	bot            *TgBot
	initialized    bool
	MinecraftLogin authdb.MinecraftLogin
}

func (handler *AddMinecraftLoginHandler) InitialHandle(update *tgbotapi.Update) {
	if !handler.initialized {
		handler.initialized = true
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Please enter your Minecraft login")
		// msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
		handler.bot.SendLog(msg)
	}
}

func (handler *AddMinecraftLoginHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) InteractiveHandler {
	login, err := authdb.MakeMinecraftLogin(update.Message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, err.Error())
		handler.bot.SendLog(msg)
	}
	handler.MinecraftLogin = login
	handler.bot.permsEngine.AddMinecraftLogin(actor.ID, handler.MinecraftLogin)
	return nil
}
