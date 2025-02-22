package tgbot

import (
	"errors"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
)

type PrivateChatHandler struct {
	bot *TgBot
}

func NewPrivateChatHandler(bot *TgBot) *PrivateChatHandler {
	return &PrivateChatHandler{bot: bot}
}

func (handler *PrivateChatHandler) InitialHandle(update *tgbotapi.Update) error {
	return nil
}

func (handler *PrivateChatHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	command := update.Message.Command()
	switch command {
	case "/add_minecraft_login":
		return &AddMinecraftLoginHandler{bot: handler.bot}, nil
	default:
		return nil, ErrUnknownCommand{Update: update, Command: command}
	}
}

func (handler *PrivateChatHandler) GetCommands() []tgtypes.BotCommand {
	return []tgtypes.BotCommand{
		{Command: "add_minecraft_login", Description: "Зарегистрировать ник на сервере"},
	}
}
func (handler *PrivateChatHandler) GetHelpDescription() string {
	return "Главное Меню"
}
func (handler *PrivateChatHandler) GetBot() *TgBot {
	return handler.bot
}

type AddMinecraftLoginHandler struct {
	bot         *TgBot
	initialized bool
}

func (handler *AddMinecraftLoginHandler) InitialHandle(update *tgbotapi.Update) error {
	if !handler.initialized {
		handler.initialized = true
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Введите ник")
		// msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
		handler.bot.SendLog(msg)
	}
	return nil
}

func (handler *AddMinecraftLoginHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	login, err := authdb.MakeMinecraftLogin(update.Message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Ник содержит недопустимые символы или слишком короткий или длинный, введите другой")
		handler.bot.SendLog(msg)
		return handler, nil
	}
	err = handler.bot.permsEngine.AddMinecraftLogin(actor.ID, login)
	if err != nil {
		if errors.Is(err, authdb.ErrorLoginTaken{}) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Ник уже занят, введите другой")
			handler.bot.SendLog(msg)
			return handler, nil
		}
		return nil, err
	}
	return nil, nil
}

func (handler *AddMinecraftLoginHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *AddMinecraftLoginHandler) GetHelpDescription() string {
	return "Сейчас вы регистрируете ник на сервере"
}
func (handler *AddMinecraftLoginHandler) GetBot() *TgBot {
	return handler.bot
}
