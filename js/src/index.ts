import { peerIdFromKeys } from '@libp2p/peer-id'
import { MemoryBlockstore } from 'blockstore-core'
import { MemoryDatastore } from 'datastore-core'
import Fastify from 'fastify'
import { Key } from 'interface-datastore'
import debug from 'weald'
import config from './config.json' with { type: 'json' }
import { base64ToUint8Array, hexToUint8Array, newCRDTDatastore, uint8ArrayToBase64 } from './utils'
import type { CRDTDatastore } from 'js-ds-crdt'

const postKVOpts = {
  schema: {
    body: {
      type: 'object',
      required: ['key', 'value'],
      properties: {
        key: { type: 'string' },
        value: { type: 'string' } // value is a base64 encoded string
      }
    }
  }
}

interface KVRequestBody {
  key: string
  value: string
}

async function startServer (datastore: CRDTDatastore, httpHost: string, httpPort: number): Promise<void> {
  const fastify = Fastify({
    logger: false
  })

  fastify.get('/*', async (request, reply) => {
    const { '*': key } = request.params as { '*': string }

    try {
      const value = await datastore.get(new Key(key))

      if (value === undefined || value === null) {
        throw new Error('Key not found')
      }
      return { value: uint8ArrayToBase64(value) }
    } catch (err) {
      fastify.log.error(err)
      await reply.status(500).send({ error: 'Failed to store data' })
    }
    return { success: true }
  })

  fastify.post<{ Body: KVRequestBody }>('/put', postKVOpts, async (request, reply) => {
    const { key, value } = request.body
    const datastoreValue = base64ToUint8Array(value)

    try {
      await datastore.put(new Key(key), datastoreValue)
      return { success: true }
    } catch (err) {
      fastify.log.error(err)
      await reply.status(500).send({ error: 'Failed to store data' })
    }
  })

  try {
    const multiaddrs = datastore.dagService.libp2p.getMultiaddrs()

    await fastify.listen({ host: httpHost, port: httpPort })
    // eslint-disable-next-line no-console
    console.log(`HTTP Server running on http://${httpHost}:${httpPort}/`)
    multiaddrs.forEach(ma => {
      // eslint-disable-next-line no-console
      console.log('Libp2p running on', ma.toString())
    })
  } catch (err) {
    fastify.log.error(err)
    process.exit(1)
  }
}

const peerId0 = await peerIdFromKeys(
  hexToUint8Array(config.peers[0].public_key),
  hexToUint8Array(config.peers[0].private_key)
)
const peerId1 = await peerIdFromKeys(
  hexToUint8Array(config.peers[1].public_key),
  hexToUint8Array(config.peers[1].private_key)
)

const libp2pPort = 6000
const gossipSubTopic = 'test'
const datastore0 = new MemoryDatastore()
const blockstore0 = new MemoryBlockstore()

const datastore1 = new MemoryDatastore()
const blockstore1 = new MemoryBlockstore()

const crdtDatastore0 = await newCRDTDatastore(peerId0, libp2pPort, gossipSubTopic, datastore0, blockstore0)

const crdtDatastore1 = await newCRDTDatastore(peerId1, libp2pPort + 1, gossipSubTopic, datastore1, blockstore1)
const httpHost = '127.0.0.1'
const httpPort = 3000

debug.enable('crdt*')
// 'crdt*,*crdt:trace')
try {
  await crdtDatastore0.dagService.libp2p.dial(crdtDatastore1.dagService.libp2p.getMultiaddrs()[0])
} catch (err) {
  // eslint-disable-next-line no-console
  console.error(err)
  process.exit(1)
}

await startServer(crdtDatastore0, httpHost, httpPort)
