package authdb

import (
	"database/sql/driver"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/imobulus/subchat-mc-server/src/mojang"
	"gorm.io/gorm"
)

type ActorId uint
type TgUserId int64
type TgChatId int64

// Scan implements the Scanner interface for TgChatId
func (t *TgChatId) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case int64:
		*t = TgChatId(v)
	default:
		return fmt.Errorf("cannot scan type %T into TgChatId", value)
	}
	return nil
}

// Value implements the driver Valuer interface for TgChatId
func (t TgChatId) Value() (driver.Value, error) {
	return int64(t), nil
}

type Actor struct {
	ID                ActorId `gorm:"primarykey"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         gorm.DeletedAt `gorm:"index"`
	Nickname          string
	Description       string
	IsAdmin           bool
	Accepted          bool
	AcceptedLastTime  time.Time
	SeenInChats       []TgChat `gorm:"many2many:actors_seen_in_chats"`
	VerifiedByAdmins  []*Actor `gorm:"many2many:actors_verified_by_admins"`
	Bans              []Ban
	TgAccounts        []TgUser
	MinecraftAccounts []MinecraftAccount

	CustomMinecraftLoginLimit *int
}

type TgChat struct {
	ID TgChatId
}

type ActorSeenInChats struct {
	TgChatID  TgChatId `gorm:"primarykey"`
	ActorID   uint     `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// table bans
type Ban struct {
	gorm.Model
	ActorID     ActorId
	BanDuration time.Duration
	Reason      string
}

type TgUser struct {
	ID           TgUserId `gorm:"primarykey"` // tg user id
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
	LastSeenInfo tgbotapi.User  `gorm:"serializer:json"`
	ActorID      ActorId
}

func ShortDescribeTgUser(u tgbotapi.User) string {
	return fmt.Sprintf("ID %d %s", u.ID, u.String())
}

type MinecraftAccount struct {
	ID        mojang.MinecraftLogin `gorm:"primarykey"` // minecraft id
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	ActorID   *ActorId
	IsOnline  bool
	PlayerID  string
}

// used for AutoMigrate
var allSchemas = []interface{}{
	&Actor{},
	&TgUser{},
	&Ban{},
	&MinecraftAccount{},
}
