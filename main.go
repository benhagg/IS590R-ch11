package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// map[string]interface{} says keys are strings, values can be any type.
type responseItem map[string]interface{}

// apiResponse is a struct (Go's equivalent of a class/object with fixed fields)
// Struct tags (the backtick strings) tell the JSON encoder what JSON field names to use
// `json:"itemIds"` means: when encoding to JSON, use the key "itemIds" for the ItemIDs field
type apiResponse struct {
	ItemIDs []string       `json:"itemIds"`
	Items   []responseItem `json:"items"`
}

func main() {
	tableName := os.Getenv("TABLE_NAME")
	if tableName == "" {
		// log.Fatal() prints error and exits the program immediately
		log.Fatal("TABLE_NAME is required")
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	storeID := os.Getenv("STORE_ID")

	// config.LoadDefaultConfig() loads AWS credentials from environment (IAM role in Fargate)
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	// dynamodb is the package name for "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	// this creates a DynamoDB client.
	client := dynamodb.NewFromConfig(cfg)

	// http router
	mux := http.NewServeMux()
	
	// Note the receiver parameters: w is passed by value, r is passed by pointer (*)
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		// CORS headers - allow requests from any origin (frontend can call this API)
		setCommonHeaders(w)
		
		// Handle CORS preflight requests (browser sends OPTIONS before GET/POST)
		if r.Method == http.MethodOptions {
			// sends 204 response, CORS headers, no respose body. tells browser it's ok to proceed with actual request with these specific http methods.
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Only allow GET requests; reject everything else
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed) 
			return
		}
	
		// := is Go's short variable declaration (only in functions)
		// Declares and initializes var in one line
		ids := parseItemIDs(r.URL.Query().Get("itemIds"))
		if len(ids) == 0 {
			http.Error(w, "itemIds is required", http.StatusBadRequest)
			return
		}


		// any queries > 5 sec are canceled, if a query is returned before then the defer keyword ensures that cancel() is immediately called to free resources. This is a common Go pattern for managing timeouts and cancellations.
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()


		var items []responseItem
		
		// Conditional logic: use batch query if storeID provided, otherwise scan all items
		if storeID != "" {
			items, err = batchGetItems(ctx, client, tableName, storeID, ids)
		} else {
			items, err = scanByItemIDs(ctx, client, tableName, ids)
		}

		if err != nil {
			// Go idiom: error handling comes immediately after operations
			log.Printf("query error: %v", err)
			http.Error(w, "failed to query items", http.StatusInternalServerError)
			return
		}

		// Create response object using the struct defined at top
		resp := apiResponse{ItemIDs: ids, Items: items}
		
		// Set content-type header before writing body
		w.Header().Set("Content-Type", "application/json")
		
		// json.NewEncoder(w).Encode() writes JSON directly to response
		// This is the streaming approach (more efficient than json.Marshal for large responses)
		// defer + ignore pattern: we check error but don't act on it (just log)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode error: %v", err)
		}
	})

	// Register /health endpoint for load balancer health checks
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		setCommonHeaders(w)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Bind to port 8080 and start listening
	// This is a blocking call - the server runs forever until an error occurs
	addr := ":8080"
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func parseItemIDs(raw string) []string {
	// strings.TrimSpace() removes leading/trailing whitespace (like trim() in other languages)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Return nil slice (not empty slice - important distinction in Go)
		// nil slices are often used to indicate "no data" vs [] which means "empty but allocated"
		return nil
	}

	// strings.Split() splits string by delimiter, returns []string
	// Example: "1,2,3" → ["1", "2", "3"]
	parts := strings.Split(raw, ",")
	
	// make() allocates a slice with capacity to hold len(parts) strings
	// make([]string, 0, len(parts)) - length 0, capacity len(parts)
	// This is more efficient than append() to nil repeatedly
	ids := make([]string, 0, len(parts))
	
	// Classic Go pattern: range over slice to get index and value
	// We only need the value here, so use _ for index (blank identifier = ignore this)
	for _, part := range parts {
		// Trim whitespace from each part
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			// append() adds element to slice, returns new slice (slices are dynamic)
			// Since we pre-allocated with capacity, this is efficient
			ids = append(ids, trimmed)
		}
	}
	return ids
}

func setCommonHeaders(w http.ResponseWriter) {
	// CORS headers allow browsers to call this API from different domains
	// In Go, methods that don't return values still use parentheses (unlike Python properties)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Accept")
}

