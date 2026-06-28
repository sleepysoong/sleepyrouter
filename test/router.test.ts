import { describe, expect, it } from 'vitest';
import { chooseGroupedModel, chooseModel, orderedCandidates } from '../src/latency/router.js';
import { allGroupModelIds, selectedGroupModelIds } from '../src/model-groups.js';

describe('config-order router', () => {
  it('honors requested selected model', () => {
    const groups = { default: ['a', 'b'] };
    expect(chooseModel(groups, 'b')).toEqual({ modelId: 'b', reason: 'requested-selected' });
  });

  it('uses config order for generic request', () => {
    const groups = { default: ['a', 'b'] };
    expect(chooseModel(groups, 'auto')).toEqual({ modelId: 'a', reason: 'fallback-order' });
  });

  it('falls back deterministically when no request', () => {
    const groups = { default: ['z', 'a'] };
    expect(chooseModel(groups, undefined)).toEqual({ modelId: 'z', reason: 'fallback-order' });
  });

  it('orders retry candidates by config order', () => {
    const groups = { default: ['a', 'b', 'c'] };
    expect(orderedCandidates(groups)).toEqual(['a', 'b', 'c']);
  });

  it('returns group order as-is for requested model in group', () => {
    const groups = { default: ['b', 'a', 'c'] };
    expect(orderedCandidates(groups, 'c')).toEqual(['b', 'a', 'c']);
  });

  it('routes group aliases within the configured group only', () => {
    const groups = { fast: ['b'], balanced: ['a'], capable: ['c'] };
    expect(chooseGroupedModel(groups, 'slr/fast')).toEqual({ modelId: 'b', reason: 'model-group' });
    expect(orderedCandidates(groups, 'haiku')).toEqual(['b']);
  });

  it('returns empty array when all groups are empty', () => {
    expect(orderedCandidates({ fast: [], balanced: [], capable: [] }, 'opus')).toEqual([]);
  });

  it('prefers an exact selected model id over a group alias', () => {
    expect(orderedCandidates({ fast: [], balanced: [], capable: ['b'] }, 'b')).toEqual(['b']);
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

  it('falls back to defaultGroup when requested model is not a group or selected model', () => {
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

  it('still routes exact model even if it matches a group name', () => {
    const groups = { coding: ['coding', 'model-a', 'model-b'] };
    expect(orderedCandidates(groups, 'coding'))
      .toEqual(['coding', 'model-a', 'model-b']);
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
