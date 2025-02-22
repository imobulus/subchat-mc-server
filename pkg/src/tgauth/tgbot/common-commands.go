package tgbot

import (
	"errors"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
)

type ErrUnknownCommand struct {
	Update  *tgbotapi.Update
	Command string
}

func (err ErrUnknownCommand) Error() string {
	return "Unknown command " + err.Command
}

func (err ErrUnknownCommand) Is(target error) bool {
	_, ok := target.(ErrUnknownCommand)
	return ok
}

type CommonHandleWrapper struct {
	Handler InteractiveHandler
}

func NewCommonHandleWrapper(handler InteractiveHandler) *CommonHandleWrapper {
	return &CommonHandleWrapper{Handler: handler}
}

func (handler *CommonHandleWrapper) GetBot() *TgBot {
	return handler.Handler.GetBot()
}

func (handler *CommonHandleWrapper) InitialHandle(update *tgbotapi.Update) error {
	if handler.Handler == nil {
		return nil
	}
	return handler.Handler.InitialHandle(update)
}

func (handler *CommonHandleWrapper) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	switch update.Message.Command() {
	case "start":
		handler.HandleStart(update)
		return handler, nil
	case "help":
		handler.HandleHelp(update)
		return handler, nil
	case "abort":
		handler.HandleAbort(update)
		return nil, nil
	}
	newHandler, err := handler.Handler.HandleUpdate(update, actor)
	if err != nil {
		if errors.Is(err, ErrUnknownCommand{}) {
			handler.HandleUnknownCommand(update, err)
			return handler, nil
		}
		return nil, err
	}
	handler.Handler = newHandler
	return handler, nil
}

func (handler *CommonHandleWrapper) GetCommands() []tgtypes.BotCommand {
	thisCommands := []tgtypes.BotCommand{
		{Command: "help", Description: "Показать список команд"},
		{Command: "abort", Description: "Вернуться в главное меню"},
	}
	if handler.Handler != nil {
		thisCommands = append(thisCommands, handler.Handler.GetCommands()...)
	}
	return thisCommands
}

func (handler *CommonHandleWrapper) GetHelpDescription() string {
	return handler.Handler.GetHelpDescription()
}

func (handler *CommonHandleWrapper) HandleStart(update *tgbotapi.Update) {
	handler.HandleHelp(update)
}

func (handler *CommonHandleWrapper) getAvailableCommandsText() string {
	helpTextBuilder := strings.Builder{}
	helpTextBuilder.WriteString("Доступные команды:\n")
	commands := handler.GetCommands()
	for _, command := range commands {
		helpTextBuilder.WriteString(fmt.Sprintf("/%s - %s\n", command.Command, command.Description))
	}
	return helpTextBuilder.String()
}

func (handler *CommonHandleWrapper) getHelpText() string {
	helpDescription := handler.GetHelpDescription()
	helpTextBuilder := strings.Builder{}
	helpTextBuilder.WriteString(helpDescription)
	helpTextBuilder.WriteString("\n\n")
	helpTextBuilder.WriteString(handler.getAvailableCommandsText())
	return helpTextBuilder.String()
}

func (handler *CommonHandleWrapper) HandleHelp(update *tgbotapi.Update) {
	handler.Handler.GetBot().SendLog(tgbotapi.NewMessage(update.Message.Chat.ID, handler.getHelpText()))
}

func (handler *CommonHandleWrapper) HandleAbort(update *tgbotapi.Update) {
	handler.GetBot().SendLog(tgbotapi.NewMessage(update.Message.Chat.ID, "Ок"))
	handler.Handler = nil
}

func (handler *CommonHandleWrapper) HandleUnknownCommand(update *tgbotapi.Update, err error) {
	textB := strings.Builder{}
	command := update.Message.Command()
	if command == "" {
		textB.WriteString("Требуется команда\n")
	} else {
		textB.WriteString("Неизвестная команда /" + command + "\n")
	}
	textB.WriteString(handler.getAvailableCommandsText())
	handler.GetBot().SendLog(tgbotapi.NewMessage(update.Message.Chat.ID, textB.String()))
}
