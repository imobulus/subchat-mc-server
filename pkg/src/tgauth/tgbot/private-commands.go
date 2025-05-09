package tgbot

import (
	"errors"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/imobulus/subchat-mc-server/src/mojang"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/permsengine"
	"github.com/imobulus/subchat-mc-server/src/tgauth/tgbot/tgtypes"
	"go.uber.org/zap"
)

type PrivateChatHandler struct {
	lastActor *authdb.Actor
	bot       *TgBot
}

func NewPrivateChatHandler(bot *TgBot, initialActor *authdb.Actor) *PrivateChatHandler {
	return &PrivateChatHandler{
		lastActor: initialActor,
		bot:       bot,
	}
}

func (handler *PrivateChatHandler) IsLastAdmin() bool {
	return handler.lastActor != nil && handler.lastActor.IsAdmin
}

func (handler *PrivateChatHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	handler.lastActor = actor
	return handler, nil
}

func (handler *PrivateChatHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	command := update.Message.Command()
	switch command {
	case "my_minecraft_logins":
		return &MyMinecraftLoginsHandler{bot: handler.bot}, nil
	case "add_minecraft_login":
		return &AddMinecraftLoginHandler{bot: handler.bot}, nil
	case "remove_minecraft_login":
		return &RevokeMinecraftLoginHandler{bot: handler.bot}, nil
	case "newpassword":
		return &NewPasswordHandler{bot: handler.bot}, nil
	case "access":
		return &AccessHandler{bot: handler.bot}, nil
	default:
		if handler.IsLastAdmin() {
			switch command {
			case "approve_user":
				return &UserActionHandler{h: NewCommonAdminHandler(handler.bot), actionType: UserActionTypeApprove}, nil
			case "reject_user":
				return &UserActionHandler{h: NewCommonAdminHandler(handler.bot), actionType: UserActionTypeReject}, nil
			case "ban_user":
				return &UserActionHandler{h: NewCommonAdminHandler(handler.bot), actionType: UserActionTypeBan}, nil
			case "list_users":
				return &ListUsersHandler{h: NewCommonAdminHandler(handler.bot)}, nil
			}
		}
		return nil, ErrUnknownCommand{Update: update, Command: command}
	}
}

func (handler *PrivateChatHandler) GetCommands() []tgtypes.BotCommand {
	commands := []tgtypes.BotCommand{
		{Command: "my_minecraft_logins", Description: "Список ваших аккаунтов"},
		{Command: "add_minecraft_login", Description: "Зарегистрировать аккаунт на сервере"},
		{Command: "remove_minecraft_login", Description: "Удалить аккаунт с сервера"},
		{Command: "newpassword", Description: "Сгенерировать новый пароль для аккаунта"},
		{Command: "access", Description: "Получить доступ к серверу"},
	}
	if handler.IsLastAdmin() {
		commands = append(commands, []tgtypes.BotCommand{
			{Command: "approve_user", Description: "Одобрить пользователя"},
			{Command: "reject_user", Description: "Отклонить пользователя"},
			{Command: "ban_user", Description: "Забанить пользователя"},
			{Command: "list_users", Description: "Список пользователей"},
		}...)
	}
	return commands
}

func (handler *PrivateChatHandler) GetHelpDescription() string {
	return "Здесь вы можете получить доступ к серверу <a href=\"https://subchat.imobul.us/\">subchat.imobul.us</a>"
}
func (handler *PrivateChatHandler) GetBot() *TgBot {
	return handler.bot
}

type MyMinecraftLoginsHandler struct {
	bot *TgBot
}

func (bot *TgBot) needToVerifyDisclaimer() string {
	return "У вас ещё нет доступа. Используйте /access чтобы получить доступ к серверу."
}

func (handler *MyMinecraftLoginsHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	msgBuilder := strings.Builder{}
	if len(actor.MinecraftAccounts) == 0 {
		msgBuilder.WriteString("У вас нет зарегистрированных аккаунтов")
	} else {
		msgBuilder.WriteString("Ваши аккаунты:\n")
		for _, acc := range actor.MinecraftAccounts {
			msgBuilder.WriteString(fmt.Sprintf("<code>%s</code>\n", acc.ID))
		}
	}
	limit := handler.bot.permsEngine.GetMinecraftLoginLimitByActor(actor.ID)
	msgBuilder.WriteString(fmt.Sprintf("\nЛимит аккаунтов: %d/%d", len(actor.MinecraftAccounts), limit))
	if !actor.Accepted {
		msgBuilder.WriteString(
			"\n\n" + handler.bot.needToVerifyDisclaimer(),
		)
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgBuilder.String())
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *MyMinecraftLoginsHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	return nil, nil
}

func (handler *MyMinecraftLoginsHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}

func (handler *MyMinecraftLoginsHandler) GetHelpDescription() string {
	return "Список ваших аккаунтов"
}

func (handler *MyMinecraftLoginsHandler) GetBot() *TgBot {
	return handler.bot
}

