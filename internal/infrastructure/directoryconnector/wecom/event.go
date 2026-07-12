package wecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"sort"
	"strings"
)

func VerifyEventSignature(token, timestamp, nonce, encrypted, signature string) error {
	parts := []string{token, timestamp, nonce, encrypted}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	expected := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return errors.New("invalid WeCom event signature")
	}
	return nil
}
func DecryptEvent(encodingAESKey, encrypted string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodingAESKey) + "=")
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid WeCom encoding AES key")
	}
	payload, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(payload) == 0 || len(payload)%aes.BlockSize != 0 {
		return nil, errors.New("invalid WeCom encrypted payload")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(payload))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, payload)
	plain, err = unpad(plain)
	if err != nil || len(plain) < 20 {
		return nil, errors.New("invalid WeCom decrypted payload")
	}
	size := int(binary.BigEndian.Uint32(plain[16:20]))
	if size < 0 || 20+size > len(plain) {
		return nil, errors.New("invalid WeCom message length")
	}
	return plain[20 : 20+size], nil
}

type Event struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Event        string `xml:"Event"`
	ChangeType   string `xml:"ChangeType"`
	UserID       string `xml:"UserID"`
	DepartmentID int64  `xml:"Id"`
}

func ParseEvent(payload []byte) (Event, error) {
	var event Event
	if err := xml.Unmarshal(payload, &event); err != nil {
		return Event{}, err
	}
	if event.Event == "" && event.MsgType == "" {
		return Event{}, errors.New("WeCom event type is required")
	}
	return event, nil
}
func ParseEncryptedXML(payload []byte) (string, error) {
	var envelope struct {
		Encrypt string `xml:"Encrypt"`
	}
	if err := xml.Unmarshal(payload, &envelope); err != nil {
		return "", err
	}
	if envelope.Encrypt == "" {
		return "", errors.New("WeCom Encrypt is required")
	}
	return envelope.Encrypt, nil
}
func unpad(value []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, errors.New("empty padded value")
	}
	padding := int(value[len(value)-1])
	if padding < 1 || padding > 32 || padding > len(value) {
		return nil, errors.New("invalid padding")
	}
	if !bytes.Equal(value[len(value)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, errors.New("invalid padding")
	}
	return value[:len(value)-padding], nil
}
