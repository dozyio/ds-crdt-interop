package main_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	nodeHttpPort         = "3000"
	nodeMappedHttpPort   = "3000/tcp"
	nodeLibp2pPort       = "6000"
	nodeMappedLibp2pPort = "6000/tcp"
	goHttpPort           = "8000"
	goMappedHttpPort     = "8000/tcp"
	goLibp2pPort         = "4000"
	goMappedLibp2pPort   = "4000/tcp"
	goPrivateKey         = "08011240be5d8bf7971c9a5b01892cf7ff5603f735a92a94623903787c15e994584eab9ce46ad62ac53a9db270ffbe03073be479f6ffc123270fc54c712a98022c8050bc" //nolint:lll // ignore
	goPeerId             = "12D3KooWRC1cNip3xyDwzxrCryQ3V7bCsVF6Q3Nvh4o2CBSFpEmR"
)

var (
	errStatusNotFound = errors.New("status code 404")
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
func setupTestEnvironment( //nolint:ireturn // ignore
	t *testing.T,
	withLogging bool,
) (nodeContainer, goContainer testcontainers.Container) {
	ctx := context.Background()

	// Create a new network
	newNetwork, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, newNetwork.Remove(ctx)) })

	networkName := newNetwork.Name

	// Start the node ontainer
	nodeContainer, err = startNodeContainer(ctx, networkName, withLogging)
	require.NoError(t, err)
	t.Cleanup(func() { nodeContainer.Terminate(ctx) })

	// Start the go container
	goContainer, err = startGoContainer(ctx, networkName, withLogging)
	require.NoError(t, err)
	t.Cleanup(func() { goContainer.Terminate(ctx) })

	// Establish connection between the containers
	connectHosts(t, nodeContainer, goContainer)

	return nodeContainer, goContainer
}

func startNodeContainer( //nolint:ireturn // ignore
	ctx context.Context,
	networkName string,
	withLogging bool,
) (testcontainers.Container, error) {
	env := map[string]string{
		"TYPE":               "node",
		"HTTP_PORT":          nodeHttpPort,
		"HTTP_PORT_MAPPED":   nodeMappedHttpPort,
		"HTTP_HOST":          "0.0.0.0",
		"LIBP2P_HOST":        "0.0.0.0",
		"LIBP2P_PORT":        nodeLibp2pPort,
		"LIBP2P_PORT_MAPPED": nodeMappedLibp2pPort,
	}

	// if withLogging {
	// 	env["DEBUG"] = "crdt:pubsub*"
	// }

	return startContainer(
		ctx,
		networkName,
		"js-crdt:latest",
		env,
		[]string{
			nodeMappedHttpPort,
			nodeMappedLibp2pPort,
		},
		nodeHttpPort,
		withLogging,
	)
}

func startGoContainer( //nolint:ireturn // ignore
	ctx context.Context,
	networkName string,
	withLogging bool,
) (testcontainers.Container, error) {
	env := map[string]string{
		"TYPE":               "go",
		"HTTP_PORT":          goHttpPort,
		"HTTP_PORT_MAPPED":   goMappedHttpPort,
		"HTTP_HOST":          "0.0.0.0",
		"LIBP2P_HOST":        "0.0.0.0",
		"LIBP2P_PORT":        goLibp2pPort,
		"LIBP2P_PORT_MAPPED": goMappedLibp2pPort,
		"PRIVATE_KEY":        goPrivateKey,
	}

	if withLogging {
		env["GOLOG_LOG_LEVEL"] = "ERROR"
	}

	return startContainer(
		ctx,
		networkName,
		"go-crdt:latest",
		env,
		[]string{
			goMappedHttpPort,
			goMappedLibp2pPort,
		},
		goHttpPort,
		withLogging,
	)
}

func startContainer( //nolint:ireturn // ignore
	ctx context.Context,
	networkName string,
	image string,
	envVars map[string]string,
	exposedPorts []string,
	healthPort string,
	withLogging bool,
) (testcontainers.Container, error) {
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
	t.Helper()

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

func putKey(t *testing.T, c testcontainers.Container, key, value string) {
	t.Helper()

	url := baseUrl(c) + key

	doPost(t, url, map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte(value)),
	})
}

func deleteKey(t *testing.T, c testcontainers.Container, key string) {
	t.Helper()

	url := baseUrl(c) + key

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

// Helper function to retrieve the value from a datastore
func getKey(url string) (*string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errStatusNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTTP response body: %w", err)
	}

	var valueResp valueResponse

	err = json.Unmarshal(body, &valueResp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON response: %w", err)
	}

	decodedValue, err := base64.StdEncoding.DecodeString(valueResp.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 value: %w", err)
	}

	v := string(decodedValue)

	return &v, nil
}

