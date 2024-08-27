package main_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

type StdoutLogConsumer struct{}

type valueResponse struct {
	Value string `json:"value"`
}

// Accept prints the log to stdout
func (lc *StdoutLogConsumer) Accept(l testcontainers.Log) {
	fmt.Print(string(l.Content))
}

// Test for node adding a single key
func TestNodeAddSingleKey(t *testing.T) {
	_, nodeHost, nodeHttpPort, _, goHost, goHttpPort := setupTestEnvironment(t, false)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(value)),
	})

	// Validate the key exists in both containers
	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), value)
	}), "Key not found in Node container")

	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key), value)
	}), "Key not found in Go container")
}

// Test for go adding a single key
func TestGoAddSingleKey(t *testing.T) {
	_, nodeHost, nodeHttpPort, _, goHost, goHttpPort := setupTestEnvironment(t, false)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key), map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(value)),
	})

	// Validate the key exists in both containers
	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key), value)
	}), "Key not found in Go container")

	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), value)
	}), "Key not found in Node container")
}

// Test for deleting a single key
func TestNodeAddDeleteKey(t *testing.T) {
	_, nodeHost, nodeHttpPort, _, goHost, goHttpPort := setupTestEnvironment(t, true)

	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(value)),
	})

	// Validate the key exists in both containers
	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), value)
	}), "Key not found in Node container")

	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key), value)
	}), "Key not found in Go container")

	// Delete the key
	deleteKey(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key))

	// Validate the key is deleted in both containers
	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		res, err := validateNoKey(fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key))
		if err != nil || !res {
			return false
		}

		return true
	}), "Key not deleted in Node container")

	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		res, err := validateNoKey(fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key))
		if err != nil || !res {
			fmt.Println("Error:", err)
			return false
		}

		return true
	}), "Key not deleted in Go container")
}

// Test for adding multiple keys
func TestAddMultipleKeys(t *testing.T) {
	_, nodeHost, nodeHttpPort, _, goHost, goHttpPort := setupTestEnvironment(t, false)

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		// Send the HTTP request with base64 encoded value
		doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), strconv.Itoa(i)), map[string]string{
			"value": base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(i))),
		})
	}

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := 0; i < 100; i++ {
			// Validate the key exists in both containers
			assert.NoError(t, waitForCondition(10*time.Second, func() bool {
				return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), strconv.Itoa(i)), strconv.Itoa(i))
			}), fmt.Sprintf("Key %s not found in Node container", strconv.Itoa(i)))
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 100; i++ {
			assert.NoError(t, waitForCondition(10*time.Second, func() bool {
				return validateKeyExists(fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), strconv.Itoa(i)), strconv.Itoa(i))
			}), fmt.Sprintf("Key %s not found in Go container", strconv.Itoa(i)))
		}
	}()

	wg.Wait()
}

func TestConflictResolution(t *testing.T) {
	_, nodeHost, nodeHttpPort, _, goHost, goHttpPort := setupTestEnvironment(t, false)

	key := "conflictKey"
	valueNode := "nodeValue"
	valueGo := "goValue"

	// Simulate concurrent updates
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), map[string]string{
			"value": base64.StdEncoding.EncodeToString([]byte(valueNode)),
		})
	}()

	go func() {
		defer wg.Done()
		doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key), map[string]string{
			"value": base64.StdEncoding.EncodeToString([]byte(valueGo)),
		})
	}()

	wg.Wait()

	// Validate that both containers converge to the same value
	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		return validateKeyConsistency(t, nodeHost, nodeHttpPort, goHost, goHttpPort, key)
	}), "Keys did not converge")
}

