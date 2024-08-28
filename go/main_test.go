package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	ds "github.com/ipfs/go-datastore"
	badger "github.com/ipfs/go-ds-badger"
	crdt "github.com/ipfs/go-ds-crdt"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/assert"
)

func createTestNode() (*P2PNode, *crdt.Datastore) {
	privateKey, _, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)

	dir, err := os.MkdirTemp("", "globaldb-example")
	if err != nil {
		panic(err)
	}

	store, err := badger.NewDatastore(dir, &badger.DefaultOptions)
	if err != nil {
		panic(err)
	}

	defer os.RemoveAll(dir)

	// datastore := ds.NewMapDatastore()
	crdtDatastore, p2pNode := newCRDTDatastore(privateKey, "4000", "test-topic", store, ds.NewKey("/test-namespace"))

	return p2pNode, crdtDatastore
}

func TestHealthCheck(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "/health", http.NoBody)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "OK", resp.Body.String())
}

func TestPutAndGetValue(t *testing.T) {
	p2pNode, crdtDatastore := createTestNode()
	defer p2pNode.Cancel()

	router := setupRouter(crdtDatastore, p2pNode)

	key := "test-key"
	value := "test-value"

	encodedValue := base64.StdEncoding.EncodeToString([]byte(value))
	vr := ValueRequest{Value: encodedValue}
	body, _ := json.Marshal(vr)

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "POST", "/"+key, bytes.NewBuffer(body))
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	// Test GET value
	req, _ = http.NewRequestWithContext(ctx, "GET", "/"+key, http.NoBody)
	resp = httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	var result ValueResponse

	json.NewDecoder(resp.Body).Decode(&result)

	decodedValue, _ := base64.StdEncoding.DecodeString(result.Value)
	assert.Equal(t, value, string(decodedValue))
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestDeleteValue(t *testing.T) {
	p2pNode, crdtDatastore := createTestNode()
	defer p2pNode.Cancel()

	router := setupRouter(crdtDatastore, p2pNode)

	key := "test-key"
	value := "test-value"

	encodedValue := base64.StdEncoding.EncodeToString([]byte(value))
	vr := ValueRequest{Value: encodedValue}
	body, _ := json.Marshal(vr)

	// Put value first
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "POST", "/"+key, bytes.NewBuffer(body))
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	// Test DELETE value
	req, _ = http.NewRequestWithContext(ctx, "DELETE", "/"+key, http.NoBody)
	resp = httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)

	// Test GET value after delete to ensure it's gone
	req, _ = http.NewRequestWithContext(ctx, "GET", "/"+key, http.NoBody)
	resp = httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusNotFound, resp.Code)
}
