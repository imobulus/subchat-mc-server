package permsengine

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/pkg/errors"
)

type ServerPermsEngineConfig struct {
	CacheInvalidationDuration   time.Duration     `yaml:"cache_invalidation_duration"`
	DefaultMinecraftLoginsLimit int               `yaml:"default_minecraft_logins_limit"`
	AdminTags                   []string          `yaml:"admin_tags"`
	AcceptedCahats              []authdb.TgChatId `yaml:"accepted_chats"`
}

var DefaultServerPermsEngineConfig = ServerPermsEngineConfig{
	CacheInvalidationDuration:   5 * time.Minute,
	DefaultMinecraftLoginsLimit: 2,
}

type ServerPermsEngine struct {
	config        ServerPermsEngineConfig
	dbExecutor    *authdb.AuthDbExecutor
	acceptedChats map[authdb.TgChatId]struct{}
	adminTags     map[string]struct{}
}

type ErrorAdminPermissionDenied struct {
	Msg string
}

func (e ErrorAdminPermissionDenied) Error() string {
	return "admin permission denied, " + e.Msg
}

func (e ErrorAdminPermissionDenied) Is(target error) bool {
	_, ok := target.(ErrorAdminPermissionDenied)
	return ok
}

func NewServerPermsEngine(config ServerPermsEngineConfig, dbExecutor *authdb.AuthDbExecutor) *ServerPermsEngine {
	acceptedChats := map[authdb.TgChatId]struct{}{}
	for _, chat := range config.AcceptedCahats {
		acceptedChats[chat] = struct{}{}
	}
	adminTags := map[string]struct{}{}
	for _, tag := range config.AdminTags {
		adminTags[tag] = struct{}{}
	}
	return &ServerPermsEngine{
		config:        config,
		dbExecutor:    dbExecutor,
		acceptedChats: acceptedChats,
		adminTags:     adminTags,
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
		return ErrorAdminPermissionDenied{"can't verify actor"}
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

func (e ErrorExceededMaxMinecraftLogins) Is(target error) bool {
	_, ok := target.(ErrorExceededMaxMinecraftLogins)
	return ok
}

type ErrorNotAccepted struct {
	ActorId authdb.ActorId
}

func (e ErrorNotAccepted) Error() string {
	return fmt.Sprintf("actor %d is not accepted", e.ActorId)
}

func (e ErrorNotAccepted) Is(target error) bool {
	_, ok := target.(ErrorNotAccepted)
	return ok
}

func (engine *ServerPermsEngine) CheckAddMinecraftLoginPermission(
	actorId authdb.ActorId,
) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	if !actor.Accepted {
		return ErrorNotAccepted{actorId}
	}
	limit := engine.config.DefaultMinecraftLoginsLimit
	if actor.CustomMinecraftLoginLimit != nil {
		limit = *actor.CustomMinecraftLoginLimit
	}
	fmt.Println("limit is ", limit)
	if limit >= 0 && len(actor.MinecraftAccounts) >= limit {
		return ErrorExceededMaxMinecraftLogins{
			CurrentNumber: len(actor.MinecraftAccounts),
			MaxLogins:     limit,
		}
	}
	return nil
}

func (engine *ServerPermsEngine) OptionalGetMinecraftAccount(login authdb.MinecraftLogin) (*authdb.MinecraftAccount, error) {
	return engine.dbExecutor.OptionalGetMinecraftAccount(login)
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

type ErrorNotYourLogin struct {
	ActorId authdb.ActorId
	Login   authdb.MinecraftLogin
}

func (e ErrorNotYourLogin) Error() string {
	return fmt.Sprintf("actor %d doesn't have login %s", e.ActorId, e.Login)
}

func (e ErrorNotYourLogin) Is(target error) bool {
	_, ok := target.(ErrorNotYourLogin)
	return ok
}

func (engine *ServerPermsEngine) CheckRemoveMinecraftLoginPermission(
	actorId authdb.ActorId,
	login authdb.MinecraftLogin,
) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	loginFound := false
	for _, acc := range actor.MinecraftAccounts {
		if acc.ID == login {
			loginFound = true
			break
		}
	}
	if !loginFound {
		return ErrorNotYourLogin{actorId, login}
	}
	return nil
}

func (engine *ServerPermsEngine) RemoveMinecraftLogin(
	actorId authdb.ActorId,
	login authdb.MinecraftLogin,
) error {
	err := engine.CheckRemoveMinecraftLoginPermission(actorId, login)
	if err != nil {
		return errors.Wrap(err, "failed to check permission")
	}
	err = engine.dbExecutor.RemoveMinecraftLogin(login)
	if err != nil {
		return errors.Wrap(err, "failed to remove minecraft login")
	}
	return nil
}

func (engine *ServerPermsEngine) computeActorAcceptedStatus(actor *authdb.Actor) bool {
	if actor.IsAdmin {
		return true
	}
	for _, maybeAdmin := range actor.VerifiedByAdmins {
		if maybeAdmin.IsAdmin {
			return true
		}
	}
	for _, chat := range actor.SeenInChats {
		if _, ok := engine.acceptedChats[chat.ID]; ok {
			return true
		}
	}
	return false
}

func (engine *ServerPermsEngine) updateAcceptedStatus(actor *authdb.Actor, doRemove bool) error {
	accepted := engine.computeActorAcceptedStatus(actor)
	if accepted == actor.Accepted {
		return nil
	}
	if !doRemove && !accepted {
		return nil
	}
	err := engine.dbExecutor.SetAccept(actor.ID, accepted)
	if err != nil {
		return errors.Wrap(err, "failed to update accept status")
	}
	return nil
}

func (engine *ServerPermsEngine) computeAdminStatus(actor *authdb.Actor) bool {
	for _, tgAccount := range actor.TgAccounts {
		if _, ok := engine.adminTags[tgAccount.LastSeenInfo.UserName]; ok {
			return true
		}
	}
	return false
}

func (engine *ServerPermsEngine) updateAdminStatus(actor *authdb.Actor) error {
	isAdmin := engine.computeAdminStatus(actor)
	if isAdmin == actor.IsAdmin {
		return nil
	}
	err := engine.dbExecutor.SetAdmin(actor.ID, isAdmin)
	if err != nil {
		return errors.Wrap(err, "failed to update admin status")
	}
	return nil
}

func (engine *ServerPermsEngine) UpdateActorStatus(actorId authdb.ActorId, doRemove bool) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	err = engine.updateAcceptedStatus(&actor, doRemove)
	if err != nil {
		return errors.Wrap(err, "failed to update accept status")
	}
	err = engine.updateAdminStatus(&actor)
	if err != nil {
		return errors.Wrap(err, "failed to update accept status")
	}
	return nil
}

func (engine *ServerPermsEngine) GetActorIdsUpdatedSince(moment time.Time) ([]authdb.ActorId, error) {
	ids, err := engine.dbExecutor.GetActorIdsUpdatedSince(moment)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get actor ids")
	}
	return ids, nil
}
