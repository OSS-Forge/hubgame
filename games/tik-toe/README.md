# Tik-Toe: Mod Arena (React)

In-house advanced tic-tac-toe for HubGame, rewritten in React + Tailwind + Bun.

## Features
- Minimal first-screen UX with mode selection (`Offline` / `Online`)
- Advanced settings toggle (board size + win length)
- Offline mode:
  - Local 2-player
  - Bot mode
- Online mode:
  - Matchmaking queue
  - Auto-match status polling
  - Server-authoritative moves
  - Match chat + emoji reactions

## Run (dev)
```bash
cd games/tik-toe
bun install
bun run dev
```

## Build
```bash
cd games/tik-toe
bun run build
```

Build output: `dist/index.html` (used by manifest entry).

## Manifest policy
- `author` is set to `hubgame` for in-house games.
