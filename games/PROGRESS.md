# Games Progress

## 2026-03-04
- Added first in-house game package: `tik-toe`.
- Implemented advanced tic-tac-toe gameplay with modifiers, timer, bots, and configurable board sizes.
- Added game manifest with in-house author policy (`author: hubgame`).
- Added publisher script to validate games and generate `games/.published/index.json`.
- Added sync script to export games into `web/public/games` plus `web/public/fallback-catalog.json`.
- Rewrote `tik-toe` into React + Tailwind + Bun with simple-first UI and advanced settings toggle.
- Added online mode support in game client (matchmaking, backend match sync, chat, emoji).
