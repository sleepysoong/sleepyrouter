import { ConfigStore } from '../config/store.js';
import { createOmfmServer, formatServerLogEvent, listen } from '../server/create-server.js';
import { allGroupModelIds } from '../model-groups.js';

export async function runStartCommand(options: { port?: number; store?: ConfigStore } = {}): Promise<void> {
  const store = options.store ?? new ConfigStore();
  store.ensureRoot();
  const config = store.readConfig();
  const port = options.port ?? config.port;
  if (config.port !== port) store.writeConfig({ ...config, port });

  const groupNames = Object.keys(config.modelGroups);
  if (groupNames.length > 0) {
    const totalModels = allGroupModelIds(config.modelGroups).length;
    console.log(`\n📋 모델 그룹 (${totalModels}개 모델, ${groupNames.length}개 그룹)`);
    console.log('─'.repeat(50));
    for (const name of groupNames) {
      const models = config.modelGroups[name]!;
      const marker = name === config.defaultGroup ? ' ⭐' : '';
      console.log(`  ${name}${marker}:`);
      models.forEach((m, i) => console.log(`    ${i + 1}. ${m}`));
    }
    if (config.defaultGroup) {
      console.log(`\n  ⭐ = 기본 그룹 (${config.defaultGroup})`);
    }
    console.log('─'.repeat(50) + '\n');
  }

  const server = createOmfmServer({ store, requestLogger: (event) => console.log(formatServerLogEvent(event, { color: process.stdout.isTTY })) });
  const actualPort = await listen(server, port);
  console.log(`slr가 http://localhost:${actualPort}에서 실행 중이에요.`);

  const shutdown = () => {
    server.close(() => process.exit(0));
  };
  process.once('SIGINT', shutdown);
  process.once('SIGTERM', shutdown);
}
