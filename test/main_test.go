package main_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test for node adding a single key
func TestNodeAddSingleKey(t *testing.T) {
	nc, gc := setupTestEnvironment(t, false)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	putKey(t, nc, key, value)

	// Validate the key exists in both containers
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for go adding a single key
func TestGoAddSingleKey(t *testing.T) {
	nc, gc := setupTestEnvironment(t, false)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	putKey(t, gc, key, value)

	// Validate the key exists in both containers
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for node deleting a single key
func TestNodeAddDeleteKey(t *testing.T) {
	nc, gc := setupTestEnvironment(t, false)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	putKey(t, nc, key, value)

	// Validate the key exists in both containers
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
	}, 10*time.Second, 100*time.Millisecond, "Key not found in containers")

	// Delete the key
	deleteKey(t, nc, key)

	// Validate the key is deleted in both containers
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateNoKey(t, gc, key), "Key found in Go container")
		assert.True(c, validateNoKey(t, nc, key), "Key found in Node container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for go deleting a single key
func TestGoAddDeleteKey(t *testing.T) {
	nc, gc := setupTestEnvironment(t, true)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	putKey(t, gc, key, value)

	// Validate the key exists in both containers
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
	}, 10*time.Second, 100*time.Millisecond, "Key not found in containers")

	// Delete the key
	deleteKey(t, gc, key)

	// Validate the key is deleted in both containers
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateNoKey(t, gc, key), "Key found in Go container")
		assert.True(c, validateNoKey(t, nc, key), "Key found in Node container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for adding multiple keys
func TestAddMultipleKeys(t *testing.T) {
	nc, gc := setupTestEnvironment(t, false)

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		// Send the HTTP request with base64 encoded value
		putKey(t, nc, strconv.Itoa(i), strconv.Itoa(i))
	}

	wg.Add(2)

	// Validate the key exists in both containers
	go func() {
		defer wg.Done()

		for i := 0; i < 100; i++ {
			assert.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.True(
					c,
					validateKeyValue(t, nc, strconv.Itoa(i), strconv.Itoa(i)),
					"Key %s not found in Node container", strconv.Itoa(i),
				)
			}, 10*time.Second, 100*time.Millisecond, "Key %s not found in Node container", strconv.Itoa(i))
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 100; i++ {
			assert.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.True(
					c,
					validateKeyValue(t, gc, strconv.Itoa(i), strconv.Itoa(i)),
					"Key %s not found in Go container", strconv.Itoa(i),
				)
			}, 10*time.Second, 100*time.Millisecond, "Key %s not found in Go container", strconv.Itoa(i))
		}
	}()

	wg.Wait()
}

func TestConflictResolution(t *testing.T) {
	nc, gc := setupTestEnvironment(t, false)

	key := "conflictKey"
	valueNode := "nodeValue"
	valueGo := "goValue"

	// Simulate concurrent updates
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		putKey(t, nc, key, valueNode)
	}()

	go func() {
		defer wg.Done()
		putKey(t, gc, key, valueGo)
	}()

	wg.Wait()

	// Validate that both containers converge to the same value
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyConsistency(t, nc, gc, key))
	}, 10*time.Second, 100*time.Millisecond, "Keys did not converge")
}

func TestNetworkPartition(t *testing.T) {
	nc, gc := setupTestEnvironment(t, false)

	key := "partitionKey"
	valueBeforePartition := "valueBeforePartition"
	valueAfterPartition := "valueAfterPartition"

	putKey(t, nc, key, valueBeforePartition)

	// Simulate network partition by stopping the Go container
	err := gc.Stop(context.Background(), nil)
	require.NoError(t, err)

	// Perform updates on the Node container while Go is partitioned
	putKey(t, nc, key, valueAfterPartition)

	// Restart Go container to simulate network reconnection
	err = gc.Start(context.Background())
	require.NoError(t, err)

	connectHosts(t, nc, gc)

	// Validate that Go catches up with the changes
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyConsistency(t, nc, gc, key))
	}, 10*time.Second, 100*time.Millisecond, "Keys did not converge")
}
