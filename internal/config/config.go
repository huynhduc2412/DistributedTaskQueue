package config

import (
	"os"
)

type Config struct {
	RedisAdrr  string
	StreamName string
	GroupName  string
	DLQStream  string
	MYSQL_DSN  string
}

func Load() *Config {
	return &Config{
		RedisAdrr:  getEnv("REDIS_ADDR", "localhost:6379"),
		StreamName: "task_stream",
		GroupName:  "worker_group",
		DLQStream:  "task_stream:dlq",
		MYSQL_DSN:  getEnv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/queue?parseTime=true"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
