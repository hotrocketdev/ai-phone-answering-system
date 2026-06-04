# Session Restore Guide — LiveKit HD Spike

**Date:** 2026-06-03
**Purpose:** Restore this session on another PC after a hardware change.

---

## TL;DR

Everything spike-related is already in git. The opencode conversation history is in the opencode data directory. To restore on a new PC:

1. Install opencode.
2. Install Git + Go 1.22+ (or 1.23+).
3. Clone the repo and switch to the spike branch.
4. (Optional) Copy the opencode data directory to restore conversation history.

**Pre-made backup:** A full backup of opencode data + config + SSH keys + LiveKit credentials is on **E:\opencode-backup-2026-06-03\** (created 2026-06-03 ~01:00 UTC). See [Backup section](#backup-on-e-drive-2026-06-03) below.

---

## What is already in git (auto-restored by clone)

The entire spike is on the `feat/livekit-hd-spike` branch, fully committed and pushed to `origin`. Restoring the repo on a new PC gives you:

- All spike source code (`experimental/livekit/{publisher,token-gen,web-client,results,server-notes,README.md}`)
- All spike tests (`pcmsampleprovider_test.go` — 5/5 pass)
- All spike docs (`docs/context/{LIVEKIT_HD_SPIKE_PLAN.md, HANDOVER_CURRENT_STATE.md, VOICE_QUALITY_STACK_STRATEGY.md, ...}`, `docs/PROJECT_STATUS.md`, `experimental/livekit/results/{README.md, BROWSER_AUDIO_TEST_RUNBOOK.md}`)
- `.env.example` templates (placeholders only, no secrets)
- `go.mod` / `go.sum` (pinned dependencies)

**No spike work is lost.** The git history is the canonical session record.

---

## What's NOT in git (needs manual handling)

### 1. Opencode conversation history

Opencode stores session data (conversation, todos, tool calls) in:
- **Windows:** `%LOCALAPPDATA%\opencode\`
- **macOS:** `~/Library/Application Support/opencode/`
- **Linux:** `~/.local/share/opencode/`

Contents:
- `opencode.db` (35.9 MB SQLite — conversation + tool output)
- `snapshot/`, `storage/`, `repos/`, `tool-output/` — caches
- `auth.json` — auth credentials (DON'T copy this to a shared machine)
- `log/` — logs

**To restore on new PC:**
```bash
# On OLD PC
xcopy /E /I /H "%LOCALAPPDATA%\opencode" "D:\backup\opencode-data"

# On NEW PC
# 1. Install opencode first (creates the data dir)
# 2. Stop opencode
# 3. Copy over (skip auth.json if you don't want to share auth)
xcopy /E /I /H "D:\backup\opencode-data\*" "%LOCALAPPDATA%\opencode\"
# 4. Restart opencode
```

**Note:** Copying the opencode.db restores the conversation history but the project path embedded in it must match. If your new PC has a different path to the repo (e.g. `D:\builds\AI-Phone-Answer-System` vs `C:\projects\AI-Phone-Answer-System`), you may need to re-open the project in opencode.

### 2. The LiveKit Cloud credentials

Stored at `experimental/livekit/.env` (gitignored) on the **VPS** at `/opt/ai-voice-receptionist/experimental/livekit/.env`. The VPS is independent of the dev PC — this is already preserved. If you also want them on the new dev PC, create the file locally:

```bash
# On new PC, in the project root
cat > experimental/livekit/.env <<'EOF'
LIVEKIT_URL=wss://ai-voice-assistant-314hy5b3.livekit.cloud
LIVEKIT_API_KEY=APIVQCVnwDyXpAk
LIVEKIT_API_SECRET=RKd65RHCDyXZMeam5CQ4wtEFeo7XrGGhw0W7ELl8eNdB
CARTESIA_API_KEY=
CARTESIA_VOICE_ID=2f251ac3-89a9-4a77-a452-704b474ccd01
CARTESIA_MODEL=sonic-3.5
SPIKE_GREETING_TEXT=Porto Douro Restaurants, Alex speaking. How can I help?
EOF
chmod 600 experimental/livekit/.env  # Unix-style perm; on Windows just lock the file
```

### 3. Local uncommitted working tree (probably safe to discard)

Current local working tree on the old PC has:
- `M .env.example` (unrelated to spike)
- `M backend/tsconfig.tsbuildinfo` (build artifact)
- `?? .understand-anything/` (opencode's local analysis data)
- `?? voice-gateway/gateway-linux` (build artifact)

**None of these are spike-related.** They are safe to discard on the new PC. They will be regenerated as needed.

---

## Step-by-step restore on new PC

### Step 1 — Install prerequisites

- **Git** for Windows: https://git-scm.com/download/win
- **Go 1.22+** (1.23 recommended): https://go.dev/dl/ — install anywhere (e.g. `C:\Program Files\Go`)
- **opencode**: install per https://opencode.ai
- A modern browser for the web client

### Step 2 — Clone the repo and switch to spike branch

```bash
cd C:\builds  # or wherever you want
git clone https://github.com/hotrocketdev/ai-phone-answering-system.git
cd ai-phone-answering-system
git fetch origin
git checkout feat/livekit-hd-spike
git log --oneline -7  # verify the spike commits are present
```

### Step 3 — (Optional) Create local `.env` for the spike

If you want to run the publisher from the new PC (instead of from the VPS):

```bash
# See "LiveKit Cloud credentials" above for the contents
# Place at experimental/livekit/.env (gitignored, never committed)
```

### Step 4 — (Optional) Restore opencode conversation

See "Opencode conversation history" above. Copy the opencode data directory.

### Step 5 — (Optional) Build and run the spike

```bash
cd experimental/livekit/publisher
go build -o publisher.exe .
./publisher.exe
# Or in wait-for-subscriber mode (browser-friendly)
SPIKE_WAIT_FOR_SUBSCRIBER=true ./publisher.exe
```

For the listener token:
```bash
cd experimental/livekit/token-gen
go run . --room voxlane-hd-spike --identity voxlane-listener --subscribe
```

### Step 6 — Open the browser test

Open `experimental/livekit/web-client/index.html` in a browser. If `file://` import errors occur, serve via:

```bash
cd experimental/livekit/web-client
python -m http.server 8765
# Open http://localhost:8765/index.html
```

Full procedure: `docs/experimental/livekit-hd-spike/BROWSER_AUDIO_TEST_RUNBOOK.md`.

---

## What is on the VPS (independent of dev PC)

The VPS at `jorge@srv1194478` already has the spike state:

- Branch: `feat/livekit-hd-spike` checked out at `/opt/ai-voice-receptionist`
- Go 1.23.4 installed in `$HOME/go` (jorge-local, NOT system-wide)
- `experimental/livekit/.env` created (0600 perms, gitignored)
- Production runtime UNTOUCHED (env, binary, services)

You can SSH to the VPS and run the spike from there without the dev PC being involved:

```bash
ssh jorge@srv1194478
export PATH="$HOME/go/bin:$PATH"
cd /opt/ai-voice-receptionist/experimental/livekit/publisher
./publisher.bin
```

---

## What is NOT restored automatically

- The `feat/livekit-hd-spike` branch is the right one to be on for spike work, but it is NOT merged to `main`. If you need to do non-spike production work, switch back:
  ```bash
  git checkout main
  ```
- The stashed `backend/tsconfig.tsbuildinfo` is on the VPS (not the old PC). If you need it on the new PC, SSH to VPS, pop the stash, copy the file.
- The `experimental/livekit/.env` is on the VPS. The new PC will need its own copy if you want to run the spike from there.

---

## Verify the restore

After cloning on the new PC, verify the spike is intact:

```bash
cd ai-phone-answering-system
git log --oneline -7
# Should show:
# d638289 docs: handover + project status — Go 1.23.4 installed on VPS (jorge-local)
# e7f3c53 docs: handover + project status — VPS sync (spike branch pulled)
# 8ff2f3c docs: handover + project status — browser test runbook deployed (iteration 3)
# 89e5c91 feat(spike): browser test runbook + wait-for-subscriber mode
# 3da7dca feat(spike): LiveKit HD one-way audio proof (PCMU intermediate) on feat/livekit-hd-spike
# e0be779 docs: handover + project status — LiveKit HD spike design + scaffold complete
# 32f9ccb docs: LiveKit HD audio spike — design + scaffold on feat/livekit-hd-spike
```

The `docs/context/HANDOVER_CURRENT_STATE.md` has the full session summary, ready to be read by the new opencode instance.

---

## Summary

| What | Where | Restored by |
|---|---|---|
| Spike source code | Git (`feat/livekit-hd-spike` branch) | `git clone` + `git checkout` |
| Spike docs (HANDOVER, PROJECT_STATUS, etc.) | Git | `git clone` |
| Spike tests | Git | `git clone` + `go test` |
| Spike env template | Git (`experimental/livekit/{publisher,}/.env.example`) | `git clone` |
| Spike runtime `.env` with LiveKit creds | VPS (`/opt/ai-voice-receptionist/experimental/livekit/.env`) AND E:\opencode-backup-2026-06-03\spike-env\.env | Recreate locally or copy from backup |
| Go 1.23.4 install | VPS (`$HOME/go`) | Already on VPS, or install locally |
| Production runtime | VPS (env, binary, services) | Unchanged |
| Opencode conversation history | E:\opencode-backup-2026-06-03\opencode-data\ (`C:\Users\jmont\.local\share\opencode\`) | Copy from backup |
| Opencode user config | E:\opencode-backup-2026-06-03\opencode-userconfig\ (`C:\Users\jmont\.config\opencode\`) | Copy from backup |
| SSH keys for VPS | E:\opencode-backup-2026-06-03\ssh\ (`C:\Users\jmont\.ssh\`) | Copy from backup |
| Local working tree (uncommitted) | Local | Safe to discard (none spike-related) |

---

## Backup on E: drive (2026-06-03)

A pre-made backup of all session-related state is on **E:\opencode-backup-2026-06-03\** (created 2026-06-03 ~01:00 UTC, ~240 MB total).

**Contents:**

| Subdir | Size | Source | What it restores |
|---|---|---|---|
| `opencode-data/` | ~191 MB | `C:\Users\jmont\.local\share\opencode\` | OpenCode runtime data: `opencode.db` (SQLite conversation history), `snapshot/`, `storage/`, `repos/`, `tool-output/`, `log/`, `auth.json` |
| `opencode-userconfig/` | ~49 MB | `C:\Users\jmont\.config\opencode\` | OpenCode user-level config + node_modules |
| `opencode-appdata-config/` | <1 KB | `C:\Users\jmont\AppData\Roaming\opencode\` | Desktop OpenCode app config |
| `ssh/` | ~3 KB | `C:\Users\jmont\.ssh\` | SSH keys for VPS + GitHub access |
| `spike-env/.env` | <1 KB | `experimental\livekit\.env` | LiveKit Cloud credentials (NEVER commit) |
| `MANIFEST.md` | ~7 KB | this manifest | Full restore instructions |

**To restore from this backup on a new PC, see `MANIFEST.md` in the backup directory itself.** It has step-by-step copy commands.

**Security note:** This backup contains sensitive material — SSH private keys, LiveKit API secret, OpenCode conversation history. Treat as confidential. Store on an encrypted drive if possible. Do not commit to git. Do not share publicly.

**Removal after successful migration:**
```bash
# On old PC, after verifying the new PC is working
rmdir /S /Q "E:\opencode-backup-2026-06-03"
```

Or keep the backup on E: drive for future migrations.
| Todo list state | Opencode memory | Restored with conversation history |
