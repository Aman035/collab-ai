# Two-machine collab demo, on one laptop, in Docker

Two containers on a custom bridge network. Each one is a "user on the
internet" with a distinct IP, its own Claude Code login, and the
Gensyn Axl daemon routing peer-to-peer between them. The demo story
for the audience is: alice and bob are on different machines, neither
of them is hosting a coordination server, and their two Claude Code
sessions are talking to each other directly.

## What's in here

| File | Purpose |
|---|---|
| `Dockerfile` | Multi-stage image: builds collab + axl-node, then bakes in node + claude-code |
| `Dockerfile.dockerignore` | Keeps the build context lean (repo root minus web, docs, git, etc.) |
| `docker-compose.yml` | Two services (alice, bob) on a custom `internet` bridge with explicit IPs |

Containers, IPs, and credentials:

| Container | Hostname       | IP            | Claude account     |
|-----------|----------------|---------------|--------------------|
| `alice`   | `alice-laptop` | `10.42.0.11`  | account A (yours)  |
| `bob`     | `bob-laptop`   | `10.42.0.22`  | account B (other)  |

`/root/.claude` and `/root/.collab` are mounted on per-container named
volumes, so the Claude login + the session state survive `docker compose
down/up` cycles. You only have to log in once per account.


## Pre-flight

You need Docker Desktop running, and two Claude accounts ready to log
in (one for each container). The first build downloads the Go
toolchain, npm + claude-code, and clones gensyn-ai/axl: budget about
3 to 5 minutes for the cold build.


## Step 1: build and start

From the repo root:

```bash
docker compose -f demo/docker-compose.yml up -d --build
```

When it finishes, both containers are running and idle (`sleep
infinity`). Confirm:

```bash
docker compose -f demo/docker-compose.yml ps
```


## Step 2: open two terminals

```bash
# terminal 1
docker compose -f demo/docker-compose.yml exec alice bash

# terminal 2
docker compose -f demo/docker-compose.yml exec bob bash
```

Inside each container you can prove the "different IP" story for the
audience:

```bash
hostname           # alice-laptop / bob-laptop
ip -4 addr show    # 10.42.0.11   / 10.42.0.22
```


## Step 3: log in to Claude (one per container)

In each container:

```bash
claude login
```

Claude Code prints a URL plus a one-time code. Open the URL in a
browser on your laptop, paste the code, and approve. The login lands
in `/root/.claude` inside the container, which is on its named volume
so it persists.

Do this once in `alice` with account A, and once in `bob` with
account B.

If interactive login does not work for some reason (some networks
block the OAuth callback), the fastest fallback is to put two API
keys in `~/.collab-demo.env` on the host and uncomment the
`env_file` lines in `docker-compose.yml`. Each container then runs
under `ANTHROPIC_API_KEY` instead of an OAuth seat. The trade-off:
the audience sees "API key" not "Claude Pro account."


## Step 4: start the collab session

In **alice**'s terminal:

```bash
collab create alice --public-addr tls://10.42.0.11:9001
```

The TUI opens. Press `[c]` to copy the invite. The invite is also
printed at the top of the screen. Copy it.

In **bob**'s terminal:

```bash
collab join COLLAB-...  bob
```

Paste the invite where `COLLAB-...` is. Bob's TUI opens, and within
a second or two both peer rosters show each other.


## Step 5: launch the agents

Press `[a]` in each TUI. Each container launches its own Claude Code
session, configured with the per-session `CLAUDE.md` collab writes
that primes the agent on the five MCP tools (`get_shared_log`,
`post_to_log`, `ask_peer`, `respond_to_peer`, `set_status`).

Now drive the demo:

- In one Claude session, ask it to read a file in `~/collab/<session>/shared/`.
  It will `post_to_log` to announce, then read.
- In the other Claude session, ask it to make a change. Watch the
  edits propagate via fsnotify across the wire.
- Have one ask the other a question via `ask_peer`. The other answers
  with `respond_to_peer`. Both events show up in both logs.

The audience sees: two terminals, two distinct IPs, two distinct
Claude accounts, two agents talking to each other peer-to-peer with
no central server in between.


## Tear down

```bash
docker compose -f demo/docker-compose.yml down
```

Volumes (and therefore Claude logins + session state) survive. To wipe
everything including the logins:

```bash
docker compose -f demo/docker-compose.yml down -v
```


## Troubleshooting

**The build is slow on the first run.** Cold build is mostly Go
fetching deps + cloning gensyn-ai/axl. Subsequent builds reuse cached
layers.

**`claude login` opens a URL that won't load.** That's the OAuth
callback host trying to talk to your machine. Open the URL on the
host, complete the flow, paste the code back. If your corp network
blocks it, fall back to `ANTHROPIC_API_KEY` (see step 3).

**Bob can't find alice.** Confirm both are on the same network:
`docker network inspect collab-demo_internet`. Confirm alice's
`--public-addr` matches her container IP (`10.42.0.11`). Confirm Axl
is running inside alice: `docker exec alice pgrep -af axl-node`.

**Want to rebuild after editing collab source.** From the repo root:
`docker compose -f demo/docker-compose.yml build --no-cache && docker compose -f demo/docker-compose.yml up -d`.