type AddMinecraftLoginHandler struct {
	bot          *TgBot
	initialized  bool
	enteredLogin mojang.MinecraftLogin
	isOnline     bool // defaults to offline
	onlineId     uuid.UUID
}

func (handler *AddMinecraftLoginHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if !handler.initialized {
		handler.initialized = true
		permissionDenied := false
		replyBuilder := strings.Builder{}
		if len(actor.MinecraftAccounts) > 0 {
			replyBuilder.WriteString("Ваши аккаунты:\n")
			replyBuilder.WriteString(buildMinecraftAccountsListForTg(actor.MinecraftAccounts))
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
				replyBuilder.WriteString(handler.bot.needToVerifyDisclaimer())
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

func (handler *AddMinecraftLoginHandler) handleLoginExists(update *tgbotapi.Update, login mojang.MinecraftLogin) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Аккаунт <code>%s</code> уже занят, введите другой", login))
	handler.bot.SendLog(msg)
}

func (handler *AddMinecraftLoginHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if handler.enteredLogin == "" {
		return handler.processLogin(update, actor)
	} else {
		return handler.processIsOnline(update, actor)
	}
}

func (handler *AddMinecraftLoginHandler) processLogin(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	login, err := mojang.MakeMinecraftLogin(update.Message.Text)
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
		if maybeAccount.ActorID != nil && *maybeAccount.ActorID == actor.ID {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Вы уже зарегистрировали этот аккаунт")
			handler.bot.SendLog(msg)
			return nil, nil
		}
		if maybeAccount.ActorID != nil {
			handler.handleLoginExists(update, login)
			return handler, nil
		}
	}
	handler.enteredLogin = login
	needOnlineRequest := true
	disclaimer := "\n\nОбратите внимание, что если владелец официального аккаунта с этим именем присоединится к серверу, вам придётся сменить ник."
	playerId, err := mojang.QueryOnlineUuid(login, handler.bot.ctx)
	if err != nil {
		if errors.Is(err, mojang.NoSuchPlayerErr{}) {
			needOnlineRequest = false
		} else {
			handler.bot.logger.Error("Failed to query online uuid", zap.Error(err))
			handler.bot.SendLog(tgbotapi.NewMessage(
				update.Message.Chat.ID,
				fmt.Sprintf("Не получилось проверить есть ли официальный аккаунт с именем <code>%s</code>. ", login)+
					"Отправьте /official если у вас есть лиценизия или /cracked если нет."+disclaimer,
			))
		}
	} else {
		handler.onlineId = playerId
		handler.bot.SendLog(tgbotapi.NewMessage(
			update.Message.Chat.ID,
			fmt.Sprintf("Найден официальный аккаунт с именем <code>%s</code>. ", login)+
				"Отправьте /official если вы владелец или /cracked если просто хотите этот ник."+disclaimer,
		))
	}
	if needOnlineRequest {
		return handler, nil
	} else {
		return handler.finishAdding(update, actor)
	}
}

func (handler *AddMinecraftLoginHandler) processIsOnline(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	switch update.Message.Command() {
	case "official":
		handler.isOnline = true
	case "cracked":
		handler.isOnline = false
	default:
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Отправьте /official если это официальный аккаунт или /cracked если нет")
		handler.bot.SendLog(msg)
		return handler, nil
	}
	return handler.finishAdding(update, actor)
}

func (handler *AddMinecraftLoginHandler) finishAdding(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	var playerId uuid.UUID
	if handler.isOnline {
		if handler.onlineId == uuid.Nil {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf(
				"Не получилось найти ID игрока <code>%s</code> из-за неизвестной ошибки. Обратитесь к администратору.",
				handler.enteredLogin,
			))
			handler.bot.SendLog(msg)
			return nil, nil
		}
		playerId = handler.onlineId
	} else {
		playerId = mojang.GetOfflineUuid(handler.enteredLogin)
	}
	err := handler.bot.permsEngine.AssignMinecraftLogin(actor.ID, handler.enteredLogin, handler.isOnline, playerId)
	if err != nil {
		if errors.Is(err, authdb.ErrorLoginTaken{}) {
			handler.handleLoginExists(update, handler.enteredLogin)
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
				handler.bot.needToVerifyDisclaimer(),
			)
			handler.bot.SendLog(msg)
			return nil, nil
		}
		return nil, err
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт добавлен, теперь с ним можно зайти на сервер")
	handler.bot.SendLog(msg)
	if !handler.isOnline {
		newPassword := handler.bot.permsEngine.GeneratePassword()
		err = handler.bot.permsEngine.SetPassword(actor.ID, handler.enteredLogin, newPassword)
		if err != nil {
			return nil, err
		}
		msg = tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf(
			"Пароль для аккаунта <code>%s</code>: <code>%s</code>\n"+
				"С маленькой вероятностью он мог не установиться на сервере. В этом случае используйте /newpassword",
			handler.enteredLogin, newPassword,
		))
		msg.ParseMode = tgbotapi.ModeHTML
		handler.bot.SendLog(msg)
	}
	return nil, nil
}

