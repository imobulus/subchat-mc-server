package authdb

import (
	"fmt"
	"regexp"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"gorm.io/gorm"
)

type TgUserId int64
type TgChatlId int64
type MinecraftLogin string

var minecraftLoginRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]{3,16}$`)

type InvalidMinecraftLoginErr struct {
	Login string
}

func (e InvalidMinecraftLoginErr) Error() string {
	return fmt.Sprintf("Invalid minecraft login: %s, must match %s", e.Login, minecraftLoginRegexp.String())
}

func MakeMinecraftLogin(s string) (MinecraftLogin, error) {
	if !minecraftLoginRegexp.Match([]byte(s)) {
		return "", InvalidMinecraftLoginErr{s}
	}
	return MinecraftLogin(s), nil
}

type Actor struct {
	gorm.Model
	Nickname          string
	Description       string
	IsAdmin           bool
	SeenInChats       []TgChat `gorm:"many2many:actors_seen_in_chats"`
	VerifiedByAdmins  []*Actor `gorm:"many2many:actors_verified_by_admins"`
	Bans              []Ban
	TgAccounts        []TgUser
	MinecraftAccounts []MinecraftAccount

	CustomMinecraftLoginLimit *int
}

// table bans
type Ban struct {
	gorm.Model
	ActorID     uint
	BanDuration time.Duration
	Reason      string
}

type TgUser struct {
	ID           TgUserId `gorm:"primarykey"` // tg user id
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
	LastSeenInfo tgbotapi.User  `gorm:"serializer:json"`
	ActorID      uint
}

func ShortDescribeTgUser(u tgbotapi.User) string {
	return fmt.Sprintf("ID %d %s", u.ID, u.String())
}

type TgChat struct {
	ID               TgChatlId `gorm:"primarykey"` // tg chat id
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt `gorm:"index"`
	LastSeenChatInfo tgbotapi.Chat  `gorm:"serializer:json"`
}

type MinecraftAccount struct {
	ID        MinecraftLogin `gorm:"primarykey"` // minecraft id
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	ActorID   uint
}

// used for AutoMigrate
var allSchemas = []interface{}{
	&Actor{},
	&TgUser{},
	&TgChat{},
	&Ban{},
}
