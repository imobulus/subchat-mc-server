package tgbot

import (
	"errors"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/permsengine"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
	"go.uber.org/zap"
)

type PublicChatHandler struct {
	bot *TgBot
}

func NewPublicChatHandler(bot *TgBot) *PublicChatHandler {
	return &PublicChatHandler{bot: bot}
}

func (handler *PublicChatHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	return handler, nil
}

func (handler *PublicChatHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	switch update.Message.Command() {
	case "approve":
		return handler.handleApproveCommand(update, actor)
	case "imhere":
		handler.bot.aux.SetReaction(tgtypes.UpdateChat(update), tgtypes.UpdateMessageId(update), "👀")
		return nil, nil
	}
	return nil, nil
}

func (handler *PublicChatHandler) GetHelpDescription() string {
	return ""
}

func (handler *PublicChatHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}

func (handler *PublicChatHandler) GetBot() *TgBot {
	return handler.bot
}

func (handler *PublicChatHandler) handleApproveCommand(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if !actor.IsAdmin {
		handler.bot.aux.SetReaction(tgtypes.UpdateChat(update), tgtypes.UpdateMessageId(update), "🗿")
		return nil, nil
	}
	err := handler.bot.permsEngine.ApproveChat(actor.ID, tgtypes.UpdateChat(update))
	if err != nil {
		if errors.Is(err, permsengine.ErrorAdminPermissionDenied{}) {
			handler.bot.aux.SetReaction(tgtypes.UpdateChat(update), tgtypes.UpdateMessageId(update), "🗿")
			return nil, nil
		}
		handler.bot.logger.Error("Failed to approve chat", zap.Error(err))
		handler.bot.aux.SetReaction(tgtypes.UpdateChat(update), tgtypes.UpdateMessageId(update), "🙉")
		return nil, nil
	}
	handler.bot.aux.SetReaction(tgtypes.UpdateChat(update), tgtypes.UpdateMessageId(update), "👍")
	return nil, nil
}
