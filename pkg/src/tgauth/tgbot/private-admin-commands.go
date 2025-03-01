package tgbot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
	"gorm.io/gorm"
)

type CommonAdminHandler struct {
	bot *TgBot
}

func NewCommonAdminHandler(bot *TgBot) *CommonAdminHandler {
	return &CommonAdminHandler{bot: bot}
}

func (handler *CommonAdminHandler) promptForUser(update *tgbotapi.Update) error {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Введите id пользователя в виде числа или юзернейм начиная с @")
	_, err := handler.bot.api.Send(msg)
	return err
}

func (handler *CommonAdminHandler) promptForConfirmation(update *tgbotapi.Update, actor *authdb.Actor) error {
	responseBuilder := strings.Builder{}
	responseBuilder.WriteString(getUserDescriptionForAdmin(actor))
	responseBuilder.WriteString("\nОдобрить? /confirm /abort")
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, responseBuilder.String())
	_, err := handler.bot.api.Send(msg)
	return err
}

func getUserDescriptionForAdmin(actor *authdb.Actor) string {
	descBuilder := strings.Builder{}
	descBuilder.WriteString(fmt.Sprintf("Пользователь %s %s:\n", actor.Nickname, actor.Description))
	for _, tgAcc := range actor.TgAccounts {
		additions := []string{}
		if tgAcc.LastSeenInfo.UserName != "" {
			additions = append(additions, "@"+tgAcc.LastSeenInfo.UserName)
		}
		if tgAcc.LastSeenInfo.FirstName != "" {
			additions = append(additions, tgAcc.LastSeenInfo.FirstName)
		}
		if tgAcc.LastSeenInfo.LastName != "" {
			additions = append(additions, tgAcc.LastSeenInfo.LastName)
		}
		descBuilder.WriteString(fmt.Sprintf("Tg %d", tgAcc.ID))
		if len(additions) > 0 {
			descBuilder.WriteString(strings.Join(additions, " "))
		}
	}
	return descBuilder.String()
}

type ErrorInvalidActorTextRepr struct {
	Text string
}

func (e ErrorInvalidActorTextRepr) Error() string {
	return fmt.Sprintf("Invalid actor text representation: %s", e.Text)
}

func (e ErrorInvalidActorTextRepr) Is(err error) bool {
	_, ok := err.(ErrorInvalidActorTextRepr)
	return ok
}

func (handler *CommonAdminHandler) parseUserRefFromText(text string) (*authdb.Actor, error) {
	actorToApprove := &authdb.Actor{}
	if strings.HasPrefix(text, "@") {
		// username
		err := handler.bot.permsEngine.GetActorByTgUserName(strings.TrimPrefix(text, "@"), actorToApprove)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			} else {
				return nil, err
			}
		}
		return actorToApprove, nil
	}
	userId, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, ErrorInvalidActorTextRepr{Text: text}
	}
	err = handler.bot.permsEngine.GetActorByTgUser(authdb.TgUserId(userId), actorToApprove)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	return actorToApprove, nil
}

func (handler *CommonAdminHandler) parseUserFromUpdateInteractive(update *tgbotapi.Update) (*authdb.Actor, error) {
	text := update.Message.Text
	user, err := handler.parseUserRefFromText(text)
	if err != nil {
		if errors.Is(err, ErrorInvalidActorTextRepr{}) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Неверный формат пользователя")
			_, err := handler.bot.api.Send(msg)
			return nil, err
		}
		return nil, err
	}
	if user == nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Пользователь не найден")
		_, err := handler.bot.api.Send(msg)
		return nil, err
	}
	return user, nil
}

func (handler *CommonAdminHandler) processConfirmationInteractive(update *tgbotapi.Update) (*bool, error) {
	var resp bool
	switch update.Message.Command() {
	case "confirm":
		err := handler.bot.aux.SetReaction(authdb.TgChatId(update.Message.Chat.ID), int64(update.Message.MessageID), "👍")
		resp = true
		return &resp, err
	case "abort":
		err := handler.bot.aux.SetReaction(authdb.TgChatId(update.Message.Chat.ID), int64(update.Message.MessageID), "👍")
		resp = false
		return &resp, err
	default:
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Отправьте /confirm или /abort")
		_, err := handler.bot.api.Send(msg)
		return nil, err
	}
}

type UserActionType string

const (
	UserActionTypeApprove UserActionType = "approve"
	UserActionTypeReject  UserActionType = "reject"
	UserActionTypeBan     UserActionType = "ban"
)

type UserActionHandler struct {
	h            *CommonAdminHandler
	selectedUser *authdb.Actor
	actionType   UserActionType
}

func (handler *UserActionHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if handler.selectedUser == nil {
		err := handler.h.promptForUser(update)
		return handler, err
	}
	err := handler.h.promptForConfirmation(update, actor)
	return handler, err
}

func (handler *UserActionHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if handler.selectedUser == nil {
		actor, err := handler.h.parseUserFromUpdateInteractive(update)
		if err != nil {
			return nil, err
		}
		if actor != nil {
			handler.selectedUser = actor
		}
		return handler, nil
	}
	return handler.processConfirmation(update, actor)
}

func (handler *UserActionHandler) processConfirmation(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	resp, err := handler.h.processConfirmationInteractive(update)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return handler, nil
	}
	if !*resp {
		return nil, nil
	}
	switch handler.actionType {
	case UserActionTypeApprove:
		err = handler.h.bot.permsEngine.AdminVerifyActor(actor.ID, handler.selectedUser.ID)
		if err != nil {
			return nil, err
		}
	case UserActionTypeReject:
		err = handler.h.bot.permsEngine.AdminRejectActor(actor.ID, handler.selectedUser.ID)
		if err != nil {
			return nil, err
		}
	case UserActionTypeBan:
		err = handler.h.bot.permsEngine.AdminBanActor(actor.ID, handler.selectedUser.ID)
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (handler *UserActionHandler) GetCommands() []tgtypes.BotCommand {
	if handler.selectedUser == nil {
		return nil
	}
	return []tgtypes.BotCommand{
		{Command: "confirm", Description: "Одобрить"},
		// abort command handler upstream
	}
}
func (handler *UserActionHandler) GetHelpDescription() string {
	if handler.selectedUser == nil {
		return "Введите пользователя для одобрения"
	}
	return "Одобрить пользователя " + handler.selectedUser.Nickname
}
func (handler *UserActionHandler) GetBot() *TgBot {
	return handler.h.bot
}

type ListUsersHandler struct {
	bot *TgBot
}

func (handler *ListUsersHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	// not implemented
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Not implemented")
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *ListUsersHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	return nil, nil
}

func (handler *ListUsersHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *ListUsersHandler) GetHelpDescription() string {
	return "Список пользователей"
}
func (handler *ListUsersHandler) GetBot() *TgBot {
	return handler.bot
}
