import fs from 'node:fs';
import { readLocalEnv } from '../config/env.js';
import { getEnvPath } from '../config/paths.js';
import { ConfigStore } from '../config/store.js';

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
  selectedModelCount: number;
  cachedModelCount: number;
  cacheFetchedAt?: string;
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
    selectedModelCount: config.selectedModelIds.length,
    cachedModelCount: cache?.models.length ?? 0,
    cacheFetchedAt: cache?.fetchedAt,
  };
}

function prefixLabel(status: DoctorProviderStatus): string {
  if (status.source === 'missing') return 'missing';
  return status.validPrefix === true ? `${status.source}, prefix ok` : `${status.source}, prefix warning`;
}

export function printDoctorStatus(options: { store?: ConfigStore; env?: NodeJS.ProcessEnv; stdout?: OutputLike } = {}): void {
  const stdout = options.stdout ?? process.stdout;
  const status = getDoctorStatus(options);
  stdout.write('omfm doctor\n');
  stdout.write(`config: ${status.configPath}\n`);
  stdout.write(`env file: ${status.envPath} (${status.envFileExists ? 'found' : 'missing'})\n`);
  stdout.write('provider keys:\n');
  for (const provider of status.providers) {
    stdout.write(`- ${provider.name}: ${prefixLabel(provider)}\n`);
  }
  stdout.write(`selected models: ${status.selectedModelCount}\n`);
  stdout.write(`cached models: ${status.cachedModelCount}${status.cacheFetchedAt ? ` (fetched ${status.cacheFetchedAt})` : ''}\n`);
}
