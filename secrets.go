package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/zalando/go-keyring"
)

const keyringService = "kamal-tui-secrets"

func getProjectID() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "kamal-tui"
	}
	sum := sha256.Sum256([]byte(cwd))
	return filepath.Base(cwd) + "-" + hex.EncodeToString(sum[:8])
}

func loadSecrets() map[string]string {
	data, err := keyring.Get(keyringService, getProjectID())
	if err != nil {
		return make(map[string]string)
	}

	var secrets map[string]string
	if err := json.Unmarshal([]byte(data), &secrets); err != nil {
		return make(map[string]string)
	}
	return secrets
}

func saveSecrets(secrets map[string]string) error {
	data, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, getProjectID(), string(data))
}

func addSecret(key, value string) error {
	secrets := loadSecrets()
	secrets[key] = value
	return saveSecrets(secrets)
}

func removeSecret(key string) error {
	secrets := loadSecrets()
	delete(secrets, key)
	return saveSecrets(secrets)
}

func getSecretKeys() []string {
	secrets := loadSecrets()
	var keys []string
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