func baseUrl(c testcontainers.Container) string {
	cInfo, err := c.Inspect(context.Background())
	if err != nil {
		panic(err)
	}

	httpPort := ""

	for _, item := range cInfo.Config.Env {
		if strings.HasPrefix(item, "HTTP_PORT=") {
			httpPort = strings.TrimPrefix(item, "HTTP_PORT=")
		}
	}

	h, err := c.Host(context.Background())
	if err != nil {
		panic(err)
	}

	nPort, err := nat.NewPort("tcp", httpPort)
	if err != nil {
		panic(err)
	}

	p, err := c.MappedPort(context.Background(), nPort)
	if err != nil {
		panic(err)
	}

	return fmt.Sprintf("http://%s/", net.JoinHostPort(h, p.Port()))
}

func connectHosts(t *testing.T, nc, gc testcontainers.Container) {
	t.Helper()

	url := baseUrl(nc) + "connect"

	goContainerIP, err := gc.ContainerIP(context.Background())
	if err != nil {
		panic(err)
	}

	doPost(t, url, map[string]string{
		"ma": fmt.Sprintf("/ip4/%s/tcp/%s/p2p/%s", goContainerIP, goLibp2pPort, goPeerId),
	})

	nodeSubscribersUrl := baseUrl(nc) + "subscribers"
	goSubscribersUrl := baseUrl(gc) + "subscribers"

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, hasSubscribers(t, nodeSubscribersUrl), "No subscribers in Node container")
		assert.True(c, hasSubscribers(t, goSubscribersUrl), "No subscribers in Go container")
	}, 10*time.Second, 100*time.Millisecond, "Key not found in containers")
}

func validateKeyValue(t *testing.T, c testcontainers.Container, key, expectedValue string) bool {
	t.Helper()

	url := baseUrl(c) + key

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	assert.NoError(t, err, "Failed to create HTTP request %s", err) //nolint:testifylint // runs in goroutine

	client := &http.Client{}

	resp, err := client.Do(req)
	assert.NoError(t, err, "Failed to send HTTP request %s", err) //nolint:testifylint // runs in goroutine

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err, "Failed to read HTTP response body %s", err) //nolint:testifylint // runs in goroutine

	actualValue := &valueResponse{}

	err = json.Unmarshal(body, actualValue)
	assert.NoError(t, err, "Failed to unmarshal JSON response %s", err) //nolint:testifylint // runs in goroutine

	// Decode the expected value and compare with the actual value
	expectedDecodedValue, err := base64.StdEncoding.DecodeString(actualValue.Value)
	assert.NoError(t, err, "Failed to decode base64 value %s", err) //nolint:testifylint // runs in goroutine

	return string(expectedDecodedValue) == expectedValue
}

func validateNoKey(t *testing.T, c testcontainers.Container, key string) bool {
	t.Helper()

	url := baseUrl(c) + key

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	assert.NoError(t, err, "Failed to create HTTP request %s", err) //nolint:testifylint // runs in goroutine

	client := &http.Client{}

	resp, err := client.Do(req)
	assert.NoError(t, err, "Failed to send HTTP request %s", err) //nolint:testifylint // runs in goroutine

	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()

	return resp.StatusCode == http.StatusNotFound
}

func validateKeyConsistency(t *testing.T, nc, gc testcontainers.Container, key string) bool {
	t.Helper()

	nodeURL := baseUrl(nc) + key
	goURL := baseUrl(gc) + key

	nodeValue, err := getKey(nodeURL)
	if err != nil {
		if !errors.Is(err, errStatusNotFound) {
			return false
		}
	}

	if nodeValue != nil {
		fmt.Printf("Node value: %s %s\n", key, *nodeValue)
	} else {
		fmt.Printf("Node value: %s nil\n", key)
	}

	// Get the value from the Go datastore

	goValue, err := getKey(goURL)
	if err != nil {
		if !errors.Is(err, errStatusNotFound) {
			return false
		}
	}

	if goValue != nil {
		fmt.Printf("Go value: %s %s\n", key, *goValue)
	} else {
		fmt.Printf("Go value: %s nil\n", key)
	}

	if nodeValue == nil && goValue == nil {
		return true
	}

	if (nodeValue == nil && goValue != nil) || (nodeValue != nil && goValue == nil) {
		return false
	}

	if *nodeValue != *goValue {
		return false
	}

	return true
}

func hasSubscribers(t *testing.T, url string) bool {
	t.Helper()

	subs, err := getSubscribers(t, url)
	if err != nil {
		return false
	}

	if len(subs) == 0 {
		return false
	}

	return true
}

// getSubscribers retrieves the list of subscribers (peer IDs) from the given URL.
func getSubscribers(t *testing.T, url string) ([]string, error) {
	t.Helper()

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
