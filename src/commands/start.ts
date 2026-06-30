import { ConfigStore } from '../config/store.js';
import { createOmfmServer, formatServerLogEvent, listen } from '../server/create-server.js';
import { allGroupModelIds } from '../model-groups.js';
import { requireAnyProviderApiKey, resolveProviderApiKeys } from '../config/env.js';
import { VERSION } from '../version.js';
import { getConfigPath, getEnvPath } from '../config/paths.js';
import Table from 'cli-table3';

export async function runStartCommand(options: { port?: number; store?: ConfigStore } = {}): Promise<void> {
  const store = options.store ?? new ConfigStore();
  store.ensureRoot();
  const config = store.readConfig();
  const port = options.port ?? config.port;
  if (config.port !== port) store.writeConfig({ ...config, port });

  const env = process.env;
  const keys = resolveProviderApiKeys(env, store.paths.root);
  const hasNvidiaKey = Boolean(keys.nvidia);
  const hasOpenRouterKey = Boolean(keys.openrouter);

  console.log(`\nslr v${VERSION}`);
  console.log(`  config: ${getConfigPath(store.paths.root)}`);
  console.log(`  env: ${getEnvPath(store.paths.root)}`);
  console.log(`  NVIDIA_API_KEY: ${hasNvidiaKey ? '✓' : '✗'}`);
  console.log(`  OPENROUTER_API_KEY: ${hasOpenRouterKey ? '✓' : '✗'}`);

  const apiKeys = requireAnyProviderApiKey(env, store.paths.root);

  const invalidModels: string[] = [];
  for (const [group, models] of Object.entries(config.modelGroups)) {
    for (const id of models) {
      if (!id.startsWith('nvidia/') && !id.startsWith('openrouter/')) {
        invalidModels.push(`${group}: ${id}`);
      }
    }
  }
  if (invalidModels.length > 0) {
    console.error(`\n모델 ID가 잘못되었어요. nvidia/ 또는 openrouter/ 접두사가 필요해요:`);
    for (const m of invalidModels) console.error(`  - ${m}`);
    console.error(`\nconfig.json을 수정한 후 다시 시도하세요.`);
    process.exit(1);
  }

  const groupNames = Object.keys(config.modelGroups);
  if (groupNames.length > 0) {
    const totalModels = allGroupModelIds(config.modelGroups).length;
    
    const table = new Table({
      head: ['그룹', '모델'],
      colWidths: [20, 60],
      style: { head: ['cyan'] }
    });

    for (const name of groupNames) {
      const models = config.modelGroups[name]!;
      const marker = name === config.defaultGroup ? ' (기본)' : '';
      table.push([`${name}${marker}`, models.join('\n')]);
    }

    console.log(`\n모델 그룹 (${totalModels}개 모델, ${groupNames.length}개 그룹)`);
    console.log(table.toString());
    
    if (config.defaultGroup) {
      console.log(`\n기본 그룹: ${config.defaultGroup}`);
    }
    console.log('');
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
