package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"zen_messaging_gateway/config"
	"zen_messaging_gateway/consumers"
	"zen_messaging_gateway/routers/webhook"
	"zen_messaging_gateway/utils"

	"github.com/gin-contrib/cors"
	"github.com/streadway/amqp"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

const (
	readTimeout     = 30 * time.Second
	writeTimeout    = 5 * time.Minute
	maxHeaderBytes  = 20 << 20 // 20MB
	shutdownTimeout = 30 * time.Second
)

func initializeServer() (*gin.Engine, error) {
	numCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPU)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Configure CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowCredentials = true
	corsConfig.AllowHeaders = []string{
		"Origin",
		"Content-Type",
		"Accept",
		"Authorization",
		"X-Requested-With",
		"X-Event-Type",
		"X-Event-ID",
		"X-Webhook-Signature",
	}
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	corsConfig.ExposeHeaders = []string{"Content-Length"}
	corsConfig.MaxAge = 12 * time.Hour

	// Add middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(cors.New(corsConfig))

	// Limit request body size to 100MB
	router.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 100<<20)
		c.Next()
	})

	// Map webhook routes
	webhook.MapWebhookRoutes(router)

	// Health check endpoints
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ZEN Messaging Gateway is running"})
	})
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	return router, nil
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	log.Println("🚀 Starting ZEN Messaging Gateway...")

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	log.Printf("Configuration loaded - Database: %s, Port: %s", cfg.DatabaseName, cfg.ServerPort)

	// Initialize MongoDB
	log.Println("Connecting to MongoDB...")
	if err := utils.InitMongo(cfg.MongoURI, cfg.DatabaseName); err != nil {
		log.Fatal("Failed to initialize MongoDB:", err)
	}

	// Start Redis connection manager
	log.Println("Connecting to Redis...")
	go utils.ManageRedisConnection(cfg.RedisURL)

	// Initialize RabbitMQ
	log.Println("Initializing RabbitMQ...")
	if err := utils.InitRabbitMQ(cfg.RabbitMQURL); err != nil {
		log.Fatal("Failed to initialize RabbitMQ:", err)
	}

	// Start webhook forwarding consumer with dedicated connection
	log.Println("Starting webhook forwarding consumer...")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[WEBHOOK_FORWARDER] 🔴 PANIC recovered: %v", r)
			}
		}()

		utils.ManageWebhookForwarderConnection(consumers.StartWebhookForwardingConsumer)
	}()

	// Initialize server
	router, err := initializeServer()
	if err != nil {
		log.Printf("Warning: Server initialization encountered error: %v", err)
	}

	srv := &http.Server{
		Addr:           ":" + cfg.ServerPort,
		Handler:        router,
		ReadTimeout:    readTimeout,
		WriteTimeout:   writeTimeout,
		MaxHeaderBytes: maxHeaderBytes,
	}

	// Start HTTP server
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Recovered from panic in server: %v", r)
			}
		}()
		log.Printf("🌐 HTTP server listening on :%s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Attempting graceful shutdown...")

	// Shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	// Close RabbitMQ resources
	utils.CloseRabbitMQ()

	log.Println("✅ Server exited successfully")
}
