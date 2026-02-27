package config

import (
	"fmt"
	"os"
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

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI environment variable is required")
	}

	databaseName := os.Getenv("DATABASE_NAME")
	if databaseName == "" {
		databaseName = "zen_messaging"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL environment variable is required")
	}

	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		return nil, fmt.Errorf("RABBITMQ_URL environment variable is required")
	}

	serverPort := os.Getenv("SERVER_PORT")
	if serverPort == "" {
		serverPort = "8080"
	}

	return &Config{
		MongoURI:     mongoURI,
		DatabaseName: databaseName,
		RedisURL:     redisURL,
		RabbitMQURL:  rabbitmqURL,
		ServerPort:   serverPort,
	}, nil
}
