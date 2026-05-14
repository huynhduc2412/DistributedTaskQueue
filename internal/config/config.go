package config

import (
	"os"
)

type Config struct {
	RedisAdrr string 
	StreamName string
	GroupName string
	DLQStream string
}

func Load() *Config {
	return &Config{ 
		RedisAdrr: getEnv("REDIS_ADDR" , "localhost:6379"),
		StreamName: "task_stream",
		GroupName: "worker_group",
		DLQStream: "task_stream:dlq",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}