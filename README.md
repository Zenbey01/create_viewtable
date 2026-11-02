# SQL View Builder

The Next.js UI can run on Vercel. The Go API should run on a machine that can
reach the private network containing the customer database. For this scaffold,
the Go API is exposed only through an outbound Cloudflare Tunnel; no API port
needs to be opened on the machine.

> This scaffold stores submitted schema metadata only. It does **not** connect
> to customer databases yet. Do not submit customer DDL or real customer data
> until user authentication, per-customer authorization, retention, and backup
> policies are in place. The current UI has no user login.

## Next steps

Continue in this order before using real customer schemas or database
credentials:

1. **Decide the deployment boundary.** Choose whether the personal machine and
   company machine will be separate Vercel projects and Cloudflare Tunnels, or
   whether the company will provide a dedicated server. Keep each environment's
   API token, tunnel, storage, and customer access separate.
2. **Add user authentication and authorization.** Protect the Next.js app with
   the organization's identity provider or another supported login method.
   Authorize every schema operation on the server and scope data to the
   authenticated customer/tenant. Do not rely on a hidden button or the shared
   `API_TOKEN` to identify end users.
3. **Choose durable, backed-up storage.** Replace the Go API's JSON file store
   with a database suitable for the deployment. Add migrations, tenant-scoped
   records, backup/restore procedures, and a retention/deletion policy.
4. **Deploy one isolated environment.** Configure a Cloudflare Tunnel and
   Access Service Auth policy for a machine that is approved to host the
   connector. Set the server-only Vercel secrets, deploy, and verify that
   unauthenticated requests are denied. Do not connect a company environment
   through a personal computer.
5. **Add database connectors only after approval.** Confirm which engines and
   versions are needed, then implement their Go drivers and per-customer
   connection configuration. Store credentials in a secret manager, use
   dedicated read-only accounts, enforce TLS and network allowlists, and never
   return credentials or query results to browser logs.
6. **Test with synthetic data.** Verify tenant isolation, read-only behavior,
   access revocation, tunnel outages, backup/restore, and credential rotation
   before onboarding a customer. Do not use production customer data for these
   tests.

The current handoff point is **step 1**: the scaffold is pushed, but no real
customer database connection, end-user login, tenant isolation, or production
storage has been implemented. Complete steps 1–3 before collecting customer
schema metadata, and complete all steps before connecting to a customer
database.

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

## What Docker Compose runs

[`compose.yaml`](./compose.yaml) starts two containers on a private Docker
network:

- **`api`** builds [`backend/Dockerfile`](./backend/Dockerfile) and runs the Go
  API. It listens on `0.0.0.0:8080` inside the container so the tunnel
  container can reach it. Compose uses `expose`, not `ports`, so port 8080 is
  not published on the host or directly opened to the internet.
- **`cloudflared`** runs the Cloudflare Tunnel using `CF_TUNNEL_TOKEN`. In the
  Cloudflare Zero Trust dashboard, configure the tunnel's public hostname to
  forward to `http://api:8080`; `api` is the Compose service name. The tunnel
  makes an outbound connection from this machine, so no inbound router or
  firewall port-forward is needed.
- **`schema-data`** is a named Docker volume mounted at `/data` in the API
  container. The Go API writes `schemas.json` there, so recreating the
  container does not remove stored schema metadata.

The API container requires `API_TOKEN`; Cloudflare Tunnel requires
`CF_TUNNEL_TOKEN`. Compose reads these from `.env` in the project directory.
Create it from the example and replace the placeholders before starting:

```sh
cp .env.example .env
openssl rand -hex 32
# Put the generated value in API_TOKEN, then add the tunnel token as CF_TUNNEL_TOKEN.
```

Keep `.env` private. Do not paste either value into source code, the browser, or
Vercel's `NEXT_PUBLIC_*` environment variables. Set the same `API_TOKEN` value
as Vercel's server-only `GO_API_TOKEN`.

Useful Compose commands, run from the project root:

```sh
docker compose up -d --build  # Build and start the API and tunnel
docker compose ps             # Check container status
docker compose logs -f        # Follow API and tunnel logs
docker compose down           # Stop containers; keep schema-data volume
```

`docker compose down -v` also removes the `schema-data` volume and its saved
schemas. Only run it if you intend to delete that data.

## API configuration

- `API_TOKEN`: required bearer token for Go API requests.
- `API_ADDR`: Go listen address; defaults to `127.0.0.1:8080`. The Compose
  deployment overrides this to the private Docker network.
- `DATA_DIR`: persistent schema directory; defaults to `data`.
- `GO_API_URL` and `GO_API_TOKEN`: Vercel/Next server-only connector settings.
- `CLOUDFLARE_ACCESS_CLIENT_ID` and
  `CLOUDFLARE_ACCESS_CLIENT_SECRET`: optional as a pair; set both when the
  connector hostname is protected by Cloudflare Access Service Auth.
