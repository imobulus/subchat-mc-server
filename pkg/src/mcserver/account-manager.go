package mcserver

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func getOfflineUuid(playerName string) uuid.UUID {
	stringToHash := "OfflinePlayer:" + playerName
	// get md5 hash of stringToHash
	hash := md5.Sum([]byte(stringToHash))
	hash[6] = hash[6]&0x0f | 0x30
	hash[8] = hash[8]&0x3f | 0x80
	uid, err := uuid.FromBytes(hash[:])
	if err != nil {
		panic("cannot create offline uuid")
	}
	return uid
}

type AccountManager struct {
	whitelistPath  string
	checkFrequency time.Duration
	execFunc       func(string) error

	// nil means not set
	neededAccounts        map[string]struct{}
	accountPasswordsToSet map[string]string

	allAccountsRequests     chan []string
	accountPasswordRequests chan map[string]string

	logger *zap.Logger
	ctx    context.Context
}

func NewAccountManager(
	whitelistPath string,
	checkFrequency time.Duration,
	execFunc func(string) error,
	logger *zap.Logger) *AccountManager {
	return &AccountManager{
		whitelistPath:           whitelistPath,
		checkFrequency:          checkFrequency,
		execFunc:                execFunc,
		neededAccounts:          nil,
		accountPasswordsToSet:   make(map[string]string),
		allAccountsRequests:     make(chan []string),
		accountPasswordRequests: make(chan map[string]string),
		logger:                  logger,
	}
}

func (manager *AccountManager) runAccountManager(ctx context.Context) {
	manager.ctx = ctx
	go manager.accountManagerLoop()
}

func (manager *AccountManager) accountManagerLoop() {
	tk := time.NewTicker(manager.checkFrequency)
	defer tk.Stop()
	for {
		select {
		case <-manager.ctx.Done():
			return
		case newAccounts := <-manager.allAccountsRequests:
			manager.neededAccounts = make(map[string]struct{}, len(manager.neededAccounts))
			for _, account := range newAccounts {
				manager.neededAccounts[account] = struct{}{}
			}
		case newAccountPasswords := <-manager.accountPasswordRequests:
			for k, v := range newAccountPasswords {
				manager.accountPasswordsToSet[k] = v
			}
		case <-tk.C:
			manager.updateAccountState()
		}
	}
}

func (manager *AccountManager) updateAccountState() {
	if manager.neededAccounts == nil {
		manager.logger.Debug("no accounts needed set")
		return
	}
	manager.setPasswords()
	manager.checkAccounts()
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func (manager *AccountManager) setPasswords() {
	for account, password := range manager.accountPasswordsToSet {
		accountUuid := getOfflineUuid(account).String()
		if _, ok := manager.neededAccounts[account]; !ok {
			// ignore setpassword for not needed account
			continue
		}
		err := manager.execFunc(fmt.Sprintf("/auth remove %s", accountUuid))
		if err != nil {
			manager.logger.Error("cannot remove user", zap.Error(err))
			continue
		}
		err = manager.execFunc(fmt.Sprintf("/auth register %s %s", accountUuid, password))
		if err != nil {
			manager.logger.Error("cannot set password", zap.Error(err))
			continue
		}
		delete(manager.accountPasswordsToSet, account)
	}
}

type whitelistEntry struct {
	Name string `json:"name"`
	Uuid string `json:"uuid"`
}

func sortWhitelist(whitelist []whitelistEntry) {
	sort.Slice(whitelist, func(i, j int) bool {
		if whitelist[i].Name != whitelist[j].Name {
			return whitelist[i].Name < whitelist[j].Name
		}
		return whitelist[i].Uuid < whitelist[j].Uuid
	})
}

func (manager *AccountManager) checkAccounts() {
	whitelist := make([]whitelistEntry, 0, len(manager.neededAccounts))
	for account := range manager.neededAccounts {
		whitelist = append(whitelist, whitelistEntry{
			Name: account,
			Uuid: getOfflineUuid(account).String(),
		})
	}
	sortWhitelist(whitelist)
	currentContent, err := os.ReadFile(manager.whitelistPath)
	if err != nil {
		manager.logger.Error("cannot read whitelist", zap.Error(err))
		return
	}
	var currentWhitelist []whitelistEntry
	err = json.Unmarshal(currentContent, &currentWhitelist)
	if err != nil {
		manager.logger.Error("cannot unmarshal whitelist", zap.Error(err))
	} else {
		sortWhitelist(currentWhitelist)
		if len(currentWhitelist) == len(whitelist) {
			equal := true
			for i := range currentWhitelist {
				if currentWhitelist[i] != whitelist[i] {
					equal = false
					break
				}
			}
			if equal {
				return
			}
		}
	}
	// write whitelist to file
	whitelistContent, err := json.Marshal(whitelist)
	if err != nil {
		manager.logger.Error("cannot marshal whitelist", zap.Error(err))
		return
	}
	err = os.WriteFile(manager.whitelistPath, whitelistContent, 0664)
	if err != nil {
		manager.logger.Error("cannot write whitelist", zap.Error(err))
		return
	}
	err = manager.execFunc("/whitelist reload")
	if err != nil {
		manager.logger.Error("cannot reload whitelist", zap.Error(err))
	}
}

func (manager *AccountManager) SetNeededAccounts(accounts []string) error {
	select {
	case manager.allAccountsRequests <- accounts:
		return nil
	case <-manager.ctx.Done():
		return manager.ctx.Err()
	}
}

func (manager *AccountManager) SetAccountPasswords(accountPasswords map[string]string) error {
	select {
	case manager.accountPasswordRequests <- accountPasswords:
		return nil
	case <-manager.ctx.Done():
		return manager.ctx.Err()
	}
}
