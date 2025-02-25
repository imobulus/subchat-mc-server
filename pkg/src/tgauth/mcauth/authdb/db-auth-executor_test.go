package authdb

import (
	"os"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func initDb(t *testing.T) *gorm.DB {
	os.Remove("test.db")
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	return db
}

func initExecutor(t *testing.T) *AuthDbExecutor {
	db := initDb(t)
	logger := zap.Must(zap.NewDevelopment())
	executor, err := NewAuthDbExecutor(db, DefaultAuthDbExecutorConfig, logger)
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	return executor
}

func TestDbInteractions(t *testing.T) {
	executor := initExecutor(t)
	err := executor.UpdateTgUserInfo(tgbotapi.User{
		ID:        1,
		FirstName: "Test",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("Failed to update user info: %v", err)
	}
	user := TgUser{ID: 1}
	err = executor.GetTgUser(&user)
	if err != nil {
		t.Fatalf("Failed to find user: %v", err)
	}
	if user.LastSeenInfo.FirstName != "Test" || user.LastSeenInfo.LastName != "User" {
		t.Fatalf("Wrong user info: %v", user)
	}
	actor := Actor{ID: user.ActorID, TgAccounts: []TgUser{}}
	err = executor.GetActor(&actor)
	if err != nil {
		t.Fatalf("Failed to find actor: %v", err)
	}
	if actor.Nickname != "" {
		t.Fatalf("Wrong actor info: %v", actor)
	}
	if actor.IsAdmin {
		t.Fatalf("Wrong actor info: %v", actor)
	}
	if len(actor.TgAccounts) != 1 {
		t.Fatalf("Wrong actor info: %v", actor)
	}
	if actor.TgAccounts[0].ID != 1 {
		t.Fatalf("Wrong actor info: %v", actor)
	}
	err = executor.SeenInChat(actor.ID, 1)
	if err != nil {
		t.Fatalf("Failed to update seen in chat: %v", err)
	}
	err = executor.SeenInChat(actor.ID, 1)
	if err != nil {
		t.Fatalf("Failed to update seen in chat: %v", err)
	}
}

func TestDoubleSave(t *testing.T) {
	executor := initExecutor(t)
	err := executor.AddMinecraftLogin(1, "test", true)
	if err != nil {
		t.Fatalf("Failed to add minecraft login: %v", err)
	}
	err = executor.AddMinecraftLogin(2, "test", true)
	if !errors.Is(err, ErrorLoginTaken{}) {
		t.Fatalf("Double save should fail")
	}
}
