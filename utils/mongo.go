package utils

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var (
	mongoClient  *mongo.Client
	database     *mongo.Database
	mongoClient2 *mongo.Client
	database2    *mongo.Database
	connMutex    sync.Mutex
	connChan     = make(chan struct{}, 1000) // Limit concurrent connections
)

func Init() {
	c, err := GetConfig()
	if err != nil {
		panic("Failed to load configuration: " + err.Error())
	}

	// Optimized client options
	clientOptions := options.Client().ApplyURI(c["mongo_uri"].(string)).
		SetMaxPoolSize(10).                   // Reduced from 20
		SetMinPoolSize(2).                    // Reduced from 5
		SetMaxConnIdleTime(30 * time.Second). // Reduced from 60
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetRetryWrites(true).
		SetRetryReads(true)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Use connection channel to limit concurrent connections
	select {
	case connChan <- struct{}{}:
		defer func() { <-connChan }()
	case <-ctx.Done():
		panic("Connection timeout while waiting for available connection slot")
	}

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to database: %v", err))
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		if closeErr := client.Disconnect(ctx); closeErr != nil {
			log.Printf("Error disconnecting failed client: %v", closeErr)
		}
		panic(fmt.Sprintf("Failed to ping database: %v", err))
	}

	databaseName, ok := c["database_name"].(string)
	if !ok || databaseName == "" {
		if closeErr := client.Disconnect(ctx); closeErr != nil {
			log.Printf("Error disconnecting client: %v", closeErr)
		}
		panic("Invalid or missing database_name in configuration")
	}

	mongoClient = client
	database = client.Database(databaseName)

	log.Printf("Successfully connected to MongoDB database: %s with optimized settings", databaseName)
}

func GetCollection(collName string) *mongo.Collection {
	connMutex.Lock()
	defer connMutex.Unlock()

	if database == nil {
		select {
		case connChan <- struct{}{}:
			defer func() { <-connChan }()
		default:
			log.Printf("Too many concurrent database connections")
			return nil
		}

		if err := Connect(); err != nil {
			log.Printf("Failed to connect to database: %v", err)
			return nil
		}
	}

	if database == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collections, err := database.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		log.Printf("Failed to list collections: %v", err)
		return nil
	}

	collExists := false
	for _, name := range collections {
		if name == collName {
			collExists = true
			break
		}
	}

	if !collExists {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = database.CreateCollection(ctx, collName)
		if err != nil {
			log.Printf("Failed to create collection %s: %v", collName, err)
			return nil
		}
	}

	return database.Collection(collName)
}

// InitMongo initializes MongoDB connection with the given URI and database name
func InitMongo(mongoURI, databaseName string) error {
	clientOptions := options.Client().ApplyURI(mongoURI).
		SetMaxPoolSize(10).
		SetMinPoolSize(2).
		SetMaxConnIdleTime(30 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetRetryWrites(true).
		SetRetryReads(true)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		if closeErr := client.Disconnect(ctx); closeErr != nil {
			log.Printf("Error disconnecting failed client: %v", closeErr)
		}
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	mongoClient = client
	database = client.Database(databaseName)

	log.Printf("Successfully connected to MongoDB database: %s", databaseName)
	return nil
}

// Connect attempts to reconnect to the MongoDB database
func Connect() error {
	c, err := GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %v", err)
	}

	clientOptions := options.Client().ApplyURI(c["mongo_uri"].(string)).
		SetMaxPoolSize(10).                   // Optimized pool size
		SetMinPoolSize(2).                    // Optimized min pool
		SetMaxConnIdleTime(30 * time.Second). // Reduced idle time
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetRetryWrites(true).
		SetRetryReads(true)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		if closeErr := client.Disconnect(ctx); closeErr != nil {
			log.Printf("Error disconnecting failed client: %v", closeErr)
		}
		return fmt.Errorf("failed to ping database: %v", err)
	}

	databaseName, ok := c["database_name"].(string)
	if !ok || databaseName == "" {
		if closeErr := client.Disconnect(ctx); closeErr != nil {
			log.Printf("Error disconnecting client: %v", closeErr)
		}
		return fmt.Errorf("invalid or missing database_name in configuration")
	}

	mongoClient = client
	database = client.Database(databaseName)

	return nil
}
