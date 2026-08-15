export function objectKeysInJSON(
  jsonText: string,
  field: string,
): string[] {
  try {
    const parsed = JSON.parse(jsonText);
    if (!parsed[field] || typeof parsed[field] !== "object") return [];
    return Object.keys(parsed[field]);
  } catch {
    return [];
  }
}
