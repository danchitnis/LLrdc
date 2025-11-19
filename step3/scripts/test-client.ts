import WebSocket from 'ws';

const ws = new WebSocket('ws://localhost:8080');

ws.on('open', () => {
    console.log('Connected to server');
    // Send Enter key
    ws.send(JSON.stringify({ type: 'keydown', key: 'Enter' }));
});

ws.on('message', (data) => {
    console.log('Received message of size:', data.toString().length);
    // We just want to send the key and see the server log, so we can exit after a bit
    setTimeout(() => {
        ws.close();
        process.exit(0);
    }, 1000);
});

ws.on('error', (err) => {
    console.error('Connection error:', err);
    process.exit(1);
});
