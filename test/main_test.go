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

// Test for Node adding a single key
func TestNodeAddSingleKey(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	key := "testN"
	value := "valueN"

	putKey(t, nc, key, value)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for Go adding a single key
func TestGoAddSingleKey(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	key := "testG"
	value := "valueG"

	putKey(t, gc, key, value)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for Node deleting a single key
func TestNodeAddDeleteKey(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	key := "testND"
	value := "valueND"

	putKey(t, nc, key, value)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")

	deleteKey(t, nc, key)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateNoKey(t, nc, key), "Key found in Node container")
		assert.True(c, validateNoKey(t, gc, key), "Key found in Go container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for Go deleting a single key
func TestGoAddDeleteKey(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	key := "testNG"
	value := "valueNG"

	putKey(t, gc, key, value)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyValue(t, gc, key, value), "Key not found in Go container")
		assert.True(c, validateKeyValue(t, nc, key, value), "Key not found in Node container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")

	deleteKey(t, gc, key)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateNoKey(t, gc, key), "Key found in Go container")
		assert.True(c, validateNoKey(t, nc, key), "Key found in Node container")
	}, 20*time.Second, 100*time.Millisecond, "Key not found in containers")
}

// Test for Node adding multiple keys
func TestNodeAddMultipleKeys(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	for i := 0; i < 100; i++ {
		putKey(t, nc, strconv.Itoa(i), strconv.Itoa(i))
	}

	// Validate the key exists in both containers
	var wg sync.WaitGroup

	wg.Add(2)

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

// Test for Go adding multiple keys
func TestGoAddMultipleKeys(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	numReq := 100

	for i := 0; i < numReq; i++ {
		putKey(t, gc, strconv.Itoa(i), strconv.Itoa(i))
	}

	// Validate the key exists in both containers
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := 0; i < numReq; i++ {
			assert.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.True(
					c,
					validateKeyValue(t, nc, strconv.Itoa(i), strconv.Itoa(i)),
					"Key %s not found in Node container", strconv.Itoa(i),
				)
			}, 20*time.Second, 100*time.Millisecond, "Key %s not found in Node container", strconv.Itoa(i))
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < numReq; i++ {
			assert.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.True(
					c,
					validateKeyValue(t, gc, strconv.Itoa(i), strconv.Itoa(i)),
					"Key %s not found in Go container", strconv.Itoa(i),
				)
			}, 20*time.Second, 100*time.Millisecond, "Key %s not found in Go container", strconv.Itoa(i))
		}
	}()

	wg.Wait()
}

// Test for conflict resolution
func TestConflictResolution(t *testing.T) {
	t.Parallel()
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

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyConsistency(t, nc, gc, key))
	}, 10*time.Second, 100*time.Millisecond, "Keys did not converge")
}

// Test for Node network partition
func TestNodeNetworkPartition(t *testing.T) {
	t.Parallel()
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

// Test for Go network partition
func TestGoNetworkPartition(t *testing.T) {
	t.Parallel()
	nc, gc := setupTestEnvironment(t, false)

	key := "partitionKey"
	valueBeforePartition := "valueBeforePartition"
	valueAfterPartition := "valueAfterPartition"

	putKey(t, nc, key, valueBeforePartition)

	// Simulate network partition by stopping the Go container
	err := nc.Stop(context.Background(), nil)
	require.NoError(t, err)

	// Perform updates on the Go container while Node is partitioned
	putKey(t, gc, key, valueAfterPartition)

	// Restart Node container to simulate network reconnection
	err = nc.Start(context.Background())
	require.NoError(t, err)

	connectHosts(t, nc, gc)

	// Validate that Go catches up with the changes
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, validateKeyConsistency(t, nc, gc, key))
	}, 10*time.Second, 100*time.Millisecond, "Keys did not converge")
}
