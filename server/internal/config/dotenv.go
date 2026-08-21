package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const maxDotEnvBytes = 1 << 20

func loadRootDotEnv(configPath string) (map[string]string, error) {
	path, explicit, err := resolveDotEnvPath(configPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read root environment file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("root environment file must be a regular file")
	}
	if info.Size() > maxDotEnvBytes {
		return nil, errors.New("root environment file exceeds the supported size")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("root environment file must have mode 0600")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open root environment file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("root environment file changed while it was opened")
	}
	return parseDotEnv(file)
}

func resolveDotEnvPath(configPath string) (string, bool, error) {
	if configured := strings.TrimSpace(os.Getenv("SERVER_ENV_FILE")); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", true, errors.New("resolve root environment file")
		}
		return absolute, true, nil
	}
	absoluteConfig, err := filepath.Abs(configPath)
	if err != nil {
		return "", false, errors.New("resolve server configuration path")
	}
	configDir := filepath.Dir(absoluteConfig)
	applicationDir := filepath.Dir(configDir)
	if filepath.Base(configDir) == "configs" && filepath.Base(applicationDir) == "server" {
		applicationDir = filepath.Dir(applicationDir)
	}
	return filepath.Join(applicationDir, ".env"), false, nil
}

func parseDotEnv(file *os.File) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxDotEnvBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, encoded, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !validDotEnvKey(key) {
			return nil, fmt.Errorf("root environment file line %d is invalid", lineNumber)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("root environment file line %d duplicates a key", lineNumber)
		}
		value, err := decodeDotEnvValue(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("root environment file line %d has an invalid value", lineNumber)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("scan root environment file")
	}
	return values, nil
}

func decodeDotEnvValue(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if encoded[0] != '"' {
		if strings.ContainsAny(encoded, "\x00\r\n") {
			return "", errors.New("invalid unquoted value")
		}
		return encoded, nil
	}
	value, err := strconv.Unquote(encoded)
	if err != nil || strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("invalid quoted value")
	}
	return value, nil
}

func validDotEnvKey(key string) bool {
	if key == "" || key[0] < 'A' || key[0] > 'Z' {
		return false
	}
	for index := 0; index < len(key); index++ {
		character := key[index]
		if character == '_' || (character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') {
			continue
		}
		return false
	}
	return true
}

func applyDotEnvOverrides(v interface{ Set(string, any) }, dotEnv map[string]string) {
	for configKey, environmentKey := range environmentBindings {
		if _, setByProcess := os.LookupEnv(environmentKey); setByProcess {
			continue
		}
		if value, ok := dotEnv[environmentKey]; ok {
			v.Set(configKey, value)
		}
	}
}
