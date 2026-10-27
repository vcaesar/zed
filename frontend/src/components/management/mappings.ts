// Mirrors pkg/uquery/mappings type rules so schema errors surface before the request.
const TYPES = new Set([
  'text', 'keyword', 'numeric', 'bool', 'date', 'constant_keyword', 'match_only_text',
  'integer', 'double', 'long', 'short', 'int', 'float', 'boolean', 'time', 'datetime',
  'geo_point', 'geo', 'vector', 'dense_vector', 'knn_vector',
  'flattened', 'object', 'nested', 'wildcard', 'byte', 'alias', 'ip', 'ip_range', 'scaled_float',
]);
export const VECTOR_TYPES = new Set(['vector', 'dense_vector', 'knn_vector']);
export const GEO_TYPES = new Set(['geo_point', 'geo']);

function isObject(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === 'object' && !Array.isArray(v);
}

export function validateMappings(mappings: Record<string, unknown>, prefix = ''): void {
  if (typeof mappings.sql === 'string') return;
  if (mappings.properties === undefined) return;
  if (!isObject(mappings.properties)) throw new Error('Mappings properties must be an object.');
  for (const [name, prop] of Object.entries(mappings.properties)) {
    const field = prefix + name;
    if (!isObject(prop)) throw new Error(`Property "${field}" must be an object.`);
    if (prop.properties !== undefined) {
      validateMappings(prop, field + '.');
      continue;
    }
    const type = typeof prop.type === 'string' ? prop.type.toLowerCase() : '';
    if (!type) throw new Error(`Property "${field}" requires a string type.`);
    if (!TYPES.has(type)) throw new Error(`Property "${field}" has unsupported type "${prop.type}".`);
    const dims = prop.dims ?? prop.dimension;
    if (VECTOR_TYPES.has(type)) {
      if (dims !== undefined && (!Number.isInteger(dims) || (dims as number) <= 0)) {
        throw new Error(`Vector property "${field}" dims must be a positive integer.`);
      }
    } else if (dims !== undefined) {
      throw new Error(`Property "${field}" dims is only valid for vector types.`);
    }
    if (GEO_TYPES.has(type) && (prop.analyzer !== undefined || prop.fields !== undefined)) {
      throw new Error(`Geo property "${field}" does not support analyzer or fields.`);
    }
  }
}
