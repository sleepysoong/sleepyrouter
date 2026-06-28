import fs from 'node:fs';
import { readLocalEnv } from '../config/env.js';
import { getEnvPath } from '../config/paths.js';
import { ConfigStore } from '../config/store.js';
import { allGroupModelIds } from '../model-groups.js';

interface OutputLike {
  write(chunk: string): unknown;
}

export interface DoctorProviderStatus {
  name: string;
  envName: 'OPENROUTER_API_KEY' | 'NVIDIA_API_KEY';
  source: 'process' | 'local-env' | 'missing';
  validPrefix: boolean | 'unknown';
}

export interface DoctorStatus {
  configPath: string;
  envPath: string;
  envFileExists: boolean;
  providers: DoctorProviderStatus[];
  modelCount: number;
  cachedModelCount: number;
  cacheFetchedAt?: string;
  groupNames: string[];
  defaultGroup?: string;
}

const PROVIDERS: Array<{ name: string; envName: DoctorProviderStatus['envName']; prefix: string }> = [
  { name: 'OpenRouter', envName: 'OPENROUTER_API_KEY', prefix: 'sk-or-' },
  { name: 'NVIDIA', envName: 'NVIDIA_API_KEY', prefix: 'nvapi-' },
];

function keyValue(env: NodeJS.ProcessEnv, local: Record<string, string>, envName: DoctorProviderStatus['envName']): { source: DoctorProviderStatus['source']; value?: string } {
  const processValue = env[envName]?.trim();
  if (processValue) return { source: 'process', value: processValue };
  const localValue = local[envName]?.trim();
  if (localValue) return { source: 'local-env', value: localValue };
  return { source: 'missing' };
}

function validPrefix(value: string | undefined, prefix: string): boolean | 'unknown' {
  if (!value) return 'unknown';
  return value.startsWith(prefix);
}

export function getDoctorStatus(options: { store?: ConfigStore; env?: NodeJS.ProcessEnv } = {}): DoctorStatus {
  const store = options.store ?? new ConfigStore();
  const env = options.env ?? process.env;
  const local = readLocalEnv(store.paths.root);
  const config = store.readConfig();
  const cache = store.readModelCache();

  const envPath = getEnvPath(store.paths.root);
  return {
    configPath: store.paths.configPath,
    envPath,
    envFileExists: fs.existsSync(envPath),
    providers: PROVIDERS.map((provider) => {
      const key = keyValue(env, local, provider.envName);
      return {
        name: provider.name,
        envName: provider.envName,
        source: key.source,
        validPrefix: validPrefix(key.value, provider.prefix),
      };
    }),
    modelCount: allGroupModelIds(config.modelGroups).length,
    cachedModelCount: cache?.models.length ?? 0,
    cacheFetchedAt: cache?.fetchedAt,
    groupNames: Object.keys(config.modelGroups),
    defaultGroup: config.defaultGroup,
  };
}

function prefixLabel(status: DoctorProviderStatus): string {
  if (status.source === 'missing') return '없음';
  return status.validPrefix === true ? `${status.source}, 접두사 확인됨` : `${status.source}, 접두사 경고`;
}

export function printDoctorStatus(options: { store?: ConfigStore; env?: NodeJS.ProcessEnv; stdout?: OutputLike } = {}): void {
  const stdout = options.stdout ?? process.stdout;
  const status = getDoctorStatus(options);
  stdout.write('slr 진단\n');
  stdout.write(`설정 파일: ${status.configPath}\n`);
  stdout.write(`환경 파일: ${status.envPath} (${status.envFileExists ? '존재' : '없음'})\n`);
  stdout.write('프로바이더 키:\n');
  for (const provider of status.providers) {
    stdout.write(`- ${provider.name}: ${prefixLabel(provider)}\n`);
  }
  stdout.write(`선택된 모델: ${status.modelCount}개\n`);
  stdout.write(`캐시된 모델: ${status.cachedModelCount}개${status.cacheFetchedAt ? ` (가져온 시간: ${status.cacheFetchedAt})` : ''}\n`);
  if (status.groupNames.length > 0) {
    stdout.write(`그룹: ${status.groupNames.join(', ')}\n`);
    if (status.defaultGroup) stdout.write(`기본 그룹: ${status.defaultGroup}\n`);
  }
}
