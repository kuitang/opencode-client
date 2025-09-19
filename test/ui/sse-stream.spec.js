const { test, expect } = require('@playwright/test');
const http = require('http');

const SSE_PORT = process.env.PLAYWRIGHT_SSE_PORT;
const SSE_PATH = process.env.PLAYWRIGHT_SSE_PATH || '/event';

function collectSseEvents({ hostname = 'localhost', port, path }) {
  return new Promise((resolve, reject) => {
    const events = [];
    const req = http.request(
      {
        hostname,
        port,
        path,
        method: 'GET',
        headers: { Accept: 'text/event-stream' },
      },
      (res) => {
        res.setEncoding('utf8');
        let buffer = '';

        res.on('data', (chunk) => {
          buffer += chunk;
          const lines = buffer.split('\n');
          buffer = lines.pop();
          for (const line of lines) {
            if (line.startsWith('data: ')) {
              const raw = line.slice(6);
              try {
                events.push(JSON.parse(raw));
              } catch (error) {
                events.push({ raw });
              }
            }
          }
        });

        res.on('end', () => resolve(events));
      },
    );

    req.on('error', reject);
    req.end();

    setTimeout(() => {
      req.destroy();
      resolve(events);
    }, 10000);
  });
}

test.describe('SSE event stream', () => {
  test.skip(!SSE_PORT, 'Set PLAYWRIGHT_SSE_PORT to validate SSE stream');

  test('returns structured SSE payloads', async () => {
    const events = await collectSseEvents({ port: Number(SSE_PORT), path: SSE_PATH });
    expect(events.length).toBeGreaterThan(0);
    expect(events.some((event) => typeof event === 'object')).toBeTruthy();
  });
});
