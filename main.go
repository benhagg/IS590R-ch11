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

type responseItem map[string]interface{}

type apiResponse struct {
  ItemIDs []string       `json:"itemIds"`
  Items   []responseItem `json:"items"`
}

func main() {
  tableName := os.Getenv("TABLE_NAME")
  if tableName == "" {
    log.Fatal("TABLE_NAME is required")
  }

  region := os.Getenv("AWS_REGION")
  if region == "" {
    region = "us-east-1"
  }

  storeID := os.Getenv("STORE_ID")

  cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
  if err != nil {
    log.Fatalf("failed to load AWS config: %v", err)
  }

  client := dynamodb.NewFromConfig(cfg)

  mux := http.NewServeMux()
  mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
    setCommonHeaders(w)
    if r.Method == http.MethodOptions {
      w.WriteHeader(http.StatusNoContent)
      return
    }

    if r.Method != http.MethodGet {
      http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
      return
    }

    ids := parseItemIDs(r.URL.Query().Get("itemIds"))
    if len(ids) == 0 {
      http.Error(w, "itemIds is required", http.StatusBadRequest)
      return
    }

    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()

    var items []responseItem
    if storeID != "" {
      items, err = batchGetItems(ctx, client, tableName, storeID, ids)
    } else {
      items, err = scanByItemIDs(ctx, client, tableName, ids)
    }

    if err != nil {
      log.Printf("query error: %v", err)
      http.Error(w, "failed to query items", http.StatusInternalServerError)
      return
    }

    resp := apiResponse{ItemIDs: ids, Items: items}
    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(resp); err != nil {
      log.Printf("encode error: %v", err)
    }
  })

  mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    setCommonHeaders(w)
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
  })

  addr := ":8080"
  log.Printf("listening on %s", addr)
  if err := http.ListenAndServe(addr, mux); err != nil {
    log.Fatalf("server error: %v", err)
  }
}

func parseItemIDs(raw string) []string {
  raw = strings.TrimSpace(raw)
  if raw == "" {
    return nil
  }

  parts := strings.Split(raw, ",")
  ids := make([]string, 0, len(parts))
  for _, part := range parts {
    trimmed := strings.TrimSpace(part)
    if trimmed != "" {
      ids = append(ids, trimmed)
    }
  }
  return ids
}

func setCommonHeaders(w http.ResponseWriter) {
  w.Header().Set("Access-Control-Allow-Origin", "*")
  w.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
  w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Accept")
}

func batchGetItems(ctx context.Context, client *dynamodb.Client, tableName, storeID string, itemIDs []string) ([]responseItem, error) {
  keys := make([]map[string]types.AttributeValue, 0, len(itemIDs))
  for _, id := range itemIDs {
    keys = append(keys, map[string]types.AttributeValue{
      "StoreID": &types.AttributeValueMemberS{Value: storeID},
      "ItemID":  &types.AttributeValueMemberS{Value: id},
    })
  }

  input := &dynamodb.BatchGetItemInput{
    RequestItems: map[string]types.KeysAndAttributes{
      tableName: {Keys: keys},
    },
  }

  out, err := client.BatchGetItem(ctx, input)
  if err != nil {
    return nil, err
  }

  return convertItems(out.Responses[tableName]), nil
}

func scanByItemIDs(ctx context.Context, client *dynamodb.Client, tableName string, itemIDs []string) ([]responseItem, error) {
  exprVals := make(map[string]types.AttributeValue, len(itemIDs))
  placeholders := make([]string, 0, len(itemIDs))
  for i, id := range itemIDs {
    key := ":v" + strconv.Itoa(i)
    exprVals[key] = &types.AttributeValueMemberS{Value: id}
    placeholders = append(placeholders, key)
  }

  filter := "ItemID IN (" + strings.Join(placeholders, ",") + ")"

  input := &dynamodb.ScanInput{
    TableName:                 aws.String(tableName),
    ExpressionAttributeValues: exprVals,
    FilterExpression:          aws.String(filter),
  }

  out, err := client.Scan(ctx, input)
  if err != nil {
    return nil, err
  }

  return convertItems(out.Items), nil
}

func convertItems(items []map[string]types.AttributeValue) []responseItem {
  results := make([]responseItem, 0, len(items))
  for _, item := range items {
    results = append(results, attributeValueToMap(item))
  }
  return results
}

func attributeValueToMap(item map[string]types.AttributeValue) responseItem {
  result := make(responseItem)
  for key, val := range item {
    result[key] = unwrapAttributeValue(val)
  }
  return result
}

func unwrapAttributeValue(val types.AttributeValue) interface{} {
  switch v := val.(type) {
  case *types.AttributeValueMemberS:
    return v.Value
  case *types.AttributeValueMemberN:
    return v.Value
  case *types.AttributeValueMemberBOOL:
    return v.Value
  case *types.AttributeValueMemberSS:
    return v.Value
  case *types.AttributeValueMemberNS:
    return v.Value
  case *types.AttributeValueMemberM:
    out := make(map[string]interface{})
    for k, mv := range v.Value {
      out[k] = unwrapAttributeValue(mv)
    }
    return out
  case *types.AttributeValueMemberL:
    list := make([]interface{}, 0, len(v.Value))
    for _, lv := range v.Value {
      list = append(list, unwrapAttributeValue(lv))
    }
    return list
  default:
    return nil
  }
}
