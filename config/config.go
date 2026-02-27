package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	// MongoDB
	MongoURI      string
	DatabaseName  string

	// Redis
	RedisURL      string

	// RabbitMQ
	RabbitMQURL   string

	// Server
	ServerPort    string
}

// KeysJSON represents the structure of Keys.json file
type KeysJSON struct {
	DatabaseName string `json:"database_name"`
	MongoURI     string `json:"mongo_uri"`
	Port         int    `json:"port"`
}

// LoadConfig loads configuration from Keys.json or environment variables
// Priority: Environment variables > Keys.json
func LoadConfig() (*Config, error) {
	var keysData KeysJSON

	// Try to load Keys.json first
	if data, err := os.ReadFile("Keys.json"); err == nil {
		if err := json.Unmarshal(data, &keysData); err != nil {
			return nil, fmt.Errorf("failed to parse Keys.json: %w", err)
		}
	}

	// Get MongoDB URI (env var takes priority)
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = keysData.MongoURI
	}
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI not found in environment or Keys.json")
	}

	// Get Database Name (env var takes priority)
	databaseName := os.Getenv("DATABASE_NAME")
	if databaseName == "" {
		databaseName = keysData.DatabaseName
	}
	if databaseName == "" {
		databaseName = "zen_messaging"
	}

	// Get Redis URL (env var takes priority)
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://:YOUR_STRONG_PASSWORD@34.100.179.109:6379/0"
	}

	// Get RabbitMQ URL (env var takes priority)
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		rabbitmqURL = "amqp://admin:StrongPassword123%21@34.66.150.47:5672"
	}

	// Get Server Port (env var takes priority)
	serverPort := os.Getenv("SERVER_PORT")
	if serverPort == "" {
		if keysData.Port > 0 {
			serverPort = strconv.Itoa(keysData.Port)
		} else {
			serverPort = "8080"
		}
	}

	return &Config{
		MongoURI:     mongoURI,
		DatabaseName: databaseName,
		RedisURL:     redisURL,
		RabbitMQURL:  rabbitmqURL,
		ServerPort:   serverPort,
	}, nil
}
