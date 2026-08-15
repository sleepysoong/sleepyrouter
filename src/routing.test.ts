import { describe, expect, test } from "bun:test";
import {
  normalizeModelGroupName,
  normalizeModelGroupsOrdered,
  allGroupModelIDs,
  resolveDefaultGroup,
  candidateIDs,
  orderedCandidates,
  objectKeysInJSON,
} from "./routing/index.js";

describe("Routing Logic", () => {
  test("normalizeModelGroupName", () => {
    expect(normalizeModelGroupName("  FAST ")).toBe("fast");
    expect(normalizeModelGroupName("")).toBe("");
  });

  test("normalizeModelGroupsOrdered", () => {
    const raw = {
      capable: ["capable-1", "capable-2"],
      fast: ["fast-1"],
    };
    const { groups, order } = normalizeModelGroupsOrdered(raw);
    expect(groups).toEqual({
      capable: ["capable-1", "capable-2"],
      fast: ["fast-1"],
    });
    expect(order).toEqual(["capable", "fast"]);
  });

  test("allGroupModelIDs deduplicates and respects group order", () => {
    const groups = {
      fast: ["model-a", "model-b"],
      balanced: ["model-b", "model-c"],
      capable: ["model-d"],
    };
    const ids = allGroupModelIDs(groups, "balanced", "fast", "capable");
    expect(ids).toEqual(["model-b", "model-c", "model-a", "model-d"]);
  });

  test("resolveDefaultGroup", () => {
    const groups = {
      fast: ["a"],
      balanced: ["b"],
    };
    expect(resolveDefaultGroup(groups, "balanced")).toBe("balanced");
    expect(resolveDefaultGroup(groups, "invalid", "fast")).toBe("fast");
  });

  test("candidateIDs with matched group", () => {
    const groups = {
      fast: ["fast-1", "fast-2"],
      balanced: ["bal-1"],
    };
    const { ids, reason } = candidateIDs(groups, "FAST", "balanced");
    expect(ids).toEqual(["fast-1", "fast-2"]);
    expect(reason).toBe("model-group");
  });

  test("candidateIDs with fallback group", () => {
    const groups = {
      fast: ["fast-1"],
      balanced: ["bal-1"],
    };
    const { ids, reason } = candidateIDs(groups, "unknown-model", "balanced");
    expect(ids).toEqual(["bal-1"]);
    expect(reason).toBe("fallback-order");
  });

  test("objectKeysInJSON extracts ordered keys from raw JSON string", () => {
    const jsonStr = `
    {
      "port": 4567,
      "modelGroups": {
        "fast": ["a"],
        "balanced": ["b"],
        "capable": ["c"]
      }
    }`;
    const keys = objectKeysInJSON(jsonStr, "modelGroups");
    expect(keys).toEqual(["fast", "balanced", "capable"]);
  });
});