func batchGetItems(ctx context.Context, client *dynamodb.Client, tableName, storeID string, itemIDs []string) ([]responseItem, error) {
	// Prepare batch keys for DynamoDB query
	// make([]map[string]types.AttributeValue, 0, len(itemIDs)) = allocate slice of maps
	// In DynamoDB, you can't just pass strings - must wrap in DynamoDB types
	keys := make([]map[string]types.AttributeValue, 0, len(itemIDs))
	
	for _, id := range itemIDs {
		// This is DynamoDB's composite key: (StoreID, ItemID)
		// &types.AttributeValueMemberS{Value: ...} is the type-safe way to create a DynamoDB string
		// The & operator takes address (pointer) of the struct literal
		keys = append(keys, map[string]types.AttributeValue{
			"StoreID": &types.AttributeValueMemberS{Value: storeID},
			"ItemID":  &types.AttributeValueMemberS{Value: id},
		})
	}

	// BatchGetItem is like SELECT * WHERE pk IN (...)
	input := &dynamodb.BatchGetItemInput{
		RequestItems: map[string]types.KeysAndAttributes{
			tableName: {Keys: keys},
		},
	}

	// client.BatchGetItem() makes the actual AWS API call
	// ctx timeout will cancel if operation takes > 5 seconds
	// & before dynamodb.BatchGetItemInput creates a pointer (required by SDK)
	out, err := client.BatchGetItem(ctx, input)
	if err != nil {
		// Go idiom: return zero values for out + error
		// Return early if error (fail fast)
		return nil, err
	}

	// out.Responses[tableName] contains the results
	// Pass to convertItems to transform DynamoDB format to JSON-friendly maps
	return convertItems(out.Responses[tableName]), nil
}

func scanByItemIDs(ctx context.Context, client *dynamodb.Client, tableName string, itemIDs []string) ([]responseItem, error) {
	// DynamoDB filter expressions use placeholders like :v0, :v1, etc.
	// This prevents SQL injection-like attacks (parameterized queries)
	exprVals := make(map[string]types.AttributeValue, len(itemIDs))
	placeholders := make([]string, 0, len(itemIDs))
	
	// Build placeholder list: ":v0", ":v1", ":v2", etc.
	for i, id := range itemIDs {
		// strconv.Itoa() = Integer To Ascii (int to string conversion)
		// Build key like ":v0", ":v1", ...
		key := ":v" + strconv.Itoa(i)
		exprVals[key] = &types.AttributeValueMemberS{Value: id}
		placeholders = append(placeholders, key)
	}

	// Build filter expression: "ItemID IN (:v0,:v1,:v2)"
	// strings.Join takes slice of strings and joins with separator
	filter := "ItemID IN (" + strings.Join(placeholders, ",") + ")"

	// Scan reads entire table (filtered by expression)
	// This is inefficient for large tables but works when no partition key filter available
	input := &dynamodb.ScanInput{
		TableName:                 aws.String(tableName),
		ExpressionAttributeValues: exprVals,
		FilterExpression:          aws.String(filter),
	}

	// aws.String() is a helper that converts string to *string (pointer)
	// DynamoDB SDK uses pointers for optional fields
	out, err := client.Scan(ctx, input)
	if err != nil {
		return nil, err
	}

	return convertItems(out.Items), nil
}

func convertItems(items []map[string]types.AttributeValue) []responseItem {
	// Create slice to hold results, pre-allocate with capacity = len(items)
	results := make([]responseItem, 0, len(items))
	for _, item := range items {
		// Call helper to transform each DynamoDB item to JSON-friendly map
		results = append(results, attributeValueToMap(item))
	}
	return results
}

func attributeValueToMap(item map[string]types.AttributeValue) responseItem {
	// Create empty map to hold converted data
	// In Go, maps must be initialized with make() (unlike slices which can be nil)
	result := make(responseItem)
	for key, val := range item {
		// unwrapAttributeValue() recursively converts DynamoDB types to Go types
		// Go's type conversion often requires explicit function calls (no operator overloading)
		result[key] = unwrapAttributeValue(val)
	}
	return result
}

func unwrapAttributeValue(val types.AttributeValue) interface{} {
	// Type switch - Go's way of pattern matching on types
	// Similar to switch/case but for type assertion instead of values
	// v := val.(type) extracts both the type and value
	switch v := val.(type) {
	case *types.AttributeValueMemberS:
		// This is a DynamoDB String. Return the wrapped value.
		// v.Value accesses the field of the concrete type
		return v.Value
	case *types.AttributeValueMemberN:
		// DynamoDB Number (stored as string internally)
		return v.Value
	case *types.AttributeValueMemberBOOL:
		// DynamoDB Boolean
		return v.Value
	case *types.AttributeValueMemberSS:
		// String Set (slice of strings in DynamoDB)
		return v.Value
	case *types.AttributeValueMemberNS:
		// Number Set
		return v.Value
	case *types.AttributeValueMemberM:
		// Map (nested object) - recursive case
		out := make(map[string]interface{})
		for k, mv := range v.Value {
			// Recursively unwrap nested values
			out[k] = unwrapAttributeValue(mv)
		}
		return out
	case *types.AttributeValueMemberL:
		// List (array)
		list := make([]interface{}, 0, len(v.Value))
		for _, lv := range v.Value {
			// Recursively unwrap list items
			list = append(list, unwrapAttributeValue(lv))
		}
		return list
	default:
		// Unknown type - return nil
		// default case in type switch is like default in switch statements
		return nil
	}
}
