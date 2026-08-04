package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const profilePrefix = "lichen-profile-v1."

type profileBundle struct {
	Version  int             `json:"version"`
	Identity json.RawMessage `json:"identity"`
	Config   Config          `json:"config"`
}

func (a *App) ExportProfile(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must contain at least 8 characters")
	}
	identityRaw, err := a.identity.MarshalJSON()
	if err != nil {
		return "", err
	}
	bundle := profileBundle{Version: 1, Identity: identityRaw, Config: a.store.Snapshot()}
	plain, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt, 200000, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, plain, []byte(profilePrefix))
	payload := append(append(salt, nonce...), sealed...)
	a.events.Add("info", "backup", "Зашифрованная резервная копия профиля создана", "")
	return profilePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (a *App) ImportProfile(token, password string) error {
	if len(password) < 8 {
		return errors.New("password must contain at least 8 characters")
	}
	if len(token) <= len(profilePrefix) || token[:len(profilePrefix)] != profilePrefix {
		return errors.New("invalid Lichen profile")
	}
	payload, err := base64.RawURLEncoding.DecodeString(token[len(profilePrefix):])
	if err != nil || len(payload) < 28 {
		return errors.New("invalid Lichen profile")
	}
	salt, nonce, sealed := payload[:16], payload[16:28], payload[28:]
	key := pbkdf2SHA256([]byte(password), salt, 200000, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	plain, err := gcm.Open(nil, nonce, sealed, []byte(profilePrefix))
	if err != nil {
		return errors.New("wrong password or damaged profile")
	}
	var bundle profileBundle
	if err := json.Unmarshal(plain, &bundle); err != nil || bundle.Version != 1 {
		return errors.New("unsupported profile version")
	}
	identity, err := identityFromJSON(bundle.Identity)
	if err != nil {
		return fmt.Errorf("profile identity: %w", err)
	}
	if identity.ID() == "" {
		return errors.New("invalid imported identity")
	}
	if len(bundle.Config.Peers) > 10000 {
		return errors.New("profile contains too many peers")
	}
	normalizeConfig(&bundle.Config)

	identityPath, configPath := a.identityPath, a.store.Path()
	if err := os.MkdirAll(a.dataDir, 0o700); err != nil {
		return err
	}
	identityTmp, configTmp := identityPath+".import", configPath+".import"
	cfgRaw, _ := json.MarshalIndent(bundle.Config, "", "  ")
	if err := os.WriteFile(identityTmp, bundle.Identity, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(configTmp, cfgRaw, 0o600); err != nil {
		_ = os.Remove(identityTmp)
		return err
	}
	if err := replacePairAtomically(identityTmp, identityPath, configTmp, configPath); err != nil {
		return err
	}
	a.events.Add("warning", "backup", "Профиль импортирован; требуется перезапуск Lichen", "")
	return nil
}

func replacePairAtomically(firstTmp, firstPath, secondTmp, secondPath string) error {
	firstBackup, secondBackup := firstPath+".before-import", secondPath+".before-import"
	_ = os.Remove(firstBackup)
	_ = os.Remove(secondBackup)
	firstExisted, secondExisted := true, true
	if err := os.Rename(firstPath, firstBackup); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		firstExisted = false
	}
	if err := os.Rename(secondPath, secondBackup); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			if firstExisted {
				_ = os.Rename(firstBackup, firstPath)
			}
			return err
		}
		secondExisted = false
	}
	rollback := func() {
		_ = os.Remove(firstPath)
		_ = os.Remove(secondPath)
		if firstExisted {
			_ = os.Rename(firstBackup, firstPath)
		}
		if secondExisted {
			_ = os.Rename(secondBackup, secondPath)
		}
	}
	if err := os.Rename(firstTmp, firstPath); err != nil {
		rollback()
		return err
	}
	if err := os.Rename(secondTmp, secondPath); err != nil {
		rollback()
		return err
	}
	_ = os.Remove(firstBackup)
	_ = os.Remove(secondBackup)
	return nil
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	hLen := 32
	blocks := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	for blockIndex := 1; blockIndex <= blocks; blockIndex++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		var index [4]byte
		binary.BigEndian.PutUint32(index[:], uint32(blockIndex))
		mac.Write(index[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
