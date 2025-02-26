package permsengine

import (
	"fmt"
	"math/rand"
	"regexp"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/google/uuid"
	"github.com/imobulus/subchat-mc-server/src/mcserver"
	"github.com/imobulus/subchat-mc-server/src/mojang"
	"github.com/imobulus/subchat-mc-server/src/tgauth/mcauth/authdb"
	"github.com/pkg/errors"
)

type ServerPermsEngineConfig struct {
	CacheInvalidationDuration   time.Duration `yaml:"cache_invalidation_duration"`
	DefaultMinecraftLoginsLimit int           `yaml:"default_minecraft_logins_limit"`
	AdminTags                   []string      `yaml:"admin_tags"`
}

var DefaultServerPermsEngineConfig = ServerPermsEngineConfig{
	CacheInvalidationDuration:   5 * time.Minute,
	DefaultMinecraftLoginsLimit: 2,
}

type ServerPermsEngine struct {
	config     ServerPermsEngineConfig
	dbExecutor *authdb.AuthDbExecutor
	adminTags  map[string]struct{}
	random     *rand.Rand
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

func NewServerPermsEngine(config ServerPermsEngineConfig, dbExecutor *authdb.AuthDbExecutor) (*ServerPermsEngine, error) {
	adminTags := map[string]struct{}{}
	for _, tag := range config.AdminTags {
		adminTags[tag] = struct{}{}
	}
	return &ServerPermsEngine{
		config:     config,
		dbExecutor: dbExecutor,
		adminTags:  adminTags,
		random:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}, nil
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func (engine *ServerPermsEngine) GeneratePassword() string {
	b := make([]rune, 20)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
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

func (engine *ServerPermsEngine) GetMinecraftLoginLimitByActor(actor *authdb.Actor) int {
	limit := engine.config.DefaultMinecraftLoginsLimit
	if actor.CustomMinecraftLoginLimit != nil {
		limit = *actor.CustomMinecraftLoginLimit
	}
	return limit
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
	limit := engine.GetMinecraftLoginLimitByActor(&actor)
	if limit >= 0 && len(actor.MinecraftAccounts) >= limit {
		return ErrorExceededMaxMinecraftLogins{
			CurrentNumber: len(actor.MinecraftAccounts),
			MaxLogins:     limit,
		}
	}
	return nil
}

func (engine *ServerPermsEngine) OptionalGetMinecraftAccount(login mojang.MinecraftLogin) (*authdb.MinecraftAccount, error) {
	return engine.dbExecutor.OptionalGetMinecraftAccount(login)
}

// can return ErrorLoginTaken
func (engine *ServerPermsEngine) AssignMinecraftLogin(
	actorId authdb.ActorId,
	login mojang.MinecraftLogin,
	isOnline bool,
	uuid uuid.UUID,
) error {
	err := engine.CheckAddMinecraftLoginPermission(actorId)
	if err != nil {
		return errors.Wrap(err, "failed to check permission")
	}
	err = engine.dbExecutor.AddMinecraftLogin(actorId, login, isOnline, uuid)
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
	Login   mojang.MinecraftLogin
}

func (e ErrorNotYourLogin) Error() string {
	return fmt.Sprintf("actor %d doesn't have login %s", e.ActorId, e.Login)
}

func (e ErrorNotYourLogin) Is(target error) bool {
	_, ok := target.(ErrorNotYourLogin)
	return ok
}

func (engine *ServerPermsEngine) CheckRevokeMinecraftLoginPermission(
	actorId authdb.ActorId,
	login mojang.MinecraftLogin,
) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	return checkActorHasLogin(&actor, login)
}

func checkActorHasLogin(actor *authdb.Actor, login mojang.MinecraftLogin) error {
	loginFound := false
	for _, acc := range actor.MinecraftAccounts {
		if acc.ID == login {
			loginFound = true
			break
		}
	}
	if !loginFound {
		return ErrorNotYourLogin{actor.ID, login}
	}
	return nil
}

func (engine *ServerPermsEngine) RevokeMinecraftLogin(
	actorId authdb.ActorId,
	login mojang.MinecraftLogin,
) error {
	err := engine.CheckRevokeMinecraftLoginPermission(actorId, login)
	if err != nil {
		return errors.Wrap(err, "failed to check permission")
	}
	err = engine.dbExecutor.RevokeMinecraftLogin(login)
	if err != nil {
		return errors.Wrap(err, "failed to revoke minecraft login")
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
		if chat.Approved {
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

// func (engine *ServerPermsEngine) GetActorIdsUpdatedSince(moment time.Time) ([]authdb.ActorId, error) {
// 	ids, err := engine.dbExecutor.GetActorIdsUpdatedSince(moment)
// 	if err != nil {
// 		return nil, errors.Wrap(err, "failed to get actor ids")
// 	}
// 	return ids, nil
// }

type UuidsUpdate struct {
	ToAdd    []string `json:"to_add"`
	ToDelete []string `json:"to_delete"`
}

// func (engine *ServerPermsEngine) GetAcceptedActorsWithAccounts() ([]authdb.Actor, error) {
// 	return engine.dbExecutor.GetAcceptedActorsWithAccounts()
// }

func (engine *ServerPermsEngine) UpdateWhitelist() error {
	actors, err := engine.dbExecutor.GetAcceptedActorsWithAccounts()
	if err != nil {
		return errors.Wrap(err, "failed to get accepted actors with accounts")
	}
	accounts := make([]mcserver.MinecraftAccountSpec, 0, len(actors))
	for _, actor := range actors {
		for _, acc := range actor.MinecraftAccounts {
			accounts = append(accounts, mcserver.MinecraftAccountSpec{
				Name:     acc.ID,
				PlayerId: acc.PlayerID,
			})
		}
	}
	err = engine.dbExecutor.SetWhitelist(accounts)
	if err != nil {
		return errors.Wrap(err, "failed to set whitelist")
	}
	return nil
}

var passwordRegex = regexp.MustCompile(`^[a-zA-Z0-9]{8,}$`)

const passwordRegexDescription = "Пароль должен состоять из не менее 8 латинских букв и цифр"

type ErrorInvalidPasswordFormat struct{}

func (e ErrorInvalidPasswordFormat) Error() string {
	return "invalid password format"
}
func (e ErrorInvalidPasswordFormat) Is(target error) bool {
	_, ok := target.(ErrorInvalidPasswordFormat)
	return ok
}
func (e ErrorInvalidPasswordFormat) Describe() string {
	return passwordRegexDescription
}

func (engine *ServerPermsEngine) CheckSetPasswordPermission(actorId authdb.ActorId, login mojang.MinecraftLogin, password string) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	hasLoginErr := checkActorHasLogin(&actor, login)
	if hasLoginErr != nil {
		return hasLoginErr
	}
	if !passwordRegex.MatchString(password) {
		return ErrorInvalidPasswordFormat{}
	}
	return nil
}

func (engine *ServerPermsEngine) SetPassword(actorId authdb.ActorId, minecraftLogin mojang.MinecraftLogin, password string) error {
	err := engine.CheckSetPasswordPermission(actorId, minecraftLogin, password)
	if err != nil {
		return errors.Wrap(err, "failed to check permission to set password")
	}
	err = engine.dbExecutor.SetPassword(minecraftLogin, password)
	if err != nil {
		return errors.Wrap(err, "failed to set password")
	}
	return nil
}

func (engine *ServerPermsEngine) CheckApproveChatPermission(actorId authdb.ActorId) error {
	actor := authdb.Actor{ID: actorId}
	err := engine.dbExecutor.GetActor(&actor)
	if err != nil {
		return err
	}
	if !actor.IsAdmin {
		return ErrorAdminPermissionDenied{"can't approve chat"}
	}
	return nil
}

func (engine *ServerPermsEngine) ApproveChat(actorId authdb.ActorId, chatId authdb.TgChatId) error {
	err := engine.CheckApproveChatPermission(actorId)
	if err != nil {
		return errors.Wrap(err, "failed to check permission to approve chat")
	}
	engine.dbExecutor.ApproveChat(chatId, actorId)
	return nil
}
