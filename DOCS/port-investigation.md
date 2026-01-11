# Port Investigation Commands

## The Task
Find what's running on port 5432 and stop it.

---

## Step 1: Check What's Listening

```bash
ss -tlnp | grep 5432
```

**Breakdown:**
- `ss` — socket statistics (modern replacement for `netstat`)
- `-t` — show TCP sockets only
- `-l` — show listening sockets only
- `-n` — don't resolve names (show numbers)
- `-p` — show process using the socket
- `| grep 5432` — filter for port 5432

**Output:**
```
LISTEN 0      4096         0.0.0.0:5432      0.0.0.0:*
LISTEN 0      4096            [::]:5432         [::]:*
```

**Meaning:** Something is listening on port 5432 on both IPv4 (0.0.0.0) and IPv6 (::).

---

## Step 2: Identify the Process

```bash
lsof -i :5432
```

**Breakdown:**
- `lsof` — list open files (sockets are files in Unix)
- `-i :5432` — filter for internet connections on port 5432

**Result:** Needs root to see process owned by another user.

---

## Step 3: Check for PostgreSQL

```bash
pgrep -a postgres | head -5
```

**Breakdown:**
- `pgrep` — find processes by name
- `-a` — show full command line
- `postgres` — the pattern to match
- `| head -5` — show first 5 results

**Output:**
```
2969 postgres
3158 postgres: checkpointer
3159 postgres: background writer
3161 postgres: walwriter
3162 postgres: autovacuum launcher
```

**Meaning:** PostgreSQL is running with its worker processes.

---

## Step 4: Try Systemd Stop (Failed)

```bash
sudo systemctl stop postgresql
sudo systemctl disable postgresql
```

**Breakdown:**
- `systemctl` — control systemd services
- `stop` — stop the service now
- `disable` — prevent starting on boot

**Result:** Failed — service doesn't exist as systemd unit.

---

## Step 5: Find How It's Actually Running

```bash
systemctl list-units | grep -i postgres
```

**Breakdown:**
- `list-units` — show all loaded systemd units
- `| grep -i postgres` — case-insensitive search for postgres

**Result:** Empty — not a systemd service.

---

## Step 6: Check for Docker

```bash
ps aux | grep -E "postgres|docker" | grep -v grep | head -10
```

**Breakdown:**
- `ps aux` — list all processes with details
- `grep -E "postgres|docker"` — extended regex, match either word
- `grep -v grep` — exclude the grep process itself
- `| head -10` — limit to 10 results

**Output showed:**
```
/usr/bin/docker-proxy -proto tcp -host-ip 0.0.0.0 -host-port 5432 -container-ip 172.18.0.3 -container-port 5432
```

**Meaning:** Docker is proxying port 5432 to a container.

---

## Step 7: Find the Container

```bash
docker ps --format "table {{.ID}}\t{{.Image}}\t{{.Ports}}" | grep 5432
```

**Breakdown:**
- `docker ps` — list running containers
- `--format "table {{.ID}}\t{{.Image}}\t{{.Ports}}"` — custom output format
  - `{{.ID}}` — container ID
  - `{{.Image}}` — image name
  - `{{.Ports}}` — port mappings
- `| grep 5432` — filter for our port

**Output:**
```
80dbc9b219f6   postgres:16-alpine   0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp
```

**Meaning:** Container `80dbc9b219f6` running `postgres:16-alpine` is using port 5432.

---

## Step 8: Stop the Container

```bash
docker stop 80dbc9b219f6
```

**Breakdown:**
- `docker stop` — send SIGTERM, wait, then SIGKILL
- `80dbc9b219f6` — container ID

---

## Alternative Commands Tried

```bash
docker compose down
```

**Failed because:** No `docker-compose.yml` in current directory.

---

## Summary: Investigation Flow

```
Port check (ss)
      │
      ▼
Process check (lsof) ──► needs root
      │
      ▼
Service name guess (pgrep postgres) ──► found postgres
      │
      ▼
Systemd stop ──► service doesn't exist
      │
      ▼
How is it running? (ps aux) ──► docker-proxy found
      │
      ▼
Find container (docker ps) ──► 80dbc9b219f6
      │
      ▼
Stop container (docker stop)
```

---

## Quick Reference

| Task | Command |
|------|---------|
| What's on port X? | `ss -tlnp \| grep X` |
| Which process? | `lsof -i :X` |
| Find by name | `pgrep -a <name>` |
| Stop systemd service | `sudo systemctl stop <service>` |
| List Docker containers | `docker ps` |
| Stop Docker container | `docker stop <id>` |
| Docker with compose | `docker compose down` |
