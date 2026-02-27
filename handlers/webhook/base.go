package webhook

import (
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// WebhookResponse is the standard response for all webhook endpoints
type WebhookResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// Common webhook processing utilities and helpers

var (
	// Goroutine limiting for high-throughput webhook processing
	goroutineSemaphore chan struct{}

	// Maximum concurrent goroutines
	maxConcurrentGoroutines int

	// Initialize once
	initOnce sync.Once
)

// InitWebhookHandlers initializes shared webhook handler resources
func InitWebhookHandlers() {
	initOnce.Do(func() {
		numCPU := runtime.NumCPU()
		maxConcurrentGoroutines = numCPU * 100 // 100 goroutines per CPU core
		goroutineSemaphore = make(chan struct{}, maxConcurrentGoroutines)

		log.Printf("[WEBHOOK_INIT] Goroutine limit set to %d (based on %d CPU cores)",
			maxConcurrentGoroutines, numCPU)
	})
}

// ProcessWebhookAsync processes a webhook in a background goroutine with semaphore control
func ProcessWebhookAsync(bspName string, processingFunc func()) {
	select {
	case goroutineSemaphore <- struct{}{}: // Acquire semaphore
		go func() {
			defer func() { <-goroutineSemaphore }() // Release semaphore
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[%s_WEBHOOK] PANIC recovered: %v", bspName, r)
				}
			}()

			processingFunc()
		}()
	default:
		// Semaphore full - log overload
		log.Printf("[%s_WEBHOOK] OVERLOAD - Semaphore full, dropping webhook (limit: %d)",
			bspName, maxConcurrentGoroutines)
	}
}

// RespondImmediately sends an immediate 200 OK response to prevent webhook retries
func RespondImmediately(c *gin.Context, bspName string) {
	c.JSON(200, WebhookResponse{
		Status:  200,
		Message: bspName + " webhook received successfully",
	})
}

// LogWebhookReceived logs webhook reception
func LogWebhookReceived(bspName string, payloadSize int) {
	log.Printf("[%s_WEBHOOK] Received webhook (payload size: %d bytes, timestamp: %s)",
		bspName, payloadSize, time.Now().Format(time.RFC3339))
}
