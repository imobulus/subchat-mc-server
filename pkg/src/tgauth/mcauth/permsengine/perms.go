package permsengine

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/pkg/errors"
)

type ServerPermsEngineConfig struct {
	CacheInvalidationDuration   time.Duration
	DefaultMinecraftLoginsLimit int
}

var DefaultServerPermsEngineConfig = ServerPermsEngineConfig{
	CacheInvalidationDuration:   5 * time.Minute,
	DefaultMinecraftLoginsLimit: 2,
}

type ServerPermsEngine struct {
	config     ServerPermsEngineConfig
	dbExecutor *authdb.AuthDbExecutor
}

type ErrorPermissionDenied struct {
	Msg string
}

func (e *ErrorPermissionDenied) Error() string {
	return "permission denied, " + e.Msg
}

func NewServerPermsEngine(config ServerPermsEngineConfig, dbExecutor *authdb.AuthDbExecutor) *ServerPermsEngine {
	return &ServerPermsEngine{
		config:     config,
		dbExecutor: dbExecutor,
	}
}
func (engine *ServerPermsEngine) IsAdmin(actorId authdb.ActorId) (bool, error) {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return false, err
	}
	return actor.IsAdmin, nil

}

func (engine *ServerPermsEngine) CheckVerifyActorPermission(requestor authdb.ActorId) error {
	isAdmin, err := engine.IsAdmin(requestor)
	if err != nil {
		return errors.Wrap(err, "failed to check permission")
	}
	if !isAdmin {
		return &ErrorPermissionDenied{"you need to be admin"}
	}
	return nil
}

func (engine *ServerPermsEngine) AdminVerifyActor(
	requestor authdb.ActorId,
	actorId authdb.ActorId,
) error {
	err := engine.CheckVerifyActorPermission(requestor)
	if err != nil {
		return errors.Wrap(err, "failed to check permission")
	}
	err = engine.dbExecutor.VerifiedByAdmin(actorId, requestor)
	if err != nil {
		return errors.Wrap(err, "failed to verify actor")
	}
	return nil
}

func (engine *ServerPermsEngine) SeenInChat(
	actorId authdb.ActorId,
	chatId authdb.TgChatId,
) error {
	err := engine.dbExecutor.SeenInChat(actorId, chatId)
	if err != nil {
		return errors.Wrap(err, "failed to update seen in chat")
	}
	return nil
}

type ErrorExceededMaxMinecraftLogins struct {
	CurrentNumber int
	MaxLogins     int
}

func (e ErrorExceededMaxMinecraftLogins) Error() string {
	return fmt.Sprintf("exceeded max minecraft logins, current: %d, max: %d", e.CurrentNumber, e.MaxLogins)
}

func (engine *ServerPermsEngine) CheckAddMinecraftLoginPermission(
	actorId authdb.ActorId,
) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	limit := engine.config.DefaultMinecraftLoginsLimit
	if actor.CustomMinecraftLoginLimit != nil {
		limit = *actor.CustomMinecraftLoginLimit
	}
	if limit >= 0 && len(actor.MinecraftAccounts) >= limit {
		return ErrorExceededMaxMinecraftLogins{
			CurrentNumber: len(actor.MinecraftAccounts),
			MaxLogins:     limit,
		}
	}
	return nil
}

// can return ErrorLoginTaken
func (engine *ServerPermsEngine) AddMinecraftLogin(
	actorId authdb.ActorId,
	login authdb.MinecraftLogin,
) error {
	err := engine.CheckAddMinecraftLoginPermission(actorId)
	if err != nil {
		return errors.Wrap(err, "failed to check permission")
	}
	err = engine.dbExecutor.AddMinecraftLogin(actorId, login)
	if err != nil {
		return errors.Wrap(err, "failed to add minecraft login")
	}
	return nil
}

func (engine *ServerPermsEngine) GetActorByTgUser(tgUserId authdb.TgUserId, actor *authdb.Actor) error {
	err := engine.dbExecutor.GetActorByTgUser(tgUserId, actor)
	return err
}

func (engine *ServerPermsEngine) UpdateTgUserInfo(tguser tgbotapi.User) error {
	err := engine.dbExecutor.UpdateTgUserInfo(tguser)
	return err
}
