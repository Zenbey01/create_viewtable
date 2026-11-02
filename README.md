# SQL View Builder

The Next.js UI can run on Vercel. The Go API should run on a machine that can
reach the private network containing the customer database. For this scaffold,
the Go API is exposed only through an outbound Cloudflare Tunnel; no API port
needs to be opened on the machine.

> This scaffold stores submitted schema metadata only. It does **not** connect
> to customer databases yet. Do not submit customer DDL or real customer data
> until user authentication, per-customer authorization, retention, and backup
> policies are in place. The current UI has no user login.

## Run locally

Create a random API token and set it in both terminals:

```sh
export API_TOKEN="$(openssl rand -hex 32)"
export GO_API_TOKEN="$API_TOKEN"
```

Start the Go API:

```sh
cd backend
go run .
```

In another terminal, from the project root:

```sh
npm install
npm run dev
```

Open <http://localhost:3000>. `GO_API_URL` defaults to
`http://localhost:8080`. The Next.js route handlers keep `GO_API_TOKEN` on the
server and never send it to the browser.

## Vercel + a private/company machine

1. Deploy the Next.js project to Vercel.
2. On the machine that can reach the database network, install Docker and
   `cloudflared`, then authenticate `cloudflared` with the Cloudflare account.
3. Create a Cloudflare Tunnel in Zero Trust and add a public hostname such as
   `connector.example.com`, with service `http://api:8080`. Configure that
   hostname in the tunnel dashboard; the `api` name is the Go container on the
   Compose network.
4. Create a Cloudflare Access **Service Auth** policy for the connector
   hostname and generate a service token. The Vercel server route sends these
   credentials to Cloudflare; they are never exposed to the browser.
5. In this directory, copy `.env.example` to `.env`. Replace both values with
   fresh secrets. Keep `.env` only on the private machine; do not commit or
   upload it to Vercel.
6. Start the connector on the private machine:

   ```sh
   docker compose up -d --build
   ```

7. In Vercel project settings, add these **server-side** environment variables
   for the production deployment:

   ```text
   GO_API_URL=https://connector.example.com
   GO_API_TOKEN=<the same API_TOKEN from the private machine>
   CLOUDFLARE_ACCESS_CLIENT_ID=<Cloudflare service-token client ID>
   CLOUDFLARE_ACCESS_CLIENT_SECRET=<Cloudflare service-token secret>
   ```

   Do not prefix these with `NEXT_PUBLIC_`. Redeploy after setting them.
8. Enable Vercel Deployment Protection or add application login before sharing
   the site. Cloudflare Access on the connector protects the Go endpoint, but
   does not authenticate users of the Vercel UI.

The compose file publishes no host port and persists schema metadata in a
Docker volume. One Vercel deployment points to one `GO_API_URL`. To use both a
personal machine and a company machine at the same time, deploy separate Vercel
projects (or separately managed deployment environments), each with its own
connector URL, API token, and Cloudflare Access service token. Do not route a
company deployment through a personal machine. For production, pin the
Cloudflare Tunnel image to a reviewed version instead of using `latest`.

## API configuration

- `API_TOKEN`: required bearer token for Go API requests.
- `API_ADDR`: Go listen address; defaults to `127.0.0.1:8080`. The Compose
  deployment overrides this to the private Docker network.
- `DATA_DIR`: persistent schema directory; defaults to `data`.
- `GO_API_URL` and `GO_API_TOKEN`: Vercel/Next server-only connector settings.
- `CLOUDFLARE_ACCESS_CLIENT_ID` and
  `CLOUDFLARE_ACCESS_CLIENT_SECRET`: optional as a pair; set both when the
  connector hostname is protected by Cloudflare Access Service Auth.
