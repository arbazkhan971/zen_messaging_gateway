package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"zen_messaging_gateway/models"
	"zen_messaging_gateway/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MessageTracker handles unified message tracking across all BSPs
type MessageTracker struct {
	collection *mongo.Collection
}

// NewMessageTracker creates a new message tracker instance
func NewMessageTracker() *MessageTracker {
	return &MessageTracker{
		collection: utils.GetCollection("unified_message_tracking"),
	}
}

// CreateMessage creates a new unified message record
func (mt *MessageTracker) CreateMessage(msg *models.UnifiedMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := mt.collection.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}

	log.Printf("[MESSAGE_TRACKER] Created message: id=%s, bsp=%s, phone=%s",
		msg.MessageID, msg.BSP, msg.PhoneNumber)

	return nil
}

// UpdateMessageStatus updates the status of a message
func (mt *MessageTracker) UpdateMessageStatus(messageID, status, source, reason, errorCode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()

	// Build the status event
	statusEvent := models.MessageStatus{
		Status:    status,
		Source:    source,
		Timestamp: now,
		Reason:    reason,
		ErrorCode: errorCode,
	}

	// Build update document
	update := bson.M{
		"$set": bson.M{
			"current_status": status,
			"updated_at":     now,
		},
		"$push": bson.M{
			"status_history": statusEvent,
		},
	}

	// Update timestamp fields based on status
	switch status {
	case "sent":
		update["$set"].(bson.M)["sent_at"] = now
	case "delivered":
		update["$set"].(bson.M)["delivered_at"] = now
	case "read":
		update["$set"].(bson.M)["read_at"] = now
	case "failed":
		update["$set"].(bson.M)["failed_at"] = now
	}

	result, err := mt.collection.UpdateOne(
		ctx,
		bson.M{"message_id": messageID},
		update,
	)
	if err != nil {
		return fmt.Errorf("failed to update message status: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("message not found: %s", messageID)
	}

	log.Printf("[MESSAGE_TRACKER] Updated message status: id=%s, status=%s, source=%s",
		messageID, status, source)

	return nil
}

// UpdateMessageByWamid updates a message using WhatsApp Message ID
func (mt *MessageTracker) UpdateMessageByWamid(wamid, status, source, reason, errorCode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()

	statusEvent := models.MessageStatus{
		Status:    status,
		Source:    source,
		Timestamp: now,
		Reason:    reason,
		ErrorCode: errorCode,
	}

	update := bson.M{
		"$set": bson.M{
			"current_status": status,
			"updated_at":     now,
		},
		"$push": bson.M{
			"status_history": statusEvent,
		},
	}

	switch status {
	case "sent":
		update["$set"].(bson.M)["sent_at"] = now
	case "delivered":
		update["$set"].(bson.M)["delivered_at"] = now
	case "read":
		update["$set"].(bson.M)["read_at"] = now
	case "failed":
		update["$set"].(bson.M)["failed_at"] = now
	}

	result, err := mt.collection.UpdateOne(
		ctx,
		bson.M{"wamid": wamid},
		update,
	)
	if err != nil {
		return fmt.Errorf("failed to update message by wamid: %w", err)
	}

	if result.MatchedCount == 0 {
		log.Printf("[MESSAGE_TRACKER] Message not found for wamid: %s, creating new record", wamid)
		// Create a new message if not found
		return mt.createMessageFromWamid(wamid, status, source, reason, errorCode)
	}

	log.Printf("[MESSAGE_TRACKER] Updated message by wamid: wamid=%s, status=%s", wamid, status)

	return nil
}

func (mt *MessageTracker) createMessageFromWamid(wamid, status, source, reason, errorCode string) error {
	msg := &models.UnifiedMessage{
		ID:            primitive.NewObjectID(),
		MessageID:     primitive.NewObjectID().Hex(),
		Wamid:         wamid,
		BSP:           source,
		CurrentStatus: status,
		StatusHistory: []models.MessageStatus{
			{
				Status:    status,
				Source:    source,
				Timestamp: time.Now(),
				Reason:    reason,
				ErrorCode: errorCode,
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return mt.CreateMessage(msg)
}

// FindByWamid finds a message by WhatsApp Message ID
func (mt *MessageTracker) FindByWamid(wamid string) (*models.UnifiedMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var msg models.UnifiedMessage
	err := mt.collection.FindOne(ctx, bson.M{"wamid": wamid}).Decode(&msg)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find message: %w", err)
	}

	return &msg, nil
}

// FindByMessageID finds a message by internal message ID
func (mt *MessageTracker) FindByMessageID(messageID string) (*models.UnifiedMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var msg models.UnifiedMessage
	err := mt.collection.FindOne(ctx, bson.M{"message_id": messageID}).Decode(&msg)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find message: %w", err)
	}

	return &msg, nil
}

// GetMessagesByCampaign gets all messages for a campaign
func (mt *MessageTracker) GetMessagesByCampaign(campaignID string, limit int64) ([]*models.UnifiedMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := mt.collection.Find(ctx, bson.M{"campaign_id": campaignID}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find messages: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []*models.UnifiedMessage
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, fmt.Errorf("failed to decode messages: %w", err)
	}

	return messages, nil
}

// GetMessagesByProjectOwner gets all messages for a project owner
func (mt *MessageTracker) GetMessagesByProjectOwner(projectOwnerID string, limit int64) ([]*models.UnifiedMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := mt.collection.Find(ctx, bson.M{"project_owner_id": projectOwnerID}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find messages: %w", err)
	}
	defer cursor.Close(ctx)

	var messages []*models.UnifiedMessage
	if err := cursor.All(ctx, &messages); err != nil {
		return nil, fmt.Errorf("failed to decode messages: %w", err)
	}

	return messages, nil
}

// GetMessageStats gets statistics for a project owner
func (mt *MessageTracker) GetMessageStats(projectOwnerID string) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pipeline := []bson.M{
		{
			"$match": bson.M{"project_owner_id": projectOwnerID},
		},
		{
			"$group": bson.M{
				"_id":   "$current_status",
				"count": bson.M{"$sum": 1},
			},
		},
	}

	cursor, err := mt.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate stats: %w", err)
	}
	defer cursor.Close(ctx)

	stats := make(map[string]int64)
	for cursor.Next(ctx) {
		var result struct {
			ID    string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&result); err != nil {
			continue
		}
		stats[result.ID] = result.Count
	}

	return stats, nil
}

// Global message tracker instance
var globalMessageTracker *MessageTracker

// InitMessageTracker initializes the global message tracker
func InitMessageTracker() {
	globalMessageTracker = NewMessageTracker()
	log.Println("[MESSAGE_TRACKER] Initialized unified message tracker")
}

// GetMessageTracker returns the global message tracker instance
func GetMessageTracker() *MessageTracker {
	if globalMessageTracker == nil {
		InitMessageTracker()
	}
	return globalMessageTracker
}
