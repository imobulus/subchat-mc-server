package authdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/google/uuid"
	"github.com/imobulus/subchat-mc-server/src/mcserver"
	"github.com/imobulus/subchat-mc-server/src/mojang"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type AuthDbExecutorConfig struct {
	ServerOverseerUrl string `yaml:"server_overseer_url"`
}

var DefaultAuthDbExecutorConfig = AuthDbExecutorConfig{
	ServerOverseerUrl: "http://mc-server:8080",
}

type AuthDbExecutor struct {
	config AuthDbExecutorConfig
	db     *gorm.DB
	logger *zap.Logger
}

func NewAuthDbExecutor(db *gorm.DB, config AuthDbExecutorConfig, logger *zap.Logger) (*AuthDbExecutor, error) {
	dbExec := &AuthDbExecutor{
		config: config,
		db:     db,
		logger: logger,
	}
	err := dbExec.InitDB()
	if err != nil {
		return nil, errors.Wrap(err, "fail to init db")
	}
	return dbExec, nil
}

func (authdb *AuthDbExecutor) InitDB() error {
	authdb.logger.Info("initializing db")
	err := authdb.db.SetupJoinTable(&Actor{}, "SeenInChats", &ActorSeenInChats{})
	if err != nil {
		return errors.Wrap(err, "fail to setup join table")
	}
	err = authdb.db.AutoMigrate(allSchemas...)
	if err != nil {
		return errors.Wrap(err, "fail to auto migrate schemas")
	}
	return nil
}

// updates tg user info if it exists. Creates new user if not.
// May throw an error in case of two simultaneous creations, the user is still created.
func (authdb *AuthDbExecutor) UpdateTgUserInfo(tguser tgbotapi.User) error {
	authdb.logger.Debug("updating tg user info", zap.Any("user", tguser))
	// find if user exists
	var users []TgUser
	err := authdb.db.Find(&users, tguser.ID).Error
	if err != nil {
		return errors.Wrapf(err, "fail to find tg user %s", ShortDescribeTgUser(tguser))
	}
	if len(users) > 1 {
		return errors.Errorf("found more than one tg user %s", ShortDescribeTgUser(tguser))
	}
	if len(users) == 0 {
		err := authdb.createTgUserWithActor(tguser)
		if err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				// only possible when a new user sends initial messages too fast
				authdb.logger.Warn("encountered UpdateTgUserInfo race, it is abnormal")
				return nil
			}
			return errors.Wrap(err, "calling create on update")
		}
		return nil
	}
	user := users[0]
	user.ID = TgUserId(tguser.ID)
	// user found
	err = authdb.db.Model(&user).Updates(TgUser{
		LastSeenInfo: tguser,
	}).Error
	if err != nil {
		return errors.Wrapf(err, "fail to update tg user %s", ShortDescribeTgUser(tguser))
	}
	return nil
}

func (authdb *AuthDbExecutor) createTgUserWithActor(tguser tgbotapi.User) error {
	authdb.logger.Debug("creating tg user", zap.Any("user", tguser))
	actor := Actor{
		Nickname:    "",
		Description: fmt.Sprintf("Telegram user %s", ShortDescribeTgUser(tguser)),
		IsAdmin:     false,
		TgAccounts: []TgUser{{
			ID:           TgUserId(tguser.ID),
			LastSeenInfo: tguser,
		}},
	}

	err := authdb.db.Create(&actor).Error
	if err != nil {
		return errors.Wrapf(err, "fail to create tg user %s", ShortDescribeTgUser(tguser))
	}
	return nil
}

// 0 duration means unlimited
func (authdb *AuthDbExecutor) BanActor(actorId ActorId, duration time.Duration, reason string) {
	authdb.logger.Debug("banning actor", zap.Uint("actor_id", uint(actorId)), zap.Duration("duration", duration), zap.String("reason", reason))
	ban := Ban{
		ActorID:     actorId,
		BanDuration: duration,
		Reason:      reason,
	}
	authdb.db.Create(&ban)
}

func (authdb *AuthDbExecutor) UnbanActor(actorId uint) {
	authdb.logger.Debug("unbanning actor", zap.Uint("actor_id", actorId))
	authdb.db.Where("actor_id = ?", actorId).Delete(&Ban{})
}

