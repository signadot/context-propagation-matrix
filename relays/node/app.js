'use strict';
// Naive relay: receives {test_id, forward_via} over HTTP/gRPC/Kafka and
// forwards to the sink over the requested protocol. Never touches headers or
// tracing APIs — context propagation is the instrumentation's job.
const path = require('path');
const express = require('express');
const axios = require('axios');
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const { Kafka, logLevel } = require('kafkajs');

const VARIANT = process.env.VARIANT || 'unknown';
const TOOL = process.env.TOOL || 'unknown';
const RELAY_TOPIC = process.env.RELAY_TOPIC || `relay-in-${VARIANT}`;
const KAFKA_BROKERS = (process.env.KAFKA_BROKERS || 'kafka:9092').split(',');
const SINK_HTTP = process.env.SINK_HTTP || 'http://sink:9080';
const SINK_GRPC = process.env.SINK_GRPC || 'sink:9081';

const def = protoLoader.loadSync(path.join(__dirname, 'relay.proto'), { keepCase: true });
const pkg = grpc.loadPackageDefinition(def).cpmatrix;

const kafka = new Kafka({
  clientId: `relay-${VARIANT}`,
  brokers: KAFKA_BROKERS,
  logLevel: logLevel.ERROR,
});
const producer = kafka.producer();
const consumer = kafka.consumer({ groupId: `relay-${VARIANT}` });

const sinkClient = new pkg.Sink(SINK_GRPC, grpc.credentials.createInsecure());

async function forward(testId, via) {
  if (via === 'http') {
    await axios.post(`${SINK_HTTP}/deliver`, { test_id: testId });
  } else if (via === 'grpc') {
    await new Promise((resolve, reject) =>
      sinkClient.Deliver({ test_id: testId }, (err, res) => (err ? reject(err) : resolve(res))));
  } else if (via === 'kafka') {
    await producer.send({
      topic: 'sink-in',
      messages: [{ value: JSON.stringify({ test_id: testId }) }],
    });
  } else {
    throw new Error(`unknown forward_via: ${via}`);
  }
}

function pkgVersion(name) {
  // fs read (not require): some packages' "exports" block package.json access
  try {
    const fs = require('fs');
    return JSON.parse(fs.readFileSync(`/app/node_modules/${name}/package.json`, 'utf8')).version;
  } catch {
    return 'unknown';
  }
}

function tracerVersion() {
  const name = {
    otel: '@opentelemetry/auto-instrumentations-node',
    datadog: 'dd-trace',
    newrelic: 'newrelic',
  }[TOOL];
  return name ? pkgVersion(name) : 'unknown';
}

const app = express();
app.use(express.json());

app.post('/relay', async (req, res) => {
  try {
    await forward(req.body.test_id, req.body.forward_via);
    res.json({ status: 'forwarded' });
  } catch (e) {
    console.error('forward error:', e);
    res.status(500).json({ error: String(e) });
  }
});

app.get('/info', (req, res) => {
  const config = {};
  for (const k of [
    'OTEL_PROPAGATORS', 'OTEL_TRACES_EXPORTER',
    'DD_TRACE_PROPAGATION_STYLE', 'NEW_RELIC_DISTRIBUTED_TRACING_ENABLED',
  ]) {
    if (process.env[k]) config[k] = process.env[k];
  }
  res.json({
    variant: VARIANT,
    tool: TOOL,
    language: 'node',
    tracer_version: tracerVersion(),
    runtime_version: `node ${process.version}`,
    config,
    libs: {
      express: pkgVersion('express'),
      kafkajs: pkgVersion('kafkajs'),
      'grpc-js': pkgVersion('@grpc/grpc-js'),
      http_client: `axios ${pkgVersion('axios')}`,
    },
  });
});

async function main() {
  await producer.connect();

  const server = new grpc.Server();
  server.addService(pkg.Relay.service, {
    Handle: async (call, cb) => {
      try {
        await forward(call.request.test_id, call.request.forward_via);
        cb(null, { status: 'forwarded' });
      } catch (e) {
        console.error('grpc forward error:', e);
        cb(e);
      }
    },
  });
  await new Promise((resolve, reject) =>
    server.bindAsync('0.0.0.0:8081', grpc.ServerCredentials.createInsecure(),
      (e) => (e ? reject(e) : resolve())));

  await consumer.connect();
  await consumer.subscribe({ topic: RELAY_TOPIC, fromBeginning: true });
  await consumer.run({
    eachMessage: async ({ message }) => {
      try {
        const req = JSON.parse(message.value.toString());
        await forward(req.test_id, req.forward_via);
      } catch (e) {
        console.error('kafka relay error:', e);
      }
    },
  });

  app.listen(8080, () =>
    console.log(`relay ${VARIANT} listening on :8080 (http), :8081 (grpc), topic ${RELAY_TOPIC}`));
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
