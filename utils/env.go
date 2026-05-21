package utils

import (
	"os"
	"strconv"
)

func GetEnvString(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

func GetEnvInt(key string, defaultValue int) (int, error) {
	if value, ok := os.LookupEnv(key); ok {
		return strconv.Atoi(value)
	}
	return defaultValue, nil
}