// populates association fields which are non-nil
func (authdb *AuthDbExecutor) GetActor(actor *Actor) error {
	authdb.logger.Debug("getting actor", zap.Uint("actor_id", uint(actor.ID)))
	if actor.ID == 0 {
		return errors.New("actor id is not set")
	}
	preloadFields := []string{
		"SeenInChats",
		"VerifiedByAdmins",
		"Bans",
		"TgAccounts",
		"MinecraftAccounts",
	}
	// if actor.SeenInChats != nil {
	// 	preloadFields = append(preloadFields, "SeenInChats")
	// }
	// if actor.VerifiedByAdmins != nil {
	// 	preloadFields = append(preloadFields, "VerifiedByAdmins")
	// }
	// if actor.Bans != nil {
	// 	preloadFields = append(preloadFields, "Bans")
	// }
	// if actor.TgAccounts != nil {
	// 	preloadFields = append(preloadFields, "TgAccounts")
	// }
	// if actor.MinecraftAccounts != nil {
	// 	preloadFields = append(preloadFields, "MinecraftAccounts")
	// }
	modelDb := authdb.db.Model(actor)
	for _, field := range preloadFields {
		modelDb = modelDb.Preload(field)
	}
	err := modelDb.First(actor).Error
	if err != nil {
		return errors.Wrapf(err, "fail to get actor %d", actor.ID)
	}
	return nil
}

func (authdb *AuthDbExecutor) GetActorChats(actor *Actor) error {
	authdb.logger.Debug("getting actor chats", zap.Uint("actor_id", uint(actor.ID)))
	err := authdb.db.Model(actor).Select("seen_in_chats").First(actor).Error
	if err != nil {
		return errors.Wrapf(err, "fail to get actor %d chats", actor.ID)
	}
	return nil
}

func (authdb *AuthDbExecutor) GetTgUser(user *TgUser) error {
	authdb.logger.Debug("getting tg user", zap.Uint("tg_user_id", uint(user.ID)))
	if user.ID == 0 {
		return errors.New("tg user id is not set")
	}
	err := authdb.db.First(&user).Error
	if err != nil {
		return errors.Wrapf(err, "fail to get tg user %d", user.ID)
	}
	return nil
}

type ErrorLoginTaken struct {
	Login mojang.MinecraftLogin
}

func (e ErrorLoginTaken) Error() string {
	return fmt.Sprintf("Login %s is already taken", e.Login)
}

func (e ErrorLoginTaken) Is(target error) bool {
	_, ok := target.(ErrorLoginTaken)
	return ok
}

func (authdb *AuthDbExecutor) OptionalGetMinecraftAccount(login mojang.MinecraftLogin) (*MinecraftAccount, error) {
	var minecraftAccounts []MinecraftAccount
	err := authdb.db.Find(&minecraftAccounts, login).Error
	if err != nil {
		return nil, errors.Wrapf(err, "fail to find minecraft account %s", login)
	}
	if len(minecraftAccounts) > 1 {
		return nil, errors.Errorf("found more than one minecraft account %s", login)
	}
	if len(minecraftAccounts) > 0 {
		acc := minecraftAccounts[0]
		return &acc, nil
	}
	return nil, nil
}

// adds minecraft login to actor. Each login must only belong to single actor.
func (authdb *AuthDbExecutor) AddMinecraftLogin(actorId ActorId, login mojang.MinecraftLogin, isOnline bool, playerId uuid.UUID) error {
	authdb.logger.Debug("adding minecraft login", zap.Uint("actor_id", uint(actorId)), zap.String("login", string(login)))
	minecraftAccount := MinecraftAccount{
		ID: login,
	}
	result := authdb.db.Model(&minecraftAccount).Find(&minecraftAccount)
	err := result.Error
	if err != nil {
		return errors.Wrapf(err, "fail to find minecraft account %s", login)
	}
	if result.RowsAffected == 0 {
		result = authdb.db.Create(&minecraftAccount)
		err = result.Error
		if err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				// race, somebody else has created this account
				return ErrorLoginTaken{
					Login: login,
				}
			}
			return errors.Wrapf(err, "fail to create minecraft account %s", login)
		}
	}

	minecraftAccount.ActorID = &actorId
	minecraftAccount.IsOnline = isOnline
	minecraftAccount.PlayerID = playerId.String()
	result = authdb.db.Where("actor_id IS NULL").Select("*").Updates(&minecraftAccount)
	err = result.Error
	if err != nil {
		return errors.Wrapf(err, "fail to create minecraft account %s for actor %d", login, actorId)
	}
	if result.RowsAffected == 0 {
		return ErrorLoginTaken{
			Login: login,
		}
	}
	return nil
}

func (authdb *AuthDbExecutor) RevokeMinecraftLogin(login mojang.MinecraftLogin) error {
	authdb.logger.Debug("removing minecraft login", zap.String("login", string(login)))
	account := &MinecraftAccount{ID: login}
	err := authdb.db.Save(account).Error
	if err != nil {
		return errors.Wrapf(err, "fail to revoke minecraft account %s", login)
	}
	return nil
}

