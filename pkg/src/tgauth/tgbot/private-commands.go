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
	lastActor *authdb.Actor
	bot       *TgBot
}

func NewPrivateChatHandler(bot *TgBot) *PrivateChatHandler {
	return &PrivateChatHandler{bot: bot}
}

func (handler *PrivateChatHandler) IsLAstAdmin() bool {
	return handler.lastActor != nil && handler.lastActor.IsAdmin
}

func (handler *PrivateChatHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	handler.lastActor = actor
	return handler, nil
}

func (handler *PrivateChatHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	command := update.Message.Command()
	switch command {
	case "add_minecraft_login":
		return &AddMinecraftLoginHandler{bot: handler.bot}, nil
	case "remove_minecraft_login":
		return &RemoveMinecraftLoginHandler{bot: handler.bot}, nil
	case "newpassword":
		return &NewPasswordHandler{bot: handler.bot}, nil
	default:
		if handler.IsLAstAdmin() {
			switch command {
			case "approve_user":
				return &ApproveUserHandler{bot: handler.bot}, nil
			case "disapprove_user":
				return &DisapproveUserHandler{bot: handler.bot}, nil
			case "list_users":
				return &ListUsersHandler{bot: handler.bot}, nil
			}
		}
		return nil, ErrUnknownCommand{Update: update, Command: command}
	}
}

func (handler *PrivateChatHandler) GetCommands() []tgtypes.BotCommand {
	commands := []tgtypes.BotCommand{
		{Command: "add_minecraft_login", Description: "Зарегистрировать аккаунт на сервере"},
		{Command: "remove_minecraft_login", Description: "Удалить аккаунт с сервера"},
		{Command: "newpassword", Description: "Сгенерировать новый пароль для аккаунта"},
	}
	if handler.IsLAstAdmin() {
		commands = append(commands, []tgtypes.BotCommand{
			{Command: "approve_user", Description: "Одобрить пользователя"},
			{Command: "disapprove_user", Description: "Отклонить пользователя"},
			{Command: "list_users", Description: "Список пользователей"},
		}...)
	}
	return commands
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
		permissionDenied := false
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
				permissionDenied = true
				replyBuilder.WriteString("Превышен лимит зарегистрированных аккаунтов, возврат в главное меню")
			} else if errors.Is(err, permsengine.ErrorNotAccepted{}) {
				permissionDenied = true
				replyBuilder.WriteString("Мне надо увидеть вас в чате прежде чем вы сможете зарегистрировать аккаунт. Используйте /imhere@subchat_sentry_bot в сабчате")
			} else {
				return handler, err
			}
		} else {
			replyBuilder.WriteString("Введите аккаунт")
		}
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, replyBuilder.String())
		// msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
		handler.bot.SendLog(msg)
		if permissionDenied {
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
		if errors.Is(err, permsengine.ErrorNotAccepted{}) {
			msg := tgbotapi.NewMessage(
				update.Message.Chat.ID,
				"Мне надо увидеть вас в чате прежде чем вы сможете зарегистрировать аккаунт. Используйте /imhere@subchat_sentry_bot в сабчате",
			)
			handler.bot.SendLog(msg)
			return nil, nil
		}
		return nil, err
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт добавлен")
	handler.bot.SendLog(msg)
	newPassword := handler.bot.permsEngine.GeneratePassword()
	err = handler.bot.permsEngine.SetPassword(actor.ID, login, newPassword)
	if err != nil {
		return nil, err
	}
	msg = tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf(
		"Пароль для аккаунта %s: %s\nС маленькой вероятностью он мог не установиться на сервере. В этом случае используйте /newpassword",
		login, newPassword,
	))
	handler.bot.SendLog(msg)
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

type RemoveMinecraftLoginHandler struct {
	bot *TgBot
}

func (handler *RemoveMinecraftLoginHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if len(actor.MinecraftAccounts) == 0 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет зарегистрированных аккаунтов")
		handler.bot.SendLog(msg)
		return nil, nil
	}
	return handler, nil
}

func (handler *RemoveMinecraftLoginHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	login, err := authdb.MakeMinecraftLogin(update.Message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт содержит недопустимые символы или слишком короткий или длинный, введите другой")
		handler.bot.SendLog(msg)
		return handler, nil
	}
	acc, err := handler.bot.permsEngine.OptionalGetMinecraftAccount(login)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт не найден")
		handler.bot.SendLog(msg)
		return nil, nil
	}
	err = handler.bot.permsEngine.RemoveMinecraftLogin(actor.ID, login)
	if err != nil {
		if errors.Is(err, permsengine.ErrorNotYourLogin{}) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Этот аккаунт не принадлежит вам")
			handler.bot.SendLog(msg)
		}
		return nil, err
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт удален")
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *RemoveMinecraftLoginHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *RemoveMinecraftLoginHandler) GetHelpDescription() string {
	return "Сейчас вы удаляете аккакнт с сервера"
}
func (handler *RemoveMinecraftLoginHandler) GetBot() *TgBot {
	return handler.bot
}

type NewPasswordHandler struct {
	bot *TgBot
}

func (handler *NewPasswordHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	respBuilder := strings.Builder{}
	respBuilder.WriteString("Введите аккаунт для которого хотите сгенерировать новый пароль.\n" + "Ваши аккаунты:\n")
	for _, acc := range actor.MinecraftAccounts {
		respBuilder.WriteString(string(acc.ID))
		respBuilder.WriteRune('\n')
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, respBuilder.String())
	handler.bot.SendLog(msg)
	return handler, nil
}

func (handler *NewPasswordHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if update.Message.Command() != "" {
		return handler, ErrUnknownCommand{Update: update, Command: update.Message.Command()}
	}
	login, err := authdb.MakeMinecraftLogin(update.Message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт содержит недопустимые символы или слишком короткий или длинный, введите другой")
		handler.bot.SendLog(msg)
		return handler, nil
	}
	newPassword := handler.bot.permsEngine.GeneratePassword()
	err = handler.bot.permsEngine.SetPassword(actor.ID, login, newPassword)
	if err != nil {
		if errors.Is(err, permsengine.ErrorNotYourLogin{}) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Этот аккаунт не принадлежит вам")
			handler.bot.SendLog(msg)
			return nil, nil
		}
		return nil, err
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf(
		"Пароль для аккаунта %s: %s\nС маленькой вероятностью он мог не установиться на сервере. В этом случае используйте /newpassword",
		login, newPassword,
	))
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *NewPasswordHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *NewPasswordHandler) GetHelpDescription() string {
	return "Сейчас вы генерируете новый пароль для аккаунта"
}
func (handler *NewPasswordHandler) GetBot() *TgBot {
	return handler.bot
}

type ApproveUserHandler struct {
	bot *TgBot
}

func (handler *ApproveUserHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	// not implemented
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Not implemented")
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *ApproveUserHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	return nil, nil
}
func (handler *ApproveUserHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *ApproveUserHandler) GetHelpDescription() string {
	return "Одобрить пользователя"
}
func (handler *ApproveUserHandler) GetBot() *TgBot {
	return handler.bot
}

type DisapproveUserHandler struct {
	bot *TgBot
}

func (handler *DisapproveUserHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	// not implemented
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Not implemented")
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *DisapproveUserHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	return nil, nil
}

func (handler *DisapproveUserHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *DisapproveUserHandler) GetHelpDescription() string {
	return "Отклонить пользователя"
}
func (handler *DisapproveUserHandler) GetBot() *TgBot {
	return handler.bot
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
