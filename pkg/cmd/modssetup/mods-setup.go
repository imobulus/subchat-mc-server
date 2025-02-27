package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
)

type ModDescription struct {
	Url         string `json:"url"`
	File        string `json:"file"`
	AddToClient bool   `json:"add_to_client"`
	NoServer    bool   `json:"no_server"`
}

func runCmd(cmdArgs ...string) {
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
}

func hashFile(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	sha1Hash := sha1.New()
	_, err = sha1Hash.Write(content)
	if err != nil {
		return nil, err
	}
	return sha1Hash.Sum(nil), nil
}

func downloadMod(modUrl string, cachePath string) (string, error) {
	fmt.Println("Downloading mod: " + modUrl)
	// download file to cachePath/tmpmod
	req, err := http.NewRequest("GET", modUrl, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("failed to download mod: %s", resp.Status)
	}
	tmpPath := path.Join(cachePath, "tmpmod")
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()
	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return "", err
	}
	// hash file
	hash, err := hashFile(tmpPath)
	if err != nil {
		return "", err
	}
	hashString := hex.EncodeToString(hash)
	// move file to cachePath/hash
	hashPath := path.Join(cachePath, hashString)
	err = os.Rename(tmpPath, hashPath)
	if err != nil {
		return "", err
	}
	return hashString, nil
}

func loadHashedFile(modUrl string, urlToHash map[string]string, cachePath string) (string, error) {
	doDownload := false
	hash, ok := urlToHash[modUrl]
	if !ok {
		doDownload = true
	} else {
		if _, err := os.Stat(path.Join(cachePath, hash)); err != nil {
			doDownload = true
		}
	}
	var err error
	if doDownload {
		hash, err = downloadMod(modUrl, cachePath)
		if err != nil {
			return "", err
		}
		urlToHash[modUrl] = hash
	} else {
		fmt.Println("Using cached mod: " + path.Join(cachePath, hash))
	}
	return path.Join(cachePath, hash), nil
}

func main() {
	modsJson := flag.String("mods-json", "mods.json", "Path to mods.json file")
	modsPath := flag.String("mods-dir", "mods", "Path to mods directory")
	clientModsPath := flag.String("client-dir", "clientmods", "Path to client directory")
	cachePath := flag.String("cache-dir", "cache", "Path to cache directory")
	flag.Parse()
	clientModsModsPath := path.Join(*clientModsPath, "mods")
	modsContents, err := os.ReadFile(*modsJson)
	if err != nil {
		panic(err)
	}
	var mods []ModDescription
	err = json.Unmarshal(modsContents, &mods)
	if err != nil {
		panic(err)
	}
	urlToHashMap := make(map[string]string)
	mapPath := path.Join(*cachePath, "urlToHash.json")
	func() {
		success := false
		defer func() {
			if !success {
				os.Remove(mapPath)
				urlToHashMap = make(map[string]string)
			}
		}()
		if _, err := os.Stat(mapPath); err == nil {
			mapContents, err := os.ReadFile(mapPath)
			if err != nil {
				fmt.Println(err.Error())
				return
			}
			err = json.Unmarshal(mapContents, &urlToHashMap)
			if err != nil {
				fmt.Println(err.Error())
				return
			}
		}
	}()
	for _, mod := range mods {
		modHashedFilePath, err := loadHashedFile(mod.Url, urlToHashMap, *cachePath)
		if err != nil {
			panic(err)
		}
		if !mod.NoServer {
			modPath := path.Join(*modsPath, mod.File)
			fmt.Println("Copying mod to: " + modPath)
			runCmd("cp", modHashedFilePath, modPath)
		}
		if mod.AddToClient {
			clientModPath := path.Join(clientModsModsPath, mod.File)
			fmt.Println("Copying mod to: " + clientModPath)
			runCmd("cp", modHashedFilePath, clientModPath)
		}
	}
	urlToHashJson, err := json.Marshal(urlToHashMap)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(mapPath, urlToHashJson, 0644)
	if err != nil {
		panic(err)
	}
	// zip all client mods
	runCmd("zip", "-r", path.Join(*clientModsPath, "mods.zip"), clientModsModsPath)
}
