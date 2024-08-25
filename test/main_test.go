package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

type LogConsumer interface {
	Accept(testcontainers.Log)
}

// StdoutLogConsumer is a LogConsumer that prints the log to stdout
type StdoutLogConsumer struct{}

// Accept prints the log to stdout
func (lc *StdoutLogConsumer) Accept(l testcontainers.Log) {
	fmt.Print(string(l.Content))
}

func TestInterop(t *testing.T) {
	ctx := context.Background()

	newNetwork, err := network.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		require.NoError(t, newNetwork.Remove(ctx))
	})

	networkName := newNetwork.Name

	// Start the yarn container
	yarnContainer, err := startYarnContainer(ctx, networkName)
	if err != nil {
		t.Fatalf("Failed to start yarn container: %v", err)
	}
	defer yarnContainer.Terminate(ctx)

	// Start the go container
	goContainer, err := startGoContainer(ctx, networkName)
	if err != nil {
		t.Fatalf("Failed to start go container: %v", err)
	}
	defer goContainer.Terminate(ctx)

	yarnHost, _ := yarnContainer.Host(ctx)
	// yarnContainerIP, _ := yarnContainer.ContainerIP(ctx)
	yarnHttpPort, _ := yarnContainer.MappedPort(ctx, "3000/tcp")

	goHost, _ := goContainer.Host(ctx)
	goContainerIP, _ := goContainer.ContainerIP(ctx)
	goHttpPort, _ := goContainer.MappedPort(ctx, "8000/tcp")

	// Send the connect request
	sendPostRequest(t, "http://"+yarnHost+":"+yarnHttpPort.Port()+"/connect", map[string]string{
		"ma": "/ip4/" + goContainerIP + "/tcp/4000/p2p/12D3KooWRC1cNip3xyDwzxrCryQ3V7bCsVF6Q3Nvh4o2CBSFpEmR",
	})

	// Send the initial HTTP request with base64 encoded value
	sendPostRequest(t, "http://"+yarnHost+":"+yarnHttpPort.Port()+"/key/test", map[string]string{
		"value": base64.StdEncoding.EncodeToString([]byte("test")),
	})

	// Validate that the key was added
	validateKeyAdded(t, "http://"+yarnHost+":"+yarnHttpPort.Port()+"/key/test", "test")
	time.Sleep(1 * time.Second) // Adjust this sleep time as necessary
	validateKeyAdded(t, "http://"+goHost+":"+goHttpPort.Port()+"/key/test", "test")
}

func startYarnContainer(ctx context.Context, networkName string) (testcontainers.Container, error) {
	g := &StdoutLogConsumer{}

	req := testcontainers.ContainerRequest{
		Image:        "js-crdt:latest", // Replace with the appropriate Node.js version
		ExposedPorts: []string{"3000/tcp", "6000/tcp"},
		Env: map[string]string{
			"HTTP_PORT":   "3000",
			"HTTP_HOST":   "0.0.0.0",
			"LIBP2P_HOST": "0.0.0.0",
			"LIBP2P_PORT": "6000",
		},
		WaitingFor: wait.ForHTTP("/health").WithPort("3000/tcp"),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
			Consumers: []testcontainers.LogConsumer{g},
		},
		Networks: []string{networkName},
	}

	yarnContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}

	return yarnContainer, nil
}

func startGoContainer(ctx context.Context, networkName string) (testcontainers.Container, error) {
	g := &StdoutLogConsumer{}

	req := testcontainers.ContainerRequest{
		Image:        "go-crdt:latest", // Replace with the appropriate Go version
		ExposedPorts: []string{"8000/tcp", "4000/tcp"},
		Env: map[string]string{
			"PRIVATE_KEY": "08011240be5d8bf7971c9a5b01892cf7ff5603f735a92a94623903787c15e994584eab9ce46ad62ac53a9db270ffbe03073be479f6ffc123270fc54c712a98022c8050bc",
		},
		Cmd:        []string{"go", "run", "."},
		WaitingFor: wait.ForHTTP("/health").WithPort("8000/tcp"),
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Opts:      []testcontainers.LogProductionOption{testcontainers.WithLogProductionTimeout(10 * time.Second)},
			Consumers: []testcontainers.LogConsumer{g},
		},
		Networks: []string{networkName},
	}

	goContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}

	return goContainer, nil
}

func sendPostRequest(t *testing.T, url string, data map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatal("Failed to marshal JSON: ", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatal("Failed to create HTTP request: ", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal("Failed to send HTTP request: ", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP request failed with status: %s", resp.Status)
	}

	t.Logf("Request to %s successfully sent", url)
}

func validateKeyAdded(t *testing.T, url, expectedValue string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create the GET request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		t.Fatalf("Failed to create GET request: %v", err)
	}

	client := &http.Client{}

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send GET request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	// Check if the expected value is in the response body
	if !strings.Contains(string(body), base64.StdEncoding.EncodeToString([]byte(expectedValue))) {
		t.Fatalf("Expected value %s not found in response: %s", expectedValue, string(body))
	}

	t.Logf("Validation successful, key found with value: %s", expectedValue)
}
