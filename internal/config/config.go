package config

import (
	"os"
	"strconv"
)

type Config struct {
	RedisAdrr string 
	StreamName string
	GroupName string
	ConsumerName string
	WorkerCount int
}

func Load() *Config {
	return &Config{ 
		RedisAdrr: getEnv("REDIS_ADDR" , "localhost:6379"),
		StreamName: getEnv("STREAM_NAME" , "task_stream"),
		GroupName: getEnv("GROUP_NAME" , "worker_group"),
		ConsumerName: getEnv("HOSTNAME" , "worker-1"),
		WorkerCount: getEnvInt("WORKER_COUNT" , 0),
	}
}

func getEnv(key , fallBack string) string {
	if value , ok := os.LookupEnv(key); ok {
		return value
	}
	return fallBack
}

func getEnvInt(key string, fallBack int) int {
	if value , ok := os.LookupEnv(key) ; ok {
		i , err := strconv.Atoi(value)
		if err != nil {
			return fallBack
		}
		return i
	}
	return fallBack
}