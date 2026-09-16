import http from "node:http";

const PORT = 3000;

const server = http.createServer((req, res) => {
  console.log(`[SampleApp] Received request: ${req.method} ${req.url}`);

  if (req.url === "/api/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "healthy", uptime: process.uptime(), timestamp: new Date().toISOString() }));
    return;
  }

  res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
  res.end(`
    <!DOCTYPE html>
    <html lang="ja">
    <head>
      <meta charset="UTF-8">
      <meta name="viewport" content="width=device-width, initial-scale=1.0">
      <title>ユーザー開発アプリケーション</title>
      <style>
        :root {
          --paper: #f6f7fb;
          --card: #ffffff;
          --ink: #14151a;
          --ink-2: #53555d;
          --rule: #e6e8ef;
          --accent: #246a50;
          --radius: 8px;
          --radius-sm: 5px;
          --font-sans: ui-sans-serif, system-ui, -apple-system, sans-serif;
          --font-mono: ui-monospace, monospace;
        }
        @media (prefers-color-scheme: dark) {
          :root {
            --paper: #131316;
            --card: #202127;
            --ink: #ecebe6;
            --ink-2: #a9a8a2;
            --rule: #2c2d33;
            --accent: #6faa8e;
          }
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
          font-family: var(--font-sans);
          background-color: var(--paper);
          color: var(--ink);
          display: flex;
          align-items: center;
          justify-content: center;
          min-height: 100vh;
          padding: 1.5rem;
        }
        .card {
          background-color: var(--card);
          border: 1px solid var(--rule);
          border-radius: var(--radius);
          padding: 2.25rem 2rem;
          max-width: 520px;
          box-shadow: 0 1px 2px rgba(20, 21, 26, 0.05);
        }
        .status-tag {
          display: inline-block;
          font-size: 0.775rem;
          color: var(--accent);
          margin-bottom: 0.75rem;
          font-weight: 500;
        }
        h1 {
          font-size: 1.35rem;
          font-weight: 600;
          margin-bottom: 0.5rem;
          color: var(--ink);
        }
        p {
          color: var(--ink-2);
          font-size: 0.9rem;
          line-height: 1.5;
          margin-bottom: 1rem;
        }
        .code-box {
          background-color: var(--paper);
          border: 1px solid var(--rule);
          border-radius: var(--radius-sm);
          padding: 0.75rem;
          font-family: var(--font-mono);
          color: var(--ink);
          font-size: 0.825rem;
          line-height: 1.5;
        }
      </style>
    </head>
    <body>
      <div class="card">
        <span class="status-tag">稼働中 (ポート ${PORT})</span>
        <h1>ユーザー開発アプリケーション</h1>
        <p>
          このページは <code>workspace.yml</code> の <code>commands.dev</code> によって起動されたプレビュー画面です。
        </p>
        <div class="code-box">
          $ nub sample_app/server.ts<br>
          > 127.0.0.1:${PORT} でリクエスト待機中
        </div>
      </div>
    </body>
    </html>
  `);
});

server.listen(PORT, "127.0.0.1", () => {
  console.log(`[SampleApp] Server listening on http://127.0.0.1:${PORT}`);
});
