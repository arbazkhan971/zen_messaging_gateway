package utils

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/streadway/amqp"
)

// PublishToQueue publishes a message to the specified RabbitMQ queue with priority
func PublishToQueue(queueName string, payload interface{}, priority uint8) error {
	// Get channel from connection pool
	ch, err := getChannelFromPool()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer ch.Close()

	// Marshal payload to JSON
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Publish message
	err = ch.Publish(
		"",        // exchange (empty for direct queue publish)
		queueName, // routing key (queue name)
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent, // Make message persistent
			Priority:     priority,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	log.Printf("[QUEUE_PUBLISH] Published to %s (size: %d bytes, priority: %d)",
		queueName, len(body), priority)

	return nil
}

// PublishToExchange publishes a message to the specified RabbitMQ exchange
func PublishToExchange(exchangeName, routingKey string, payload interface{}, priority uint8) error {
	// Get channel from connection pool
	ch, err := getChannelFromPool()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer ch.Close()

	// Marshal payload to JSON
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Publish message
	err = ch.Publish(
		exchangeName, // exchange
		routingKey,   // routing key
		false,        // mandatory
		false,        // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Priority:     priority,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	log.Printf("[EXCHANGE_PUBLISH] Published to %s/%s (size: %d bytes, priority: %d)",
		exchangeName, routingKey, len(body), priority)

	return nil
}