func TestNetworkPartition(t *testing.T) {
	_, nodeHost, nodeHttpPort, goContainer, goHost, goHttpPort := setupTestEnvironment(t, false)

	key := "partitionKey"
	valueBeforePartition := "valueBeforePartition"
	valueAfterPartition := "valueAfterPartition"

	doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(valueBeforePartition)),
	})

	// Simulate network partition by stopping the Go container
	err := goContainer.Stop(context.Background(), nil)
	require.NoError(t, err)

	// Perform updates on the Node container while Go is partitioned
	doPost(t, fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(valueAfterPartition)),
	})

	// Restart Go container to simulate network reconnection
	err = goContainer.Start(context.Background())
	require.NoError(t, err)

	// Validate that Go catches up with the changes
	validateKeyConsistency(t, nodeHost, nodeHttpPort, goHost, goHttpPort, key)
}

// Setup function to initialize the environment
func setupTestEnvironment(t *testing.T, withLogging bool) (testcontainers.Container, string, nat.Port, testcontainers.Container, string, nat.Port) {
	ctx := context.Background()

	// Create a new network
	newNetwork, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, newNetwork.Remove(ctx)) })

	networkName := newNetwork.Name

	// Start the node ontainer
	nodeContainer, err := startNodeContainer(ctx, networkName, withLogging)
	require.NoError(t, err)
	t.Cleanup(func() { nodeContainer.Terminate(ctx) })

	// Start the go container
	goContainer, err := startGoContainer(ctx, networkName, withLogging)
	require.NoError(t, err)
	t.Cleanup(func() { goContainer.Terminate(ctx) })

	nodeHost, _ := nodeContainer.Host(ctx)
	nodeHttpPort, _ := nodeContainer.MappedPort(ctx, "3000/tcp")

	goHost, _ := goContainer.Host(ctx)
	goHttpPort, _ := goContainer.MappedPort(ctx, "8000/tcp")

	// Establish connection between the containers
	goContainerIP, _ := goContainer.ContainerIP(ctx)
	doPost(t, fmt.Sprintf("http://%s/connect", net.JoinHostPort(nodeHost, nodeHttpPort.Port())), map[string]string{
		"ma": fmt.Sprintf("/ip4/%s/tcp/4000/p2p/12D3KooWRC1cNip3xyDwzxrCryQ3V7bCsVF6Q3Nvh4o2CBSFpEmR", goContainerIP),
	})

	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		subs, err := getSubscribers(fmt.Sprintf("http://%s/subscribers", net.JoinHostPort(nodeHost, nodeHttpPort.Port())))
		if err != nil {
			return false
		}

		if len(subs) == 0 {
			return false
		}

		t.Logf("Node subscribers: %+v", subs)

		return true
	}), "No subscribers in Node container")

	require.NoError(t, waitForCondition(10*time.Second, func() bool {
		subs, err := getSubscribers(fmt.Sprintf("http://%s/subscribers", net.JoinHostPort(goHost, goHttpPort.Port())))
		if err != nil {
			return false
		}

		if len(subs) == 0 {
			return false
		}

		t.Logf("Go subscribers: %+v", subs)

		return true
	}), "No subscribers in Go container")

	return nodeContainer, nodeHost, nodeHttpPort, goContainer, goHost, goHttpPort
}

func startNodeContainer(ctx context.Context, networkName string, withLogging bool) (testcontainers.Container, error) {
	env := map[string]string{
		"HTTP_PORT":   "3000",
		"HTTP_HOST":   "0.0.0.0",
		"LIBP2P_HOST": "0.0.0.0",
		"LIBP2P_PORT": "6000",
		"DEBUG":       "*",
	}

	if withLogging {
		env["DEBUG"] = "*"
	}

	return startContainer(
		ctx,
		networkName,
		"js-crdt:latest",
		env,
		[]string{
			"3000/tcp",
			"6000/tcp",
		},
		"3000",
		withLogging,
	)
}

func startGoContainer(ctx context.Context, networkName string, withLogging bool) (testcontainers.Container, error) {
	return startContainer(
		ctx,
		networkName,
		"go-crdt:latest",
		map[string]string{
			"PRIVATE_KEY": "08011240be5d8bf7971c9a5b01892cf7ff5603f735a92a94623903787c15e994584eab9ce46ad62ac53a9db270ffbe03073be479f6ffc123270fc54c712a98022c8050bc",
		},
		[]string{
			"8000/tcp",
			"4000/tcp",
		},
		"8000",
		withLogging,
	)
}

