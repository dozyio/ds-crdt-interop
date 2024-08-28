package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ipfslite "github.com/hsanjuan/ipfs-lite"
	ds "github.com/ipfs/go-datastore"
	badger "github.com/ipfs/go-ds-badger"
	crdt "github.com/ipfs/go-ds-crdt"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"
)

type P2PNode struct {
	Cancel       context.CancelFunc
	H            host.Host
	Broadcaster  *crdt.PubSubBroadcaster
	DagSyncer    *ipfslite.Peer
	Datastore    ds.Datastore
	Pubsub       *pubsub.PubSub
	PubsubCancel context.CancelFunc
	Topic        string
}

type ValueRequest struct {
	Value string `json:"value"`
}

type ValueResponse struct {
	Value string `json:"value"`
}

type OkResponse struct {
	Success bool `json:"success"`
}

var (
	logger = logging.Logger("crdt")
)

func AddressesWithPeerID(h host.Host) string {
	addresses := ""
	for _, addr := range h.Addrs() {
		addresses += addr.String() + "/p2p/" + h.ID().String() + "\n"
	}

	return addresses
}

func newCRDTDatastore(
	privateKey crypto.PrivKey,
	port string,
	topic string,
	datastore ds.Batching,
	namespace ds.Key,
) (*crdt.Datastore, *P2PNode) {
	// _ = logging.SetLogLevel("*", "debug")
	// _ = logging.SetLogLevel("p2p-config", "info")
	n := createNode(privateKey, port, topic, datastore)

	opts := crdt.DefaultOptions()
	opts.Logger = logger
	opts.RebroadcastInterval = 5 * time.Second
	opts.PutHook = func(k ds.Key, v []byte) {
		fmt.Printf("Go Added: [%s] -> %s\n", k, string(v))
	}
	opts.DeleteHook = func(k ds.Key) {
		fmt.Printf("Go Removed: [%s]\n", k)
	}

	crdtDatastore, err := crdt.New(n.Datastore, namespace, n.DagSyncer, n.Broadcaster, opts)
	if err != nil {
		panic(err)
	}

	return crdtDatastore, n
}

func createNode(privateKey crypto.PrivKey, port, topic string, datastore ds.Batching) *P2PNode {
	p2pNode := &P2PNode{}

	ctx, cancel := context.WithCancel(context.Background())
	p2pNode.Cancel = cancel

	listen, err := multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/" + port)
	if err != nil {
		panic(err)
	}

	h, dht, err := ipfslite.SetupLibp2p(
		ctx,
		privateKey,
		nil,
		[]multiaddr.Multiaddr{listen},
		nil,
		ipfslite.Libp2pOptionsExtra...,
	)
	if err != nil {
		logger.Fatal(err)
	}

	p2pNode.H = h

	psub, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		logger.Fatal(err)
	}

	p2pNode.Pubsub = psub
	p2pNode.Topic = topic

	ipfs, err := ipfslite.New(ctx, datastore, nil, h, dht, nil)
	if err != nil {
		logger.Fatal(err)
	}

	p2pNode.DagSyncer = ipfs

	psubCtx, psubCancel := context.WithCancel(ctx)

	pubsubBC, err := crdt.NewPubSubBroadcaster(psubCtx, psub, topic)
	if err != nil {
		logger.Fatal(err)
	}

	p2pNode.Broadcaster = pubsubBC
	p2pNode.PubsubCancel = psubCancel
	p2pNode.Datastore = datastore

	return p2pNode
}

func main() {
	envKey := os.Getenv("PRIVATE_KEY")

	decodedKey, err := hex.DecodeString(envKey)
	if err != nil {
		panic(err)
	}

	privateKey, err := crypto.UnmarshalPrivateKey(decodedKey)
	if err != nil {
		panic(err)
	}

	// store := dssync.MutexWrap(ds.NewMapDatastore())

	dir, err := os.MkdirTemp("", "globaldb-example")
	if err != nil {
		panic(err)
	}

	store, err := badger.NewDatastore(dir, &badger.DefaultOptions)
	if err != nil {
		panic(err)
	}

	defer os.RemoveAll(dir)
	defer store.Close()

	crdtDatastore, p2pNode := newCRDTDatastore(privateKey, "4000", "crdt-interop", store, ds.NewKey("/crdt-interop"))

	fmt.Printf("Libp2p running on %s\n", AddressesWithPeerID(p2pNode.H))

	router := setupRouter(crdtDatastore, p2pNode)

	err = http.ListenAndServe(":8000", router)
	if err != nil {
		logger.Fatal(err)
	}

	sigChan := make(chan os.Signal, 1)

	signal.Notify(
		sigChan,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGHUP,
	)
	<-sigChan

	fmt.Println("Shutting down...")
}

// Refactor the router setup into its own function
func setupRouter(crdtDatastore *crdt.Datastore, p2pNode *P2PNode) *http.ServeMux {
	router := http.NewServeMux()

	router.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
	})

	router.HandleFunc("GET /subscribers", func(w http.ResponseWriter, r *http.Request) {
		peers := p2pNode.Pubsub.ListPeers(p2pNode.Topic)

		var peerIDs []string
		for _, p := range peers {
			peerIDs = append(peerIDs, p.String())
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(peerIDs); err != nil {
			http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
		}
	})

	router.HandleFunc("POST /{rest...}", func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("rest")
		handlePost(crdtDatastore, w, r, rest)
	})

	router.HandleFunc("GET /{rest...}", func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("rest")
		handleGet(crdtDatastore, w, r, rest)
	})

	router.HandleFunc("DELETE /{rest...}", func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("rest")
		handleDelete(crdtDatastore, w, r, rest)
	})

	return router
}

func handlePost(crdtDatastore *crdt.Datastore, w http.ResponseWriter, r *http.Request, key string) {
	decoder := json.NewDecoder(r.Body)

	var vr ValueRequest
	if err := decoder.Decode(&vr); err != nil {
		logger.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	value, err := base64.StdEncoding.DecodeString(vr.Value)
	if err != nil {
		logger.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	if err = crdtDatastore.Put(r.Context(), ds.NewKey(key), value); err != nil {
		logger.Error(err)
		http.Error(w, err.Error(), http.StatusNotFound)

		return
	}

	result := &OkResponse{Success: true}

	w.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		logger.Error(err)
	}
}

func handleGet(crdtDatastore *crdt.Datastore, w http.ResponseWriter, r *http.Request, key string) {
	value, err := crdtDatastore.Get(r.Context(), ds.NewKey(key))
	if err != nil || value == nil {
		http.Error(w, err.Error(), http.StatusNotFound)

		return
	}

	result := &ValueResponse{Value: base64.StdEncoding.EncodeToString(value)}

	w.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		logger.Error(err)
	}
}

func handleDelete(crdtDatastore *crdt.Datastore, w http.ResponseWriter, r *http.Request, key string) {
	if err := crdtDatastore.Delete(r.Context(), ds.NewKey(key)); err != nil {
		logger.Error(err)
		http.Error(w, err.Error(), http.StatusNotFound)

		return
	}

	result := &OkResponse{Success: true}

	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(result)
	if err != nil {
		logger.Error(err)
	}
}
