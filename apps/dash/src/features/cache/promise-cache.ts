// A promise cache resolves each key once and then serves the settled value
// synchronously, so callers can render cached results on the first paint.
// Concurrent loads for one key share a single in-flight promise, and failed
// loads are forgotten so the next caller retries instead of replaying the
// rejection.
export type PromiseCache<Key, Value> = {
  /** The cached value for a key, if its load has succeeded. */
  get(key: Key): Value | undefined;
  /** The cached value, the shared in-flight promise, or a new load. */
  load(key: Key, fetch: () => Promise<Value>): Promise<Value>;
  /** Forgets one key, disposing its cached value. */
  delete(key: Key): void;
  /** Forgets every key, disposing all cached values. */
  clear(): void;
};

export function createPromiseCache<Key, Value>(options?: {
  /** Releases a cached value's resources when it leaves the cache. */
  dispose?: (value: Value) => void;
}): PromiseCache<Key, Value> {
  const values = new Map<Key, Value>();
  const requests = new Map<Key, Promise<Value>>();

  return {
    get: (key) => values.get(key),

    load(key, fetch) {
      if (values.has(key)) return Promise.resolve(values.get(key) as Value);

      let request = requests.get(key);
      if (!request) {
        request = fetch()
          .then((value) => {
            values.set(key, value);
            requests.delete(key);
            return value;
          })
          .catch((error: unknown) => {
            requests.delete(key);
            throw error;
          });
        requests.set(key, request);
      }
      return request;
    },

    delete(key) {
      const value = values.get(key);
      if (values.delete(key) && value !== undefined) options?.dispose?.(value);
      requests.delete(key);
    },

    clear() {
      if (options?.dispose) {
        for (const value of values.values()) options.dispose(value);
      }
      values.clear();
      requests.clear();
    },
  };
}