func startContainer(ctx context.Context, networkName, image string, envVars map[string]string, exposedPorts []string, healthPort string, withLogging bool) (testcontainers.Container, error) {

	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: exposedPorts,
		Env:          envVars,
		WaitingFor:   wait.ForHTTP("/health").WithPort(nat.Port(healthPort)),
		Networks:     []string{networkName},
	}

	if withLogging {
		g := &StdoutLogConsumer{}

		req.LogConsumerCfg = &testcontainers.LogConsumerConfig{
			Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
			Consumers: []testcontainers.LogConsumer{g},
		}
	}

	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
}

func doPost(t *testing.T, url string, data map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jsonData, err := json.Marshal(data)
	assert.NoError(t, err, "Failed to marshal JSON") //nolint:testifylint // runs in goroutine

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	assert.NoError(t, err, "Failed to create HTTP request") //nolint:testifylint // runs in goroutine

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	assert.NoError(t, err, "Failed to send HTTP request") //nolint:testifylint // runs in goroutine
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "HTTP request failed")
}

func deleteKey(t *testing.T, url string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, http.NoBody)
	assert.NoError(t, err, "Failed to create HTTP request") //nolint:testifylint // runs in goroutine

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	assert.NoError(t, err, "Failed to send HTTP request") //nolint:testifylint // runs in goroutine
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "HTTP request failed")
}

func validateKeyExists(url, expectedValue string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return false
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	actualValue := &valueResponse{}

	err = json.Unmarshal(body, actualValue)
	if err != nil {
		return false
	}

	// Decode the expected value and compare with the actual value
	expectedDecodedValue, err := base64.StdEncoding.DecodeString(actualValue.Value)
	if err != nil {
		return false
	}

	return string(expectedDecodedValue) == expectedValue
}

func validateNoKey(url string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return false, err
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return true, nil
}

func validateKeyConsistency(t *testing.T, nodeHost string, nodeHttpPort nat.Port, goHost string, goHttpPort nat.Port, key string) bool {
	t.Helper() // Marks this function as a helper, so the line number in the test output is correct

	// Get the value from the Node.js datastore
	nodeURL := fmt.Sprintf("http://%s/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key)

	nodeValue, err := getValueFromDatastore(nodeURL)
	if err != nil {
		return false
	}

	// Get the value from the Go datastore
	goURL := fmt.Sprintf("http://%s/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key)

	goValue, err := getValueFromDatastore(goURL)
	if err != nil {
		return false
	}

	if nodeValue != goValue {
		return false
	}

	return true
}

// Helper function to retrieve the value from a datastore
func getValueFromDatastore(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read HTTP response body: %w", err)
	}

	var valueResp valueResponse

	err = json.Unmarshal(body, &valueResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	decodedValue, err := base64.StdEncoding.DecodeString(valueResp.Value)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 value: %w", err)
	}

	return string(decodedValue), nil
}

// getSubscribers retrieves the list of subscribers (peer IDs) from the given URL.
func getSubscribers(url string) ([]string, error) {
	// Create a context with a timeout to avoid hanging requests
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a new HTTP request with the given URL
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Initialize the HTTP client
	client := &http.Client{}

	// Send the HTTP request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check if the response status code is 200 OK
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse the response body
	var peerIDs []string
	if err := json.NewDecoder(resp.Body).Decode(&peerIDs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	// Return the list of peer IDs
	return peerIDs, nil
}

func waitForCondition(timeout time.Duration, condition func() bool) error { //nolint:unparam // ignore
	timeoutChan := time.After(timeout)
	tick := time.Tick(100 * time.Millisecond) // Check every 100ms to account for replication lag

	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("condition not met within timeout")
		case <-tick:
			if condition() {
				return nil
			}
		}
	}
}
