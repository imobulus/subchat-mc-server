package tgbot

import (
	"errors"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/permsengine"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
)

type PrivateChatHandler struct {
	bot *TgBot
}

func NewPrivateChatHandler(bot *TgBot) *PrivateChatHandler {
	return &PrivateChatHandler{bot: bot}
}

func (handler *PrivateChatHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	return handler, nil
}

func (handler *PrivateChatHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	command := update.Message.Command()
	switch command {
	case "add_minecraft_login":
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

func (handler *AddMinecraftLoginHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if !handler.initialized {
		handler.initialized = true
		loginStrings := make([]string, 0, len(actor.MinecraftAccounts))
		for _, acc := range actor.MinecraftAccounts {
			loginStrings = append(loginStrings, string(acc.ID))
		}
		limitReached := false
		replyBuilder := strings.Builder{}
		if len(loginStrings) > 0 {
			replyBuilder.WriteString("Ваши аккаунты:\n")
			for _, login := range loginStrings {
				replyBuilder.WriteString(login)
				replyBuilder.WriteRune('\n')
			}
			replyBuilder.WriteRune('\n')
		} else {
			replyBuilder.WriteString("У вас нет зарегистрированных аккаунтов\n\n")
		}
		err := handler.bot.permsEngine.CheckAddMinecraftLoginPermission(actor.ID)
		if err != nil {
			if errors.Is(err, permsengine.ErrorExceededMaxMinecraftLogins{}) {
				limitReached = true
				replyBuilder.WriteString("Превышен лимит зарегистрированных аккаунтов, возврат в главное меню")
			} else {
				return handler, err
			}
		} else {
			replyBuilder.WriteString("Введите аккаунт")
		}
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, replyBuilder.String())
		// msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
		handler.bot.SendLog(msg)
		if limitReached {
			return nil, nil
		}
	}
	return handler, nil
}

func (handler *AddMinecraftLoginHandler) handleLoginExists(update *tgbotapi.Update, login authdb.MinecraftLogin) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Аккаунт %s уже занят, введите другой", login))
	handler.bot.SendLog(msg)
}

func (handler *AddMinecraftLoginHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	login, err := authdb.MakeMinecraftLogin(update.Message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт содержит недопустимые символы или слишком короткий или длинный, введите другой")
		handler.bot.SendLog(msg)
		return handler, nil
	}
	maybeAccount, err := handler.bot.permsEngine.OptionalGetMinecraftAccount(login)
	if err != nil {
		return nil, err
	}
	if maybeAccount != nil {
		if maybeAccount.ActorID == actor.ID {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Вы уже зарегистрировали этот аккаунт")
			handler.bot.SendLog(msg)
			return nil, nil
		}
		handler.handleLoginExists(update, login)
		return handler, nil
	}
	err = handler.bot.permsEngine.AddMinecraftLogin(actor.ID, login)
	if err != nil {
		if errors.Is(err, authdb.ErrorLoginTaken{}) {
			handler.handleLoginExists(update, login)
			return handler, nil
		}
		if errors.Is(err, permsengine.ErrorExceededMaxMinecraftLogins{}) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Превышен лимит зарегистрированных аккаунтов")
			handler.bot.SendLog(msg)
			return nil, nil
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
