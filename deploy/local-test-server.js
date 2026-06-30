const http = require('http');

const port = Number(process.env.PORT || 19444);

const server = http.createServer((req, res) => {
  const chunks = [];
  req.on('data', chunk => chunks.push(chunk));
  req.on('end', () => {
    const body = Buffer.concat(chunks).toString('utf8');
    const payload = {
      service: 'lobster-local-test-server',
      ok: true,
      method: req.method,
      url: req.url,
      host: req.headers.host || '',
      via: req.headers.via || '',
      xForwardedFor: req.headers['x-forwarded-for'] || '',
      body,
      time: new Date().toISOString()
    };

    res.writeHead(200, {
      'content-type': 'application/json; charset=utf-8',
      'x-local-test-server': 'ok'
    });
    res.end(JSON.stringify(payload, null, 2));
  });
});

server.listen(port, '0.0.0.0', () => {
  console.log(`local test server listening on http://127.0.0.1:${port}`);
});
