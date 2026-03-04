import { useEffect, useMemo, useState } from 'react'

type Mode = 'offline' | 'online'
type OfflineMode = 'local' | 'bot'

type MatchState = {
  id: string
  mode: string
  board_size: number
  win_length: number
  board: string[][]
  player_x: string
  player_o: string
  current: 'X' | 'O'
  winner: '' | 'X' | 'O' | 'draw'
  move_count: number
  updated_at: string
  last_action: string
}

type ChatMessage = {
  id: string
  user_id: string
  message?: string
  emoji?: string
  type: string
  created_at: string
}

const gatewayURL = import.meta.env.VITE_GATEWAY_URL ?? 'http://localhost:8080'

export function App() {
  const [mode, setMode] = useState<Mode>('offline')
  const [offlineMode, setOfflineMode] = useState<OfflineMode>('bot')
  const [advanced, setAdvanced] = useState(false)
  const [boardSize, setBoardSize] = useState(3)
  const [winLength, setWinLength] = useState(3)
  const [playerName, setPlayerName] = useState('Player1')
  const [opponentName, setOpponentName] = useState('Bot')
  const [token, setToken] = useState<string>('')
  const [status, setStatus] = useState('Choose mode to start')
  const [match, setMatch] = useState<MatchState | null>(null)
  const [chat, setChat] = useState<ChatMessage[]>([])
  const [chatInput, setChatInput] = useState('')
  const [loading, setLoading] = useState(false)

  const myUserID = useMemo(() => slugify(playerName) || 'player1', [playerName])

  useEffect(() => {
    const saved = localStorage.getItem('hubgame.dev.token')
    if (saved) setToken(saved)
  }, [])

  useEffect(() => {
    if (!match || mode !== 'online' || !token) return
    const matchTick = setInterval(() => void refreshMatch(match.id), 1500)
    const chatTick = setInterval(() => void refreshChat(match.id), 1500)
    return () => {
      clearInterval(matchTick)
      clearInterval(chatTick)
    }
  }, [match?.id, mode, token])

  async function ensureToken() {
    if (token) return token
    const response = await fetch(`${gatewayURL}/v1/auth/dev-token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: myUserID, tenant_id: 'hubgame-dev', role: 'developer', ttl_seconds: 86400 }),
    })
    if (!response.ok) throw new Error('Unable to get gateway dev token')
    const payload = (await response.json()) as { token: string }
    localStorage.setItem('hubgame.dev.token', payload.token)
    setToken(payload.token)
    return payload.token
  }

  async function startOffline() {
    setLoading(true)
    try {
      const board = createBoard(boardSize)
      const localMatch: MatchState = {
        id: `offline_${Date.now()}`,
        mode: offlineMode,
        board_size: boardSize,
        win_length: winLength,
        board,
        player_x: myUserID,
        player_o: offlineMode === 'bot' ? 'bot' : slugify(opponentName) || 'player2',
        current: 'X',
        winner: '',
        move_count: 0,
        updated_at: new Date().toISOString(),
        last_action: 'offline.start',
      }
      setMatch(localMatch)
      setChat([])
      setStatus(`Offline ${offlineMode} match started`)
    } finally {
      setLoading(false)
    }
  }

  async function startOnline() {
    setLoading(true)
    setStatus('Searching for player...')
    try {
      const auth = await ensureToken()
      await fetchAPI('/v1/tiktoe/matchmaking/enqueue', auth, {
        method: 'POST',
        body: JSON.stringify({
          user_id: myUserID,
          display_name: playerName,
          board_size: boardSize,
          win_length: winLength,
        }),
      })

      let tries = 0
      while (tries < 30) {
        const res = await fetchAPI(
          `/v1/tiktoe/matchmaking/status?user_id=${encodeURIComponent(myUserID)}&board_size=${boardSize}&win_length=${winLength}`,
          auth,
          { method: 'GET' },
        )
        const payload = (await res.json()) as { status: string; match?: MatchState }
        if (payload.status === 'matched' && payload.match) {
          setMatch(payload.match)
          setChat([])
          setStatus(`Matched! ${payload.match.player_x} vs ${payload.match.player_o}`)
          return
        }
        await sleep(1000)
        tries += 1
      }
      setStatus('Still searching. Try again or adjust board settings.')
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Matchmaking failed')
    } finally {
      setLoading(false)
    }
  }

  async function makeMove(row: number, col: number) {
    if (!match || match.winner) return

    if (mode === 'offline') {
      const next = structuredClone(match)
      if (next.board[row][col]) return
      next.board[row][col] = next.current
      next.move_count += 1
      const winner = checkWinner(next.board, next.win_length)
      if (winner) {
        next.winner = winner
        setStatus(`Winner: ${winner}`)
      } else if (isBoardFull(next.board)) {
        next.winner = 'draw'
        setStatus('Draw')
      } else {
        next.current = next.current === 'X' ? 'O' : 'X'
        if (offlineMode === 'bot' && next.current === 'O') {
          const bot = findBotMove(next.board)
          if (bot) {
            next.board[bot.row][bot.col] = 'O'
            next.move_count += 1
            const botWinner = checkWinner(next.board, next.win_length)
            if (botWinner) {
              next.winner = botWinner
              setStatus(`Winner: ${botWinner}`)
            } else if (isBoardFull(next.board)) {
              next.winner = 'draw'
              setStatus('Draw')
            } else {
              next.current = 'X'
            }
          }
        }
      }
      setMatch(next)
      return
    }

    try {
      const auth = await ensureToken()
      const res = await fetchAPI(`/v1/tiktoe/matches/${match.id}/moves`, auth, {
        method: 'POST',
        body: JSON.stringify({ user_id: myUserID, row, col }),
      })
      const payload = (await res.json()) as MatchState
      setMatch(payload)
      if (payload.winner) setStatus(payload.winner === 'draw' ? 'Draw' : `Winner: ${payload.winner}`)
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Move failed')
    }
  }

  async function refreshMatch(id: string) {
    const auth = await ensureToken()
    const res = await fetchAPI(`/v1/tiktoe/matches/${id}`, auth, { method: 'GET' })
    const payload = (await res.json()) as MatchState
    setMatch(payload)
  }

  async function refreshChat(id: string) {
    const auth = await ensureToken()
    const res = await fetchAPI(`/v1/tiktoe/matches/${id}/chat?limit=40`, auth, { method: 'GET' })
    const payload = (await res.json()) as ChatMessage[]
    setChat(payload)
  }

  async function sendChat(emoji?: string) {
    if (!match || mode !== 'online') return
    const message = chatInput.trim()
    if (!message && !emoji) return
    try {
      const auth = await ensureToken()
      await fetchAPI(`/v1/tiktoe/matches/${match.id}/chat`, auth, {
        method: 'POST',
        body: JSON.stringify({ user_id: myUserID, message, emoji: emoji || '' }),
      })
      setChatInput('')
      await refreshChat(match.id)
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Chat failed')
    }
  }

  const currentTurnLabel = match ? `${match.current} turn` : 'No match started'

  return (
    <div className="min-h-screen p-4 text-[#3f2d1f] sm:p-6">
      <div className="mx-auto grid max-w-7xl gap-4 lg:grid-cols-[330px_1fr_320px]">
        <aside className="rounded-3xl border border-[#ccb18f] bg-[#f2e6d7]/90 p-4 shadow-md sm:p-5">
          <h1 className="text-2xl font-semibold">Tik-Toe: Mod Arena</h1>
          <p className="mt-1 text-sm text-[#75563b]">Minimal by default, advanced when needed.</p>

          <div className="mt-4 grid gap-2">
            <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Your name</label>
            <input
              value={playerName}
              onChange={(e) => setPlayerName(e.target.value)}
              className="rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
            />
          </div>

          <div className="mt-4 grid gap-2">
            <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Mode</label>
            <div className="flex gap-2">
              <button
                onClick={() => setMode('offline')}
                className={`rounded-full border px-3 py-2 text-sm ${mode === 'offline' ? 'bg-[#7f5e3d] text-[#fff6ea]' : 'bg-[#efdfca]'}`}
              >
                Offline
              </button>
              <button
                onClick={() => setMode('online')}
                className={`rounded-full border px-3 py-2 text-sm ${mode === 'online' ? 'bg-[#7f5e3d] text-[#fff6ea]' : 'bg-[#efdfca]'}`}
              >
                Online
              </button>
            </div>
          </div>

          {mode === 'offline' ? (
            <div className="mt-4 grid gap-2">
              <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Offline type</label>
              <select
                value={offlineMode}
                onChange={(e) => setOfflineMode(e.target.value as OfflineMode)}
                className="rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
              >
                <option value="bot">Play vs Bot</option>
                <option value="local">Local 2 Players</option>
              </select>
              {offlineMode === 'local' ? (
                <input
                  placeholder="Second player name"
                  value={opponentName}
                  onChange={(e) => setOpponentName(e.target.value)}
                  className="rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
                />
              ) : null}
            </div>
          ) : (
            <p className="mt-4 rounded-xl border border-[#cbb090] bg-[#f7efdf] px-3 py-2 text-sm text-[#6a4e36]">
              Online mode uses backend matchmaking and realtime match refresh.
            </p>
          )}

          <button
            onClick={() => setAdvanced((v) => !v)}
            className="mt-4 rounded-full border border-[#b89c78] bg-[#ecd8bf] px-3 py-1 text-sm"
          >
            {advanced ? 'Hide Advanced' : 'Open Advanced Settings'}
          </button>

          {advanced ? (
            <div className="mt-3 grid gap-3 rounded-2xl border border-[#ceb08f] bg-[#f7efdf] p-3">
              <div>
                <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Board</label>
                <select
                  value={boardSize}
                  onChange={(e) => {
                    const size = Number(e.target.value)
                    setBoardSize(size)
                    setWinLength(Math.min(size, winLength))
                  }}
                  className="mt-1 w-full rounded-xl border border-[#ceb08f] bg-white px-3 py-2"
                >
                  <option value={3}>3x3</option>
                  <option value={4}>4x4</option>
                  <option value={5}>5x5</option>
                </select>
              </div>
              <div>
                <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Win length</label>
                <select
                  value={winLength}
                  onChange={(e) => setWinLength(Number(e.target.value))}
                  className="mt-1 w-full rounded-xl border border-[#ceb08f] bg-white px-3 py-2"
                >
                  {[3, 4, 5].filter((v) => v <= boardSize).map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          ) : null}

          <div className="mt-4 flex gap-2">
            <button
              disabled={loading}
              onClick={() => void (mode === 'offline' ? startOffline() : startOnline())}
              className="rounded-xl bg-[#6d4c2e] px-4 py-2 text-sm font-semibold text-[#fff4e7] disabled:opacity-60"
            >
              {loading ? 'Please wait...' : mode === 'offline' ? 'Start Offline' : 'Find Match'}
            </button>
            <button
              onClick={() => {
                setMatch(null)
                setChat([])
                setStatus('Reset complete')
              }}
              className="rounded-xl border border-[#b89c78] bg-[#ecd8bf] px-4 py-2 text-sm"
            >
              Reset
            </button>
          </div>

          <p className="mt-4 rounded-xl border border-[#c9ab86] bg-[#ecdbc6] px-3 py-2 text-sm text-[#5f4228]">{status}</p>
        </aside>

        <section className="rounded-3xl border border-[#ccb18f] bg-[#f2e6d7]/90 p-4 shadow-md sm:p-5">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xl font-semibold">Board</h2>
            <p className="text-sm text-[#6b4d34]">{currentTurnLabel}</p>
          </div>

          {match ? (
            <div
              className="grid gap-2"
              style={{
                gridTemplateColumns: `repeat(${match.board_size}, minmax(0, 1fr))`,
                maxWidth: 720,
              }}
            >
              {match.board.flatMap((row, r) =>
                row.map((cell, c) => (
                  <button
                    key={`${r}-${c}`}
                    onClick={() => void makeMove(r, c)}
                    className="aspect-square rounded-xl border border-[#bfa182] bg-[#f8f0e5] text-3xl font-bold text-[#4f3522] transition hover:bg-[#f4e9db]"
                  >
                    {cell || ''}
                  </button>
                )),
              )}
            </div>
          ) : (
            <p className="text-[#6f5038]">No active match yet.</p>
          )}
        </section>

        <aside className="rounded-3xl border border-[#ccb18f] bg-[#f2e6d7]/90 p-4 shadow-md sm:p-5">
          <h2 className="text-xl font-semibold">Chat & Emoji</h2>
          <p className="mt-1 text-sm text-[#6f5038]">Available in online mode.</p>

          <div className="mt-3 flex gap-2">
            {['😀', '🔥', '🎯', '👏', '😅'].map((emoji) => (
              <button
                key={emoji}
                onClick={() => void sendChat(emoji)}
                className="rounded-lg border border-[#b89c78] bg-[#ecd8bf] px-2 py-1 text-lg"
              >
                {emoji}
              </button>
            ))}
          </div>

          <div className="mt-3 flex gap-2">
            <input
              value={chatInput}
              onChange={(e) => setChatInput(e.target.value)}
              placeholder="Type message"
              className="flex-1 rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
            />
            <button onClick={() => void sendChat()} className="rounded-xl bg-[#6d4c2e] px-3 py-2 text-[#fff4e7]">
              Send
            </button>
          </div>

          <div className="mt-4 max-h-[420px] space-y-2 overflow-auto pr-1">
            {chat.length === 0 ? (
              <p className="text-sm text-[#6f5038]">No chat yet.</p>
            ) : (
              chat
                .slice()
                .reverse()
                .map((msg) => (
                  <div key={msg.id} className="rounded-xl border border-[#d2b89a] bg-[#f7efe4] px-3 py-2 text-sm">
                    <p className="font-medium text-[#553a24]">{msg.user_id}</p>
                    <p>{msg.emoji ? `${msg.emoji} ` : ''}{msg.message || ''}</p>
                  </div>
                ))
            )}
          </div>
        </aside>
      </div>
    </div>
  )
}

async function fetchAPI(path: string, token: string, init: RequestInit) {
  const response = await fetch(`${gatewayURL}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
      ...(init.headers || {}),
    },
  })
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Request failed (${response.status})`)
  }
  return response
}

function createBoard(size: number) {
  return Array.from({ length: size }, () => Array(size).fill(''))
}

function checkWinner(board: string[][], winLength: number): '' | 'X' | 'O' {
  const n = board.length
  const dirs = [
    [0, 1],
    [1, 0],
    [1, 1],
    [1, -1],
  ]
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      const symbol = board[r][c]
      if (!symbol) continue
      for (const [dr, dc] of dirs) {
        let ok = true
        for (let k = 1; k < winLength; k++) {
          const nr = r + dr * k
          const nc = c + dc * k
          if (nr < 0 || nr >= n || nc < 0 || nc >= n || board[nr][nc] !== symbol) {
            ok = false
            break
          }
        }
        if (ok) return symbol as 'X' | 'O'
      }
    }
  }
  return ''
}

function isBoardFull(board: string[][]) {
  return board.every((row) => row.every((cell) => cell !== ''))
}

function findBotMove(board: string[][]) {
  const center = Math.floor(board.length / 2)
  if (!board[center][center]) return { row: center, col: center }
  for (let r = 0; r < board.length; r++) {
    for (let c = 0; c < board.length; c++) {
      if (!board[r][c]) return { row: r, col: c }
    }
  }
  return null
}

function slugify(v: string) {
  return v
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
