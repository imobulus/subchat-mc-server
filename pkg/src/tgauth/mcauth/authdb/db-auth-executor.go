package authdb

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type AuthDbExecutor struct {
	db     *gorm.DB
	logger *zap.Logger
}

func NewAuthDbExecutor(db *gorm.DB) *AuthDbExecutor {
	return &AuthDbExecutor{db: db}
}

func (authdb *AuthDbExecutor) InitDB() error {
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
	// find if user exists
	var user TgUser
	err := authdb.db.First(&user, tguser.ID).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Wrapf(err, "fail to find tg user %s", ShortDescribeTgUser(tguser))
		}
		err := authdb.createTgUser(tguser)
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

func (authdb *AuthDbExecutor) createTgUser(tguser tgbotapi.User) error {
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
	ban := Ban{
		ActorID:     actorId,
		BanDuration: duration,
		Reason:      reason,
	}
	authdb.db.Create(&ban)
}

func (authdb *AuthDbExecutor) UnbanActor(actorId uint) {
	authdb.db.Where("actor_id = ?", actorId).Delete(&Ban{})
}

// populates association fields which are non-nil
func (authdb *AuthDbExecutor) GetActor(actor *Actor) error {
	if actor.ID == 0 {
		return errors.New("actor id is not set")
	}
	preloadFields := []string{}
	if actor.SeenInChats != nil {
		preloadFields = append(preloadFields, "SeenInChats")
	}
	if actor.VerifiedByAdmins != nil {
		preloadFields = append(preloadFields, "VerifiedByAdmins")
	}
	if actor.Bans != nil {
		preloadFields = append(preloadFields, "Bans")
	}
	if actor.TgAccounts != nil {
		preloadFields = append(preloadFields, "TgAccounts")
	}
	if actor.MinecraftAccounts != nil {
		preloadFields = append(preloadFields, "MinecraftAccounts")
	}
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
	err := authdb.db.Model(actor).Select("seen_in_chats").First(actor).Error
	if err != nil {
		return errors.Wrapf(err, "fail to get actor %d chats", actor.ID)
	}
	return nil
}

func (authdb *AuthDbExecutor) GetTgUser(user *TgUser) error {
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
	Login MinecraftLogin
}

func (e ErrorLoginTaken) Error() string {
	return fmt.Sprintf("Login %s is already taken", e.Login)
}

// adds minecraft login to actor. Each login must only belong to single actor.
func (authdb *AuthDbExecutor) AddMinecraftLogin(actorId ActorId, login MinecraftLogin) error {
	minecraftAccount := MinecraftAccount{
		ID:      login,
		ActorID: actorId,
	}
	err := authdb.db.Create(&minecraftAccount).Error
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrorLoginTaken{login}
		}
		return errors.Wrapf(err, "fail to create minecraft account %s for actor %d", login, actorId)
	}
	return nil
}

func (authdb *AuthDbExecutor) RemoveMinecraftAccount(login MinecraftLogin) error {
	account := &MinecraftAccount{ID: login}
	err := authdb.db.Delete(account).Error
	if err != nil {
		return errors.Wrapf(err, "fail to remove minecraft account %s", login)
	}
	return nil
}

func (authdb *AuthDbExecutor) SeenInChat(actorId ActorId, chatId TgChatId) error {
	actor := Actor{ID: actorId}
	authdb.db.Model(&actor).Association("SeenInChats").Append(&TgChat{ID: chatId})
	return nil
}

func (authdb *AuthDbExecutor) VerifiedByAdmin(actorId ActorId, adminId ActorId) error {
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
	tgAcc := &TgUser{ID: tguser}
	err := authdb.db.First(tgAcc).Error
	if err != nil {
		return errors.Wrapf(err, "fail to get actor by tg user %d", tguser)
	}
	actor.ID = tgAcc.ActorID
	err = authdb.db.First(actor).Error
	if err != nil {
		return errors.Wrap(err, "failed to get actor")
	}
	return nil
}
