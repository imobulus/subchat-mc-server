package tgtypes

import (
	"encoding/json"
	"net/url"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/pkg/errors"
)

const PrivateChatType string = "private"
const GroupChatType string = "group"
const SupergroupChatType string = "supergroup"

const methodSetMyCommands string = "setMyCommands"

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type scopeType struct {
	Type string `json:"type"`
}

type scopeTypeChat struct {
	Type   string `json:"type"`
	ChatID int64  `json:"chat_id"`
}

type scopeTypeChatUser struct {
	Type   string `json:"type"`
	ChatID int64  `json:"chat_id"`
	UserID int64  `json:"user_id"`
}

type BotCommandScopeDefault scopeType

func NewBotCommandScopeDefault() *BotCommandScopeDefault {
	return &BotCommandScopeDefault{Type: "default"}
}

type BotCommandScopeAllPrivateChats scopeType

func NewBotCommandScopeAllPrivateChats() *BotCommandScopeAllPrivateChats {
	return &BotCommandScopeAllPrivateChats{Type: "all_private_chats"}
}

type BotCommandScopeAllGroupChats scopeType

func NewBotCommandScopeAllGroupChats() *BotCommandScopeAllGroupChats {
	return &BotCommandScopeAllGroupChats{Type: "all_group_chats"}
}

type BotCommandScopeAllChatAdministrators scopeType

func NewBotCommandScopeAllChatAdministrators() *BotCommandScopeAllChatAdministrators {
	return &BotCommandScopeAllChatAdministrators{Type: "all_chat_administrators"}
}

type BotCommandScopeChat scopeTypeChat

func NewBotCommandScopeChat(chatID int64) *BotCommandScopeChat {
	return &BotCommandScopeChat{Type: "chat", ChatID: chatID}
}

type BotCommandScopeChatAdministrators scopeTypeChat

func NewBotCommandScopeChatAdministrators(chatID int64) *BotCommandScopeChatAdministrators {
	return &BotCommandScopeChatAdministrators{Type: "chat_administrators", ChatID: chatID}
}

type BotCommandScopeChatMember scopeTypeChatUser

func NewBotCommandScopeChatMember(chatID, userID int64) *BotCommandScopeChatMember {
	return &BotCommandScopeChatMember{Type: "chat_member", ChatID: chatID, UserID: userID}
}

type BotCommandScope struct {
	Default               *BotCommandScopeDefault
	AllPrivateChats       *BotCommandScopeAllPrivateChats
	AllGroupChats         *BotCommandScopeAllGroupChats
	AllChatAdministrators *BotCommandScopeAllChatAdministrators
	Chat                  *BotCommandScopeChat
	ChatAdministrators    *BotCommandScopeChatAdministrators
	ChatMember            *BotCommandScopeChatMember
}

func (scope *BotCommandScope) UnmarshalJSON(data []byte) error {
	var scopeType scopeType
	if err := json.Unmarshal(data, &scopeType); err != nil {
		return err
	}
	var toUnmarshal interface{}
	switch scopeType.Type {
	case "default":
		scope.Default = &BotCommandScopeDefault{}
		toUnmarshal = scope.Default
	case "all_private_chats":
		scope.AllPrivateChats = &BotCommandScopeAllPrivateChats{}
		toUnmarshal = scope.AllPrivateChats
	case "all_group_chats":
		scope.AllGroupChats = &BotCommandScopeAllGroupChats{}
		toUnmarshal = scope.AllGroupChats
	case "all_chat_administrators":
		scope.AllChatAdministrators = &BotCommandScopeAllChatAdministrators{}
		toUnmarshal = scope.AllChatAdministrators
	case "chat":
		scope.Chat = &BotCommandScopeChat{}
		toUnmarshal = scope.Chat
	case "chat_administrators":
		scope.ChatAdministrators = &BotCommandScopeChatAdministrators{}
		toUnmarshal = scope.ChatAdministrators
	case "chat_member":
		scope.ChatMember = &BotCommandScopeChatMember{}
		toUnmarshal = scope.ChatMember
	default:
		return errors.New("Unknown BotCommandScope type " + scopeType.Type)
	}
	return json.Unmarshal(data, toUnmarshal)
}

func (scope *BotCommandScope) MarshalJSON() ([]byte, error) {
	if scope.Default != nil {
		return json.Marshal(scope.Default)
	}
	if scope.AllPrivateChats != nil {
		return json.Marshal(scope.AllPrivateChats)
	}
	if scope.AllGroupChats != nil {
		return json.Marshal(scope.AllGroupChats)
	}
	if scope.AllChatAdministrators != nil {
		return json.Marshal(scope.AllChatAdministrators)
	}
	if scope.Chat != nil {
		return json.Marshal(scope.Chat)
	}
	if scope.ChatAdministrators != nil {
		return json.Marshal(scope.ChatAdministrators)
	}
	if scope.ChatMember != nil {
		return json.Marshal(scope.ChatMember)
	}
	return nil, errors.New("BotCommandScope is empty")
}

func SetMyCommands(api *tgbotapi.BotAPI, commands []BotCommand, scope *BotCommandScope, languageCode *string) error {
	v := url.Values{}
	commandsJSON, err := json.Marshal(commands)
	if err != nil {
		return errors.Wrap(err, "failed to marshal commands")
	}
	v.Add("commands", string(commandsJSON))
	if scope != nil {
		scopeJSON, err := json.Marshal(scope)
		if err != nil {
			return errors.Wrap(err, "failed to marshal scope")
		}
		v.Add("scope", string(scopeJSON))
	}
	if languageCode != nil {
		v.Add("language_code", *languageCode)
	}
	_, err = api.MakeRequest(methodSetMyCommands, v)
	return err
}

func DeleteMyCommands(api *tgbotapi.BotAPI, scope *BotCommandScope, languageCode *string) error {
	v := url.Values{}
	if scope != nil {
		scopeJSON, err := json.Marshal(scope)
		if err != nil {
			return errors.Wrap(err, "failed to marshal scope")
		}
		v.Add("scope", string(scopeJSON))
	}
	if languageCode != nil {
		v.Add("language_code", *languageCode)
	}
	_, err := api.MakeRequest(methodSetMyCommands, v)
	return err
}
