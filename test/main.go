package main

/*
import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"
)

func main() {
	var wg sync.WaitGroup

	// Create a context that will be cancelled on program termination or when one process fails
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure all processes are killed on exit

	// Channel to receive OS signals for termination (e.g., SIGINT)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Cancel context on receiving a termination signal
	go func() {
		<-sigChan
		cancel()
	}()

	// Channels to signal when each program has logged "Libp2p running on..."
	jsReady := make(chan string)
	goReady := make(chan string)
	testSuccess := make(chan struct{})

	key := "/key/test"
	value := "test"

	// Run the JS and Go processes
	wg.Add(2)
	go runProcess(ctx, cancel, &wg, "yarn", []string{"start"}, "../js", jsReady, nil, nil)
	go runProcess(ctx, cancel, &wg, "go", []string{"run", "."}, "../go", goReady, testSuccess, map[string]string{
		"PRIVATE_KEY": "08011240be5d8bf7971c9a5b01892cf7ff5603f735a92a94623903787c15e994584eab9ce46ad62ac53a9db270ffbe03073be479f6ffc123270fc54c712a98022c8050bc",
	}, key, value)

	// Wait for both programs to log "Libp2p running on..." and capture the multiaddrs
	<-jsReady
	goMultiaddr := <-goReady

	// Send the connect request with the Go multiaddr
	sendPostRequest("http://127.0.0.1:3000/connect", map[string]string{"ma": goMultiaddr})

	// Send the initial HTTP request with base64 encoded value
	sendPostRequest("http://127.0.0.1:3000"+key, map[string]string{"value": base64.StdEncoding.EncodeToString([]byte(value))})

	// Wait for the test to succeed
	select {
	case <-testSuccess:
		log.Printf("Test successful: The Go program added the key %s with value %s correctly.", key, value)
	case <-time.After(30 * time.Second):
		log.Printf("Test failed: The Go program did not add the key %s with value %s within the expected time.", key, value)
	}

	// Wait for all processes to finish
	wg.Wait()
}

func runProcess(ctx context.Context, cancelFunc context.CancelFunc, wg *sync.WaitGroup, command string, args []string, dir string, readyChan chan string, testChan chan struct{}, envVars map[string]string, keyAndValue ...string) {
	defer wg.Done()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir

	// Set environment variables if provided
	if envVars != nil {
		cmd.Env = append(os.Environ(), formatEnvVars(envVars)...)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("%s: Failed to capture stdout: %v", command, err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalf("%s: Failed to capture stderr: %v", command, err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("%s: Failed to start command: %v", command, err)
	}

	key := "/key/test"
	value := "test"
	if len(keyAndValue) == 2 {
		key = keyAndValue[0]
		value = keyAndValue[1]
	}

	go monitorOutput(command, stdout, readyChan, testChan, key, value)
	go monitorOutput(command, stderr, nil, nil, key, value)

	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("%s: Process exited with error: %v", command, err)
			cancelFunc() // Cancel all processes if one fails
		}
	}()
}

func formatEnvVars(envVars map[string]string) []string {
	var formatted []string
	for key, value := range envVars {
		formatted = append(formatted, key+"="+value)
	}
	return formatted
}

func monitorOutput(name string, pipe io.ReadCloser, readyChan chan string, testChan chan struct{}, key string, value string) {
	scanner := bufio.NewScanner(pipe)
	re := regexp.MustCompile(`Libp2p running on (.+)`)
	testRe := regexp.MustCompile(`Added: \[` + regexp.QuoteMeta(key) + `\] -> ` + regexp.QuoteMeta(value))

	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[%s]: %s", name, line)
		if readyChan != nil && re.MatchString(line) {
			multiaddr := re.FindStringSubmatch(line)[1]
			readyChan <- multiaddr
			close(readyChan)
		}
		if testChan != nil && testRe.MatchString(line) {
			close(testChan)
		}
	}
}

func sendPostRequest(url string, data map[string]string) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatal("Failed to marshal JSON: ", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal("Failed to create HTTP request: ", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Failed to send HTTP request: ", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("HTTP request failed with status: %s", resp.Status)
	}

	log.Printf("Request to %s successfully sent", url)
}
*/