func (authdb *AuthDbExecutor) SeenInChat(actorId ActorId, chatId TgChatId) error {
	authdb.logger.Debug("marking actor as seen in chat", zap.Uint("actor_id", uint(actorId)), zap.Uint("chat_id", uint(chatId)))
	actor := Actor{ID: actorId}
	authdb.db.Model(&actor).Association("SeenInChats").Append(&TgChat{ID: chatId})
	return nil
}

func (authdb *AuthDbExecutor) VerifiedByAdmin(actorId ActorId, adminId ActorId) error {
	authdb.logger.Debug("marking actor as verified by admin", zap.Uint("actor_id", uint(actorId)), zap.Uint("admin_id", uint(adminId)))
	actor := Actor{ID: actorId}
	err := authdb.GetActor(&actor)
	if err != nil {
		return errors.Wrap(err, "failed to get actor")
	}
	admin := Actor{ID: adminId}
	err = authdb.GetActor(&admin)
	if err != nil {
		return errors.Wrap(err, "failed to get admin")
	}
	err = authdb.db.Model(&actor).Association("VerifiedByAdmins").Append(&admin)
	if err != nil {
		return errors.Wrap(err, "failed to update actor")
	}
	return nil
}

func (authdb *AuthDbExecutor) GetActorByTgUser(tguser TgUserId, actor *Actor) error {
	authdb.logger.Debug("getting actor by tg user", zap.Uint("tg_user_id", uint(tguser)))
	tgAcc := &TgUser{ID: tguser}
	err := authdb.db.First(tgAcc).Error
	if err != nil {
		return errors.Wrapf(err, "fail to get actor by tg user %d", tguser)
	}
	actor.ID = tgAcc.ActorID
	err = authdb.GetActor(actor)
	if err != nil {
		return errors.Wrap(err, "failed to get actor")
	}
	return nil
}

func (authdb *AuthDbExecutor) SetAccept(actorId ActorId, accepted bool) error {
	authdb.logger.Debug("setting actor accept", zap.Uint("actor_id", uint(actorId)), zap.Bool("accepted", accepted))
	actor := Actor{ID: actorId}
	err := authdb.db.Model(&actor).Update("accepted", accepted).Error
	if err != nil {
		return errors.Wrapf(err, "fail to set actor accept %d", actorId)
	}
	return nil
}

func (authdb *AuthDbExecutor) SetAdmin(actorId ActorId, isAdmin bool) error {
	authdb.logger.Debug("setting actor admin", zap.Uint("actor_id", uint(actorId)), zap.Bool("admin", isAdmin))
	actor := Actor{ID: actorId}
	err := authdb.db.Model(&actor).Update("is_admin", isAdmin).Error
	if err != nil {
		return errors.Wrapf(err, "fail to set actor admin %d", actorId)
	}
	return nil
}

// func (authdb *AuthDbExecutor) GetActorIdsUpdatedSince(time time.Time) ([]ActorId, error) {
// 	authdb.logger.Debug("getting actor ids updated since", zap.Time("time", time))
// 	var actorIds []ActorId
// 	err := authdb.db.Model(&TgUser{}).Where("updated_at > ?", time).Distinct("actor_id").Pluck("actor_id", &actorIds).Error
// 	if err != nil {
// 		return nil, errors.Wrap(err, "fail to get actor ids")
// 	}
// 	return actorIds, nil
// }

func (authdb *AuthDbExecutor) GetAcceptedActorsWithAccounts() ([]Actor, error) {
	// authdb.logger.Debug("getting accepted actors with accounts")
	var actors []Actor
	err := authdb.db.Where(&Actor{Accepted: true}).Preload("MinecraftAccounts").Find(&actors).Error
	if err != nil {
		return nil, errors.Wrap(err, "fail to get accepted actors")
	}
	return actors, nil
}

func (authdb *AuthDbExecutor) SetWhitelist(logins []mcserver.MinecraftAccountSpec) error {
	// authdb.logger.Debug("setting logins", zap.Any("logins", logins))
	body, err := json.Marshal(logins)
	if err != nil {
		return errors.Wrap(err, "fail to marshal logins")
	}
	client := http.Client{}
	req, err := http.NewRequest("POST", authdb.config.ServerOverseerUrl+"/set-whitelist", bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "fail to create request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "fail to send request")
	}
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("bad response status %d", resp.StatusCode)
	}
	return nil
}

func (authdb *AuthDbExecutor) SetPassword(login mojang.MinecraftLogin, password string) error {
	authdb.logger.Debug("setting password", zap.String("login", string(login)))
	body, err := json.Marshal(map[string]string{string(login): password})
	if err != nil {
		return errors.Wrap(err, "fail to marshal password")
	}
	client := http.Client{}
	req, err := http.NewRequest("POST", authdb.config.ServerOverseerUrl+"/set-passwords", bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "fail to create request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrap(err, "fail to send request")
	}
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("bad response status %d", resp.StatusCode)
	}
	return nil
}
