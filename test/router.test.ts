import { describe, expect, it } from 'vitest';
import { chooseGroupedModel, chooseModel, orderedCandidates } from '../src/latency/router.js';
import { selectedGroupModelIds } from '../src/model-groups.js';

describe('config-order router', () => {
  it('honors requested selected model', () => {
    expect(chooseModel(['a', 'b'], 'b')).toEqual({ modelId: 'b', reason: 'requested-selected' });
  });

  it('uses config order for generic request', () => {
    expect(chooseModel(['a', 'b'], 'auto')).toEqual({ modelId: 'a', reason: 'fallback-order' });
  });

  it('falls back deterministically when no request', () => {
    expect(chooseModel(['z', 'a'], undefined)).toEqual({ modelId: 'z', reason: 'fallback-order' });
  });

  it('orders retry candidates by config order', () => {
    expect(orderedCandidates(['a', 'b', 'c'])).toEqual(['a', 'b', 'c']);
  });

  it('preserves config order for requested model', () => {
    expect(orderedCandidates(['b', 'a', 'c'], 'c')).toEqual(['c', 'b', 'a']);
  });

  it('routes group aliases within the configured group only', () => {
    const groups = { fast: ['b'], balanced: ['a'], capable: ['c'] };
    expect(chooseGroupedModel(['a', 'b', 'c'], 'slr/fast', groups)).toEqual({ modelId: 'b', reason: 'model-group' });
    expect(orderedCandidates(['a', 'b', 'c'], 'haiku', groups)).toEqual(['b']);
  });

  it('falls back to the full selection when a requested group is empty', () => {
    expect(orderedCandidates(['a', 'b'], 'opus', { fast: [], balanced: [], capable: [] })).toEqual(['a', 'b']);
  });

  it('prefers an exact selected model id over a group alias', () => {
    expect(orderedCandidates(['opus', 'b'], 'opus', { fast: [], balanced: [], capable: ['b'] })).toEqual(['opus', 'b']);
  });
});

describe('configurable groups', () => {
  it('routes to a custom group by name', () => {
    const groups = { coding: ['model-a', 'model-b'], chat: ['model-c'] };
    expect(chooseGroupedModel(['model-a', 'model-b', 'model-c'], 'coding', groups))
      .toEqual({ modelId: 'model-a', reason: 'model-group' });
  });

  it('returns all models in group as ordered candidates', () => {
    const groups = { coding: ['model-a', 'model-b', 'model-c'] };
    expect(orderedCandidates(['model-a', 'model-b', 'model-c'], 'coding', groups))
      .toEqual(['model-a', 'model-b', 'model-c']);
  });

  it('falls back to defaultGroup when requested model is not a group or selected model', () => {
    const groups = { coding: ['model-a', 'model-b'], default: ['model-c', 'model-d'] };
    expect(orderedCandidates(['model-a', 'model-b', 'model-c', 'model-d'], 'unknown-model', groups, 'default'))
      .toEqual(['model-c', 'model-d']);
  });

  it('falls back to first group when defaultGroup is not set', () => {
    const groups = { coding: ['model-a', 'model-b'], chat: ['model-c'] };
    expect(orderedCandidates(['model-a', 'model-b', 'model-c'], 'unknown-model', groups))
      .toEqual(['model-a', 'model-b']);
  });

  it('uses defaultGroup for generic model names like auto', () => {
    const groups = { fast: ['model-a'], default: ['model-b', 'model-c'] };
    expect(orderedCandidates(['model-a', 'model-b', 'model-c'], 'auto', groups, 'default'))
      .toEqual(['model-b', 'model-c']);
  });

  it('still routes exact selected model even if it matches a group name', () => {
    const groups = { coding: ['model-a', 'model-b'] };
    expect(orderedCandidates(['coding', 'model-a', 'model-b'], 'coding', groups))
      .toEqual(['coding', 'model-a', 'model-b']);
  });

  it('ignores slr/ prefix on group names', () => {
    const groups = { coding: ['model-a', 'model-b'] };
    expect(chooseGroupedModel(['model-a', 'model-b'], 'slr/coding', groups))
      .toEqual({ modelId: 'model-a', reason: 'model-group' });
  });

  it('supports legacy aliases (haiku, sonnet, opus)', () => {
    const groups = { fast: ['model-a'], balanced: ['model-b'], capable: ['model-c'] };
    expect(chooseGroupedModel(['model-a', 'model-b', 'model-c'], 'haiku', groups))
      .toEqual({ modelId: 'model-a', reason: 'model-group' });
    expect(chooseGroupedModel(['model-a', 'model-b', 'model-c'], 'sonnet', groups))
      .toEqual({ modelId: 'model-b', reason: 'model-group' });
    expect(chooseGroupedModel(['model-a', 'model-b', 'model-c'], 'opus', groups))
      .toEqual({ modelId: 'model-c', reason: 'model-group' });
  });

  it('filters group models to only selected models', () => {
    const groups = { coding: ['model-a', 'model-b', 'model-c'] };
    // model-c is not in selectedModelIds
    expect(orderedCandidates(['model-a', 'model-b'], 'coding', groups))
      .toEqual(['model-a', 'model-b']);
  });

  it('returns undefined from selectedGroupModelIds for unknown group', () => {
    const groups = { coding: ['model-a'] };
    expect(selectedGroupModelIds(['model-a'], groups, 'unknown')).toBeUndefined();
  });
});
