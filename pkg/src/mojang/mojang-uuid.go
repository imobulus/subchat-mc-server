package mojang

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type MinecraftLogin string

var minecraftLoginRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]{3,16}$`)

type InvalidMinecraftLoginErr struct {
	Login string
}

func (e InvalidMinecraftLoginErr) Error() string {
	return fmt.Sprintf("Invalid minecraft login: %s, must match %s", e.Login, minecraftLoginRegexp.String())
}

func (e InvalidMinecraftLoginErr) Is(target error) bool {
	_, ok := target.(InvalidMinecraftLoginErr)
	return ok
}

func MakeMinecraftLogin(s string) (MinecraftLogin, error) {
	if !minecraftLoginRegexp.Match([]byte(s)) {
		return "", InvalidMinecraftLoginErr{s}
	}
	return MinecraftLogin(s), nil
}

type NoSuchPlayerErr struct {
	Login MinecraftLogin
}

func (e NoSuchPlayerErr) Error() string {
	return fmt.Sprintf("No such player: %s", e.Login)
}

func (e NoSuchPlayerErr) Is(target error) bool {
	_, ok := target.(NoSuchPlayerErr)
	return ok
}

func QueryOnlineUuid(login MinecraftLogin, ctx context.Context) (uuid.UUID, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://api.mojang.com/users/profiles/minecraft/%s", login), nil)
	if err != nil {
		return uuid.UUID{}, errors.Wrap(err, "failed to create request")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return uuid.UUID{}, errors.Wrap(err, "failed to perform request")
	}
	if response.StatusCode != 200 {
		return uuid.UUID{}, errors.Errorf("Mojang API returned %d", response.StatusCode)
	}
	var result map[string]string
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return uuid.UUID{}, errors.Wrap(err, "failed to read response body")
	}
	err = json.Unmarshal(bodyBytes, &result)
	if err != nil {
		return uuid.UUID{}, errors.Wrap(err, "failed to parse response body")
	}
	playerId, ok := result["id"]
	if !ok {
		errorMsg, ok := result["errorMessage"]
		if !ok {
			return uuid.UUID{}, errors.Errorf("Mojang API returned unexpected response: %v", result)
		}
		if errorMsg == fmt.Sprintf("Couldn't find any profile with name %s", login) {
			return uuid.UUID{}, NoSuchPlayerErr{login}
		}
		return uuid.UUID{}, errors.Errorf("Mojang API returned error: %s", errorMsg)
	}
	uuidBytes, err := hex.DecodeString(playerId)
	if err != nil {
		return uuid.UUID{}, errors.Wrap(err, "failed to decode player ID")
	}
	id, err := uuid.FromBytes(uuidBytes)
	if err != nil {
		return uuid.UUID{}, errors.Wrap(err, "failed to parse player ID")
	}
	return id, nil
}

func GetOfflineUuid(login MinecraftLogin) uuid.UUID {
	stringToHash := "OfflinePlayer:" + string(login)
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
