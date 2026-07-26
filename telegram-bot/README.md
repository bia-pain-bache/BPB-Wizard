# BPB Telegram Bot

This directory contains the Telegram Bot for deploying and managing BPB Panel instances on Cloudflare.

The bot provides a conversational interface to deploy BPB Panel to Cloudflare Workers or Pages. It sets up the required KV namespaces and manages Cloudflare tokens securely via Telegram.

## Requirements
- Node.js (v18+)
- Cloudflare API Token (with Workers, KV, Pages, and DNS permissions)
- A Telegram Bot Token from [@BotFather](https://t.me/botfather)

## Setup

1. Install dependencies:
   ```bash
   npm install
   ```

2. Create a `.env` file based on `.env.example` or set the following environment variables if deploying directly with Wrangler:
   - `BOT_TOKEN`: Your Telegram Bot API Token.
   - `SESSION_KV`: Cloudflare KV namespace binding for Grammy sessions.

3. Update `wrangler.toml` to add your Cloudflare Account ID and a KV namespace binding for the session storage.

## Deployment

To deploy the bot to Cloudflare Workers:
```bash
npx wrangler deploy
```

Once deployed, set the Telegram webhook to your Worker's URL:
```bash
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook?url=https://<YOUR_WORKER_URL>"
```

## Local Testing

You can test the bot locally using `ts-node-dev`:
```bash
npm run dev
```
