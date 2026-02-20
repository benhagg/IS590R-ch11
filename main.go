package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Donut struct {
	ItemId string `json:"itemId" dynamodbav:"ItemId"`
	Name   string `json:"name"   dynamodbav:"Name"`
}

var db *dynamodb.Client
const tableName = "PDC-Inventory"

func main() {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db = dynamodb.NewFromConfig(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", withCORS(healthHandler))
	mux.HandleFunc("/all_donuts", withCORS(allDonutsHandler))
	mux.HandleFunc("/donuts", withCORS(donutByIdHandler))

	fmt.Println("Server active at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func allDonutsHandler(w http.ResponseWriter, r *http.Request) {
	out, err := db.Scan(context.TODO(), &dynamodb.ScanInput{TableName: aws.String(tableName)}) // & is creating a pointer to the ScanInput struct. this way we dont pass a large object to the function
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var donuts []Donut
	attributevalue.UnmarshalListOfMaps(out.Items, &donuts) // passing the pointer with & also allows the function to modify the original donuts, instead of getting a temporary copy of it.
	
	fmt.Printf("Scan successful. Found %d items.\n", len(donuts))
	json.NewEncoder(w).Encode(donuts) // w writes directly to the HTTP response body, json.NewEncoder encodes the donuts slice as JSON and sends it in the response
	// there is no return statement because we are modifying the HTTP response directly through the http.ResponseWriter interface.
}

func donutByIdHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	fmt.Printf("Searching for ID: '%s'\n", id) // DEBUG 1
	if id == "" {
		http.Error(w, "Missing id parameter", 400)
		return
	}

	out, err := db.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"ItemID": &types.AttributeValueMemberS{Value: id},
		},
	})

	if err != nil || out.Item == nil {
		fmt.Printf(err.Error())
		http.Error(w, "Donut not found", 404)
		return
	}

	var d Donut
	attributevalue.UnmarshalMap(out.Item, &d)
	json.NewEncoder(w).Encode(d)
}