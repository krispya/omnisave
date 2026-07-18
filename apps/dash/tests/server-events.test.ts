import { describe, expect, it } from 'vitest';
import { createServerEventParser, type ServerEvent } from '../src/lib/omnisave-api.js';

describe('server event parsing', () => {
  it('assembles an invalidation split across chunks and ignores heartbeats', () => {
    const events: ServerEvent[] = [];
    const parser = createServerEventParser((event) => events.push(event));

    parser.push(': keepalive\n\nid: 42\neve');
    parser.push('nt: library.changed\r\ndata: {"scope":\r\n');
    parser.push('data: "library"}\r\n\r\n');

    expect(events).toEqual([
      {
        id: '42',
        type: 'library.changed',
        data: '{"scope":\n"library"}',
      },
    ]);
    expect(parser.lastEventID()).toBe('42');
  });

  it('dispatches a final unterminated frame with the default event type', () => {
    const events: ServerEvent[] = [];
    const parser = createServerEventParser((event) => events.push(event), '7');

    parser.push('data: {}');
    parser.finish();

    expect(events).toEqual([{ id: '7', type: 'message', data: '{}' }]);
  });
});
