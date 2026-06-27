import { ConfigStore } from '../config/store.js';
import { createOmfmServer, formatServerLogEvent, listen } from '../server/create-server.js';

export async function runStartCommand(options: { port?: number; store?: ConfigStore } = {}): Promise<void> {
  const store = options.store ?? new ConfigStore();
  store.ensureRoot();
  const config = store.readConfig();
  const port = options.port ?? config.port;
  if (config.port !== port) store.writeConfig({ ...config, port });

  const server = createOmfmServer({ store, requestLogger: (event) => console.log(formatServerLogEvent(event, { color: process.stdout.isTTY })) });
  const actualPort = await listen(server, port);
  console.log(`omfm listening on http://localhost:${actualPort}`);

  const shutdown = () => {
    server.close(() => process.exit(0));
  };
  process.once('SIGINT', shutdown);
  process.once('SIGTERM', shutdown);
}
