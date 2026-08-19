package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id 参数(OWASP 推荐档位,手机端可接受)。
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
)

// hashPassword 使用 Argon2id 计算密码哈希,编码格式:
// argon2id$v=19$m=65536,t=3,p=4$<salt base64>$<hash base64>
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword 校验密码与哈希是否匹配(恒定时间比较)。
// 哈希格式:argon2id$v=19$m=65536,t=3,p=4$<salt base64>$<hash base64>(5 段)。
func verifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false, errors.New("不支持的密码哈希格式")
	}
	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return false, fmt.Errorf("解析 Argon2id 版本: %w", err)
	}
	var memory, time uint32
	var threads uint8
	for _, kv := range strings.Split(parts[2], ",") {
		kvParts := strings.SplitN(kv, "=", 2)
		if len(kvParts) != 2 {
			return false, errors.New("Argon2id 参数格式无效")
		}
		val, err := strconv.ParseUint(kvParts[1], 10, 32)
		if err != nil {
			return false, fmt.Errorf("解析 Argon2id 参数 %q: %w", kv, err)
		}
		switch kvParts[0] {
		case "m":
			memory = uint32(val)
		case "t":
			time = uint32(val)
		case "p":
			threads = uint8(val)
		}
	}
	if version != argon2.Version || memory == 0 || time == 0 || threads == 0 {
		return false, errors.New("Argon2id 参数无效")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, fmt.Errorf("解码盐: %w", err)
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("解码哈希: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(hash)))
	if subtle.ConstantTimeCompare(key, hash) != 1 {
		return false, nil
	}
	return true, nil
}
