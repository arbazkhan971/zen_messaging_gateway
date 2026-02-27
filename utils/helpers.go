package utils

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"zen_messaging_gateway/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// GetInitials returns the initials from a given name
func GetInitials(name string) string {
	words := strings.Fields(name)
	initials := ""
	for _, word := range words {
		if len(word) > 0 {
			initials += string(word[0])
		}
	}
	return url.QueryEscape(initials)
}

// DoWithRetry performs an HTTP request with retry logic for transient errors
func DoWithRetry(client *http.Client, req *http.Request, maxRetries int) (*http.Response, error) {
	var resp *http.Response
	var err error
	backoff := time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = client.Do(req)
		if err != nil {
			log.Printf("[WARN] HTTP request error (attempt %d): %v", attempt+1, err)
		} else if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode < 600) {
			log.Printf("[WARN] Got status %d (attempt %d), retrying after %s...", resp.StatusCode, attempt+1, backoff)
			resp.Body.Close() // Important to close the body before retrying
		} else {
			// Success
			return resp, nil
		}

		// Wait before retrying
		time.Sleep(backoff)
		backoff *= 2 // exponential
	}
	return resp, err
}

// getProjectProviderType gets the provider type for a project
func GetProjectProviderType(projectID string) (string, string, string, error) {
	log.Printf("[PROJECT PROVIDER] Getting provider type for project: %s", projectID)

	projectObjID, err := primitive.ObjectIDFromHex(projectID)
	if err != nil {
		log.Printf("[PROJECT PROVIDER] Invalid ProjectID format: %s, error: %v", projectID, err)
		return "", "", "", fmt.Errorf("invalid ProjectID format: %w", err)
	}

	projectsCollection := GetCollection("projects")
	if projectsCollection == nil {
		log.Printf("[PROJECT PROVIDER] Failed to get projects collection for ID: %s", projectID)
		return "", "", "", fmt.Errorf("database connection error: projects collection not found")
	}

	var project bson.M
	err = projectsCollection.FindOne(context.Background(), bson.M{"_id": projectObjID}).Decode(&project)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			log.Printf("[PROJECT PROVIDER] Project not found: %s", projectID)
			return "", "", "", fmt.Errorf("project not found: %w", err)
		}
		log.Printf("[PROJECT PROVIDER] Error finding project %s: %v", projectID, err)
		return "", "", "", fmt.Errorf("error finding project: %w", err)
	}

	providerType, _ := project["project_provider_type"].(string)
	authCode, _ := project["fb_permanent_token"].(string)
	wabaID, _ := project["waba_id"].(string)

	log.Printf("[PROJECT PROVIDER] Project %s provider type: %s", projectID, providerType)

	return providerType, authCode, wabaID, nil
}

// Helper functions for safe value extraction from bson.M
func GetStringValue(data bson.M, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

func GetInt64Value(data bson.M, key string) int64 {
	return GetInt64FromInterface(data[key])
}

func GetIntValue(data bson.M, key string) int {
	return GetIntFromInterface(data[key])
}

func GetBoolValue(data bson.M, key string) bool {
	if value, ok := data[key].(bool); ok {
		return value
	}
	return false
}

func GetMapValue(data bson.M, key string) map[string]interface{} {
	if value, ok := data[key].(bson.M); ok {
		return value
	}
	if value, ok := data[key].(map[string]interface{}); ok {
		return value
	}
	return make(map[string]interface{})
}

func GetInt64FromInterface(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}

func GetIntFromInterface(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func GetKey(data bson.M) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

// cleanPhoneNumber removes all non-digit characters from a phone number
func CleanPhoneNumber(phone string) string {
	// Remove all non-digit characters
	var result strings.Builder
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			result.WriteRune(char)
		}
	}
	return result.String()
}

// getStringValue safely extracts a string value from nested map structure
func getStringValue(payload map[string]interface{}, keys ...string) string {
	current := payload
	for i, key := range keys {
		if i == len(keys)-1 {
			// Last key, return the string value
			if val, ok := current[key].(string); ok {
				return val
			}
			return ""
		}
		// Navigate deeper into the map
		if next, ok := current[key].(map[string]interface{}); ok {
			current = next
		} else {
			return ""
		}
	}
	return ""
}

func GetProjectOwnerIDFromContext(c *gin.Context, ctx context.Context) (string, *models.Agent, error) {
	agentID := c.GetString("user_id")
	if agentID == "" {
		return "", nil, fmt.Errorf("user_id not found in context")
	}

	agentCollection := GetCollection("agents")
	if agentCollection == nil {
		return "", nil, fmt.Errorf("database connection failed for agents")
	}

	var agent models.Agent
	err := agentCollection.FindOne(ctx, bson.M{"_id": agentID}).Decode(&agent)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch agent: %w", err)
	}

	var projectOwnerID string
	if agent.Type == models.AgentTypeOwner {
		projectOwnerID = agent.ID
	} else if agent.Type == models.AgentTypeAgent {
		if agent.ProjectOwnerID == "" {
			// If agent doesn't have ProjectOwnerID, try to find the owner from the project
			var projectOwner models.Agent
			err = agentCollection.FindOne(ctx, bson.M{"project_id": agent.ProjectID, "type": models.AgentTypeOwner}).Decode(&projectOwner)
			if err != nil {
				return "", nil, fmt.Errorf("failed to fetch project owner: %w", err)
			}
			projectOwnerID = projectOwner.ID
		} else {
			projectOwnerID = agent.ProjectOwnerID
		}
	} else {
		return "", nil, fmt.Errorf("invalid agent type: %s", agent.Type)
	}

	return projectOwnerID, &agent, nil
}
