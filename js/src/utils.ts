import { gossipsub } from '@chainsafe/libp2p-gossipsub'
import { noise } from '@chainsafe/libp2p-noise'
import { yamux } from '@chainsafe/libp2p-yamux'
import { identify } from '@libp2p/identify'
import { prefixLogger } from '@libp2p/logger'
import { tcp } from '@libp2p/tcp'
import { createHelia, type HeliaLibp2p } from 'helia'
import { type Blockstore } from 'interface-blockstore'
import { Key, type Datastore } from 'interface-datastore'
import { CRDTDatastore, msgIdFnStrictNoSign, PubSubBroadcaster, defaultOptions, type CRDTLibp2pServices, type Options } from 'js-ds-crdt'
import { createLibp2p } from 'libp2p'
import type { Libp2p, PeerId } from '@libp2p/interface'

export async function newCRDTDatastore (peerId: PeerId, port: number, topic: string = 'test', datastore: Datastore, blockstore: Blockstore, options?: Options): Promise<CRDTDatastore> {
  const store = datastore // new MemoryDatastore()
  const namespace = new Key('/test')
  const dagService = await createNode(peerId, port, datastore, blockstore)
  const broadcaster = new PubSubBroadcaster(dagService.libp2p, topic, prefixLogger('crdt').forComponent('pubsub'))

  let opts
  if (options !== undefined) {
    opts = { ...defaultOptions(), ...options }
  }

  return new CRDTDatastore(store, namespace, dagService, broadcaster, opts)
}

export async function createNode (peerId: PeerId, port: number, datastore: Datastore, blockstore: Blockstore): Promise<HeliaLibp2p<Libp2p<CRDTLibp2pServices>>> {
  const libp2p = await createLibp2p({
    peerId,
    addresses: {
      listen: [
        '/ip4/127.0.0.1/tcp/' + port
      ]
    },
    transports: [
      tcp()
    ],
    connectionEncryption: [
      noise()
    ],
    streamMuxers: [
      yamux()
    ],
    services: {
      identify: identify(),
      pubsub: gossipsub({
        emitSelf: false,
        allowPublishToZeroTopicPeers: true,
        msgIdFn: msgIdFnStrictNoSign,
        ignoreDuplicatePublishError: true,
        tagMeshPeers: true
        // doPX: true
      })
    }
  })

  const h = await createHelia({
    datastore,
    blockstore,
    libp2p
  })

  return h
}

export function hexToUint8Array (hexString: string): Uint8Array {
  if (hexString.length % 2 !== 0) {
    throw new Error('Invalid hex string')
  }

  const arrayBuffer = new Uint8Array(hexString.length / 2)

  for (let i = 0; i < hexString.length; i += 2) {
    const byteValue = parseInt(hexString.slice(i, i + 2), 16)
    arrayBuffer[i / 2] = byteValue
  }

  return arrayBuffer
}

export function base64ToUint8Array (base64: string): Uint8Array {
  const binaryString = atob(base64)
  const length = binaryString.length
  const bytes = new Uint8Array(length)

  for (let i = 0; i < length; i++) {
    bytes[i] = binaryString.charCodeAt(i)
  }

  return bytes
}

export function uint8ArrayToBase64 (uint8Array: Uint8Array): string {
  let binaryString = ''
  for (let i = 0; i < uint8Array.length; i++) {
    binaryString += String.fromCharCode(uint8Array[i])
  }
  return btoa(binaryString)
}
