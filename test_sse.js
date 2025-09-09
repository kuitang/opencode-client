#!/usr/bin/env node

const http = require('http');

// Connect to SSE endpoint
const options = {
  hostname: 'localhost',
  port: 6001,
  path: '/event',
  method: 'GET',
  headers: {
    'Accept': 'text/event-stream'
  }
};

console.log('Connecting to SSE endpoint...');
const req = http.request(options, (res) => {
  console.log(`Status: ${res.statusCode}`);
  
  res.setEncoding('utf8');
  let buffer = '';
  
  res.on('data', (chunk) => {
    buffer += chunk;
    const lines = buffer.split('\n');
    buffer = lines.pop(); // Keep incomplete line in buffer
    
    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const data = line.slice(6);
        try {
          const event = JSON.parse(data);
          console.log('Event type:', event.type);
          
          // Check for session-specific data
          if (event.properties) {
            if (event.properties.part && event.properties.part.sessionID) {
              console.log('  - Part sessionID:', event.properties.part.sessionID);
            }
            if (event.properties.info && event.properties.info.sessionID) {
              console.log('  - Info sessionID:', event.properties.info.sessionID);
            }
            if (event.properties.sessionID) {
              console.log('  - Direct sessionID:', event.properties.sessionID);
            }
          }
        } catch (e) {
          console.log('Failed to parse:', data);
        }
      }
    }
  });
  
  res.on('end', () => {
    console.log('Connection closed');
  });
});

req.on('error', (e) => {
  console.error(`Problem with request: ${e.message}`);
});

req.end();

// Listen for 10 seconds then exit
setTimeout(() => {
  console.log('Stopping listener...');
  req.destroy();
  process.exit(0);
}, 10000);