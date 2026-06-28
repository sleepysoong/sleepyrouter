import { ConfigStore } from '../config/store.js';
import { allGroupModelIds } from '../model-groups.js';

export function getStatus(store = new ConfigStore()) {
  const config = store.readConfig();
  const allIds = allGroupModelIds(config.modelGroups);
  const primaryModel = allIds.length > 0 ? allIds[0] : undefined;
  const groupNames = Object.keys(config.modelGroups);
  return { port: config.port, configPath: store.paths.configPath, modelCount: allIds.length, primaryModel, modelGroups: config.modelGroups, defaultGroup: config.defaultGroup, groupNames };
}

export function printStatus(store = new ConfigStore()): void {
  const status = getStatus(store);
  console.log(`포트: ${status.port}`);
  console.log(`설정: ${status.configPath}`);
  console.log(`선택된 모델: ${status.modelCount}개`);
  if (status.primaryModel) console.log(`기본 모델: ${status.primaryModel}`);
  if (status.groupNames.length > 0) {
    console.log(`그룹: ${status.groupNames.join(', ')}`);
    if (status.defaultGroup) console.log(`기본 그룹: ${status.defaultGroup}`);
    for (const name of status.groupNames) {
      const models = status.modelGroups[name];
      console.log(`  ${name}: ${models.join(' → ')}`);
    }
  }
}
