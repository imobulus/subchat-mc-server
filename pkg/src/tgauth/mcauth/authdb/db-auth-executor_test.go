package authdb

import (
	"os"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDbInteractions(t *testing.T) {
	os.Remove("test.db")
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	executor := NewAuthDbExecutor(db)
	err = executor.InitDB()
	if err != nil {
		t.Fatalf("Failed to init db: %v", err)
	}
	err = executor.UpdateTgUserInfo(tgbotapi.User{
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
