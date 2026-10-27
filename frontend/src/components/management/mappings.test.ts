import { describe, expect, it } from 'vitest';
import { validateMappings } from './mappings';

describe('validateMappings', () => {
  it.each([
    ['empty', {}],
    ['sql', { sql: 'CREATE TABLE t (a TEXT)' }],
    ['text', { properties: { msg: { type: 'text' } } }],
    ['geo_point', { properties: { loc: { type: 'geo_point' } } }],
    ['geo alias', { properties: { loc: { type: 'GEO' } } }],
    ['vector with dims', { properties: { emb: { type: 'vector', dims: 3 } } }],
    ['dense_vector without dims', { properties: { emb: { type: 'dense_vector' } } }],
    ['knn_vector dimension', { properties: { emb: { type: 'knn_vector', dimension: 2 } } }],
    ['nested', { properties: { user: { properties: { loc: { type: 'geo_point' } } } } }],
  ])('accepts %s', (_, mappings) => {
    expect(() => validateMappings(mappings)).not.toThrow();
  });

  it.each([
    ['properties not object', { properties: [] }, 'properties must be an object'],
    ['property not object', { properties: { a: 'text' } }, '"a" must be an object'],
    ['missing type', { properties: { a: {} } }, '"a" requires a string type'],
    ['unknown type', { properties: { a: { type: 'embedding' } } }, 'unsupported type "embedding"'],
    ['vector zero dims', { properties: { emb: { type: 'vector', dims: 0 } } }, 'dims must be a positive integer'],
    ['vector float dims', { properties: { emb: { type: 'vector', dims: 1.5 } } }, 'dims must be a positive integer'],
    ['vector string dims', { properties: { emb: { type: 'vector', dims: '3' } } }, 'dims must be a positive integer'],
    ['dims on text', { properties: { a: { type: 'text', dims: 3 } } }, 'only valid for vector'],
    ['dims on geo_point', { properties: { loc: { type: 'geo_point', dims: 2 } } }, 'only valid for vector'],
    ['geo analyzer', { properties: { loc: { type: 'geo_point', analyzer: 'standard' } } }, 'does not support analyzer'],
    ['nested error path', { properties: { user: { properties: { emb: { type: 'vector', dims: -1 } } } } }, '"user.emb"'],
  ])('rejects %s', (_, mappings, message) => {
    expect(() => validateMappings(mappings as Record<string, unknown>)).toThrow(message);
  });
});
