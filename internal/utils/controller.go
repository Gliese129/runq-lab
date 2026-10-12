package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ControllerID persists independently of target configuration and the DB.
func ControllerID(dataDir string) (string, error) {
	file := filepath.Join(dataDir, "controller-id")
	if data, err := os.ReadFile(file); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) != 12 {
			return "", fmt.Errorf("invalid controller ID in %s", file)
		}
		if _, err = hex.DecodeString(id); err != nil {
			return "", err
		}
		return id, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	var data [6]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(data[:])
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return ControllerID(dataDir)
	}
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(id + "\n")
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	return id, err
}
func RemoteSessionDir(controller, target string) string {
	sum := sha256.Sum256([]byte(target))
	return path.Join(".runq", "f", controller, hex.EncodeToString(sum[:4]))
}
func RemoteSocketPath(controller, target string) string {
	return path.Join(RemoteSessionDir(controller, target), "rq.sock")
}
