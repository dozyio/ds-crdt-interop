package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	ipfslite "github.com/hsanjuan/ipfs-lite"
	ds "github.com/ipfs/go-datastore"
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
	PubsubCancel context.CancelFunc
}

var (
	logger = logging.Logger("crdt-interop")
)

func AddressesWithPeerID(h host.Host) string {
	addresses := ""
	for _, addr := range h.Addrs() {
		addresses += "'" + addr.String() + "/p2p/" + h.ID().String() + "',\n"
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
	n := createNode(privateKey, port, topic, datastore)

	opts := crdt.DefaultOptions()
	opts.Logger = logger
	opts.RebroadcastInterval = 5 * time.Second
	opts.PutHook = func(k ds.Key, v []byte) {
		fmt.Printf("Added: [%s] -> %s\n", k, string(v))
	}
	opts.DeleteHook = func(k ds.Key) {
		fmt.Printf("Removed: [%s]\n", k)
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

	listen, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/" + port)
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
	//
	// pubsubTopic, err := psub.Join(topic)
	// if err != nil {
	// 	logger.Fatal(err)
	// }
	//
	// subscription, err := pubsubTopic.Subscribe()
	// if err != nil {
	// 	logger.Fatal(err)
	// }
	//
	// go func() {
	// 	for {
	// 		msg, err2 := subscription.Next(ctx)
	// 		if err2 != nil {
	// 			fmt.Println(err2)
	// 			break
	// 		}
	//
	// 		h.ConnManager().TagPeer(msg.ReceivedFrom, "keep", 100)
	// 	}
	// }()

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

	_, p2pNode := newCRDTDatastore(privateKey, "4000", "crdt-interop", ds.NewMapDatastore(), ds.NewKey("/crdt-interop"))

	fmt.Printf("Libp2p running on %s\n", AddressesWithPeerID(p2pNode.H))

	// http interface to add/delete keys / dag export
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