func (handler *AddMinecraftLoginHandler) GetCommands() []tgtypes.BotCommand {
	if handler.enteredLogin == "" {
		return nil
	}
	return []tgtypes.BotCommand{
		{Command: "official", Description: "Официальный аккаунт"},
		{Command: "cracked", Description: "Нет официального аккаунта"},
	}
}
func (handler *AddMinecraftLoginHandler) GetHelpDescription() string {
	return "Сейчас вы регистрируете ник на сервере"
}
func (handler *AddMinecraftLoginHandler) GetBot() *TgBot {
	return handler.bot
}

type RevokeMinecraftLoginHandler struct {
	bot *TgBot
}

func buildMinecraftAccountsListForTg(list []authdb.MinecraftAccount) string {
	b := strings.Builder{}
	for _, acc := range list {
		b.WriteString(fmt.Sprintf("<code>%s</code>", acc.ID))
		if acc.IsOnline {
			b.WriteString(" (официальный)")
		}
		b.WriteRune('\n')
	}
	return b.String()
}

func (handler *RevokeMinecraftLoginHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if len(actor.MinecraftAccounts) == 0 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет зарегистрированных аккаунтов")
		handler.bot.SendLog(msg)
		return nil, nil
	}
	msgBuilder := strings.Builder{}
	msgBuilder.WriteString("Введите аккаунт который хотите удалить\n")
	msgBuilder.WriteString(buildMinecraftAccountsListForTg(actor.MinecraftAccounts))
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgBuilder.String())
	handler.bot.SendLog(msg)
	return handler, nil
}

func (handler *RevokeMinecraftLoginHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	login, err := mojang.MakeMinecraftLogin(update.Message.Text)
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
	err = handler.bot.permsEngine.RevokeMinecraftLogin(actor.ID, login)
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

func (handler *RevokeMinecraftLoginHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}
func (handler *RevokeMinecraftLoginHandler) GetHelpDescription() string {
	return "Сейчас вы удаляете аккаунт с сервера"
}
func (handler *RevokeMinecraftLoginHandler) GetBot() *TgBot {
	return handler.bot
}

type NewPasswordHandler struct {
	bot *TgBot
}

func (handler *NewPasswordHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if len(actor.MinecraftAccounts) == 0 {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "У вас нет зарегистрированных аккаунтов")
		handler.bot.SendLog(msg)
		return nil, nil
	}
	respBuilder := strings.Builder{}
	respBuilder.WriteString("Введите аккаунт для которого хотите сгенерировать новый пароль.\n" + "Ваши аккаунты:\n")
	respBuilder.WriteString(buildMinecraftAccountsListForTg(actor.MinecraftAccounts))
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, respBuilder.String())
	handler.bot.SendLog(msg)
	return handler, nil
}

func (handler *NewPasswordHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if update.Message.Command() != "" {
		return handler, ErrUnknownCommand{Update: update, Command: update.Message.Command()}
	}
	login, err := mojang.MakeMinecraftLogin(update.Message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт содержит недопустимые символы или слишком короткий или длинный, введите другой")
		handler.bot.SendLog(msg)
		return handler, nil
	}
	var actorsAccount *authdb.MinecraftAccount
	for _, acc := range actor.MinecraftAccounts {
		if acc.ID == login {
			actorsAccount = &acc
			break
		}
	}
	if actorsAccount == nil {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Аккаунт не найден")
		handler.bot.SendLog(msg)
		return nil, nil
	}
	if actorsAccount.IsOnline {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Официальный аккаунт не")
		handler.bot.SendLog(msg)
		return nil, nil
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

type AccessHandler struct {
	bot *TgBot
}

func (handler *AccessHandler) InitialHandle(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	if actor.Accepted {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Вы уже получили доступ")
		handler.bot.SendLog(msg)
		return nil, nil
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Введите пароль для доступа. Если не знаете пароль спросите его в сабчате или у @imobulus")
	handler.bot.SendLog(msg)
	return handler, nil
}

func (handler *AccessHandler) HandleUpdate(update *tgbotapi.Update, actor *authdb.Actor) (InteractiveHandler, error) {
	err := handler.bot.permsEngine.HandleAccessPassword(actor.ID, update.Message.Text)
	if err != nil {
		if errors.Is(err, permsengine.ErrorWrongAccessPassword{}) {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Неверный пароль, попробуйте еще раз или используйте /abort")
			handler.bot.SendLog(msg)
			return handler, nil
		}
		return handler, err
	}
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Доступ получен")
	handler.bot.SendLog(msg)
	return nil, nil
}

func (handler *AccessHandler) GetCommands() []tgtypes.BotCommand {
	return nil
}

func (handler *AccessHandler) GetHelpDescription() string {
	return "Сейчас вы получаете доступ к серверу"
}

func (handler *AccessHandler) GetBot() *TgBot {
	return handler.bot
}
