import { describe, expect, it } from 'vitest';
import { chooseGroupedModel, chooseModel, orderedCandidates } from '../src/latency/router.js';
import { allGroupModelIds, selectedGroupModelIds } from '../src/model-groups.js';

describe('group-name router', () => {
  it('routes to group when requestedModel matches group name', () => {
    const groups = { fast: ['a', 'b'], balanced: ['c'] };
    expect(chooseModel(groups, 'fast')).toEqual({ modelId: 'a', reason: 'model-group' });
  });

  it('falls back to defaultGroup when requestedModel is not a group', () => {
    const groups = { fast: ['a', 'b'], balanced: ['c'] };
    expect(orderedCandidates(groups, 'auto')).toEqual(['a', 'b']);
  });

  it('falls back to first group when no request', () => {
    const groups = { default: ['z', 'a'] };
    expect(chooseModel(groups, undefined)).toEqual({ modelId: 'z', reason: 'fallback-order' });
  });

  it('returns group order as candidates', () => {
    const groups = { default: ['a', 'b', 'c'] };
    expect(orderedCandidates(groups)).toEqual(['a', 'b', 'c']);
  });

  it('routes group aliases within the configured group only', () => {
    const groups = { fast: ['b'], balanced: ['a'], capable: ['c'] };
    expect(chooseGroupedModel(groups, 'slr/fast')).toEqual({ modelId: 'b', reason: 'model-group' });
    expect(orderedCandidates(groups, 'haiku')).toEqual(['b']);
  });

  it('returns empty array when all groups are empty', () => {
    expect(orderedCandidates({ fast: [], balanced: [], capable: [] }, 'opus')).toEqual([]);
  });
});

describe('configurable groups', () => {
  it('routes to a custom group by name', () => {
    const groups = { coding: ['model-a', 'model-b'], chat: ['model-c'] };
    expect(chooseGroupedModel(groups, 'coding'))
      .toEqual({ modelId: 'model-a', reason: 'model-group' });
  });

  it('returns all models in group as ordered candidates', () => {
    const groups = { coding: ['model-a', 'model-b', 'model-c'] };
    expect(orderedCandidates(groups, 'coding'))
      .toEqual(['model-a', 'model-b', 'model-c']);
  });

  it('falls back to defaultGroup when requested model is not a group name', () => {
    const groups = { coding: ['model-a', 'model-b'], default: ['model-c', 'model-d'] };
    expect(orderedCandidates(groups, 'unknown-model', 'default'))
      .toEqual(['model-c', 'model-d']);
  });

  it('falls back to first group when defaultGroup is not set', () => {
    const groups = { coding: ['model-a', 'model-b'], chat: ['model-c'] };
    expect(orderedCandidates(groups, 'unknown-model'))
      .toEqual(['model-a', 'model-b']);
  });

  it('uses defaultGroup for generic model names like auto', () => {
    const groups = { fast: ['model-a'], default: ['model-b', 'model-c'] };
    expect(orderedCandidates(groups, 'auto', 'default'))
      .toEqual(['model-b', 'model-c']);
  });

  it('ignores slr/ prefix on group names', () => {
    const groups = { coding: ['model-a', 'model-b'] };
    expect(chooseGroupedModel(groups, 'slr/coding'))
      .toEqual({ modelId: 'model-a', reason: 'model-group' });
  });

  it('supports legacy aliases (haiku, sonnet, opus)', () => {
    const groups = { fast: ['model-a'], balanced: ['model-b'], capable: ['model-c'] };
    expect(chooseGroupedModel(groups, 'haiku'))
      .toEqual({ modelId: 'model-a', reason: 'model-group' });
    expect(chooseGroupedModel(groups, 'sonnet'))
      .toEqual({ modelId: 'model-b', reason: 'model-group' });
    expect(chooseGroupedModel(groups, 'opus'))
      .toEqual({ modelId: 'model-c', reason: 'model-group' });
  });

  it('derives master list from all groups', () => {
    const groups = { a: ['x', 'y'], b: ['y', 'z'] };
    expect(allGroupModelIds(groups)).toEqual(['x', 'y', 'z']);
  });

  it('returns undefined from selectedGroupModelIds for unknown group', () => {
    const groups = { coding: ['model-a'] };
    expect(selectedGroupModelIds(groups, 'unknown')).toBeUndefined();
  });
});
