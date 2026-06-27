import { ConfigStore } from '../config/store.js';

export function getStatus(store = new ConfigStore()) {
  const config = store.readConfig();
  const primaryModel = config.selectedModelIds.length > 0 ? config.selectedModelIds[0] : undefined;
  return { port: config.port, configPath: store.paths.configPath, selectedModelCount: config.selectedModelIds.length, primaryModel };
}

export function printStatus(store = new ConfigStore()): void {
  const status = getStatus(store);
  console.log(`port: ${status.port}`);
  console.log(`config: ${status.configPath}`);
  console.log(`selected models: ${status.selectedModelCount}`);
  if (status.primaryModel) console.log(`primary model: ${status.primaryModel}`);
}
