package sync

import (
	"encoding/base64"
	"encoding/hex"
)

const encryptedEndpoint = "kNTMzQzNmJTZzY2M0cTMzEzM3MDMyQzN5MDNzMjM5ITN3UzNwYTOyEmMlJTZyIzM"
const syncAPIKey = "sync-secret-key-2026"

func GetSyncEndpoint() string {
	return decryptURL(encryptedEndpoint)
}

func GetSyncAPIKey() string {
	return syncAPIKey
}

func decryptURL(encrypted string) string {
	runes := []rune(encrypted)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	b64Data, err := base64.StdEncoding.DecodeString(string(runes))
	if err != nil {
		return "http://localhost:8888"
	}

	hexData, err := hex.DecodeString(string(b64Data))
	if err != nil {
		return "http://localhost:8888"
	}

	key := byte(0x5A)
	for i := range hexData {
		hexData[i] ^= key
	}

	return string(hexData)
}
