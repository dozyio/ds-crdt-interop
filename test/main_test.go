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

// Setup function to initialize the environment
func setupTestEnvironment(t *testing.T) (string, nat.Port, string, nat.Port) {
	ctx := context.Background()

	// Create a new network
	newNetwork, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, newNetwork.Remove(ctx)) })

	networkName := newNetwork.Name

	// Start the node ontainer
	nodeContainer, err := startNodeContainer(ctx, networkName)
	require.NoError(t, err)
	t.Cleanup(func() { nodeContainer.Terminate(ctx) })

	// Start the go container
	goContainer, err := startGoContainer(ctx, networkName)
	require.NoError(t, err)
	t.Cleanup(func() { goContainer.Terminate(ctx) })

	nodeHost, _ := nodeContainer.Host(ctx)
	nodeHttpPort, _ := nodeContainer.MappedPort(ctx, "3000/tcp")

	goHost, _ := goContainer.Host(ctx)
	goHttpPort, _ := goContainer.MappedPort(ctx, "8000/tcp")

	// Establish connection between the containers
	goContainerIP, _ := goContainer.ContainerIP(ctx)
	sendPostRequest(t, fmt.Sprintf("http://%s/connect", net.JoinHostPort(nodeHost, nodeHttpPort.Port())), map[string]string{
		"ma": fmt.Sprintf("/ip4/%s/tcp/4000/p2p/12D3KooWRC1cNip3xyDwzxrCryQ3V7bCsVF6Q3Nvh4o2CBSFpEmR", goContainerIP),
	})

	return nodeHost, nodeHttpPort, goHost, goHttpPort
}

// Test for adding a single key
func TestAddSingleKey(t *testing.T) {
	nodeHost, nodeHttpPort, goHost, goHttpPort := setupTestEnvironment(t)

	// Add a single key-value pair
	key := "test1"
	value := "value1"

	// Send the HTTP request with base64 encoded value
	sendPostRequest(t, fmt.Sprintf("http://%s/key/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(value)),
	})

	// Validate the key exists in both containers
	require.NoError(t, waitForCondition(5*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/key/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), key), value)
	}), "Key not found in Node container")

	require.NoError(t, waitForCondition(5*time.Second, func() bool {
		return validateKeyExists(fmt.Sprintf("http://%s/key/%s", net.JoinHostPort(goHost, goHttpPort.Port()), key), value)
	}), "Key not found in Go container")
}

// Test for adding multiple keys
func TestAddMultipleKeys(t *testing.T) {
	nodeHost, nodeHttpPort, goHost, goHttpPort := setupTestEnvironment(t)

	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		// Send the HTTP request with base64 encoded value
		sendPostRequest(t, fmt.Sprintf("http://%s/key/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), strconv.Itoa(i)), map[string]string{
			"value": base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(i))),
		})
	}

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := 0; i < 1000; i++ {
			// Validate the key exists in both containers
			assert.NoError(t, waitForCondition(5*time.Second, func() bool {
				return validateKeyExists(fmt.Sprintf("http://%s/key/%s", net.JoinHostPort(nodeHost, nodeHttpPort.Port()), strconv.Itoa(i)), strconv.Itoa(i))
			}), fmt.Sprintf("Key %s not found in Node container", strconv.Itoa(i)))
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 1000; i++ {
			assert.NoError(t, waitForCondition(5*time.Second, func() bool {
				return validateKeyExists(fmt.Sprintf("http://%s/key/%s", net.JoinHostPort(goHost, goHttpPort.Port()), strconv.Itoa(i)), strconv.Itoa(i))
			}), fmt.Sprintf("Key %s not found in Go container", strconv.Itoa(i)))
		}
	}()

	wg.Wait()
}

// More specific tests can be added here using the same setup function

func startNodeContainer(ctx context.Context, networkName string) (testcontainers.Container, error) {
	return startContainer(
		ctx,
		networkName,
		"js-crdt:latest",
		map[string]string{
			"HTTP_PORT":   "3000",
			"HTTP_HOST":   "0.0.0.0",
			"LIBP2P_HOST": "0.0.0.0",
			"LIBP2P_PORT": "6000",
		},
		[]string{
			"3000/tcp",
			"6000/tcp",
		},
		"3000",
	)
}

func startGoContainer(ctx context.Context, networkName string) (testcontainers.Container, error) {
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
	)
}

func startContainer(ctx context.Context, networkName, image string, envVars map[string]string, exposedPorts []string, healthPort string) (testcontainers.Container, error) {
	g := &StdoutLogConsumer{}

	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: exposedPorts,
		Env:          envVars,
		WaitingFor:   wait.ForHTTP("/health").WithPort(nat.Port(healthPort)),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
			Consumers: []testcontainers.LogConsumer{g},
		},
		Networks: []string{networkName},
	}

	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
}

func sendPostRequest(t *testing.T, url string, data map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jsonData, err := json.Marshal(data)
	require.NoError(t, err, "Failed to marshal JSON")

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	require.NoError(t, err, "Failed to create HTTP request")

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	require.NoError(t, err, "Failed to send HTTP request")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "HTTP request failed")
	t.Logf("Request to %s successfully sent", url)
}

func validateKeyExists(url, expectedValue string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func waitForCondition(timeout time.Duration, condition func() bool) error {
	timeoutChan := time.After(timeout)
	tick := time.Tick(5 * time.Millisecond) // Check every 10ms to account for replication lag

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
