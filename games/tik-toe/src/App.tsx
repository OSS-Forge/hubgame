import { useEffect, useMemo, useRef, useState } from 'react'

type Mode = 'offline' | 'online'
type OfflineMode = 'local' | 'bot'
type OnlineFlow = 'auto' | 'direct'

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
}

type ChatMessage = {
  id: string
  user_id: string
  message?: string
  emoji?: string
  type: string
}

const gatewayURL = import.meta.env.VITE_GATEWAY_URL ?? 'http://localhost:8080'

export function App() {
  const matchSocketRef = useRef<WebSocket | null>(null)
  const chatSocketRef = useRef<WebSocket | null>(null)

  const [screen, setScreen] = useState<'choose' | 'setup' | 'play'>('choose')
  const [mode, setMode] = useState<Mode>('offline')
  const [offlineMode, setOfflineMode] = useState<OfflineMode>('bot')
  const [onlineFlow, setOnlineFlow] = useState<OnlineFlow>('auto')
  const [advanced, setAdvanced] = useState(false)

  const [playerName, setPlayerName] = useState('Player1')
  const [opponentName, setOpponentName] = useState('Player2')
  const [targetUsername, setTargetUsername] = useState('')

  const [boardSize, setBoardSize] = useState(3)
  const [winLength, setWinLength] = useState(3)

  const [token, setToken] = useState('')
  const [status, setStatus] = useState('Select a mode to begin')
  const [loading, setLoading] = useState(false)
  const [match, setMatch] = useState<MatchState | null>(null)
  const [chatOpen, setChatOpen] = useState(false)
  const [chatInput, setChatInput] = useState('')
  const [chat, setChat] = useState<ChatMessage[]>([])

  const myUserID = useMemo(() => slugify(playerName) || 'player1', [playerName])

  useEffect(() => {
    const saved = localStorage.getItem('hubgame.dev.token')
    if (saved) setToken(saved)
  }, [])

  useEffect(() => {
    if (!match || screen !== 'play' || mode !== 'online' || !token) return
    connectRealtime(match.id, token)
    void refreshMatch(match.id)
    void refreshChat(match.id)
    return () => closeRealtime()
  }, [match?.id, mode, screen, token])

  async function ensureToken() {
    if (token) return token
    const res = await fetch(`${gatewayURL}/v1/auth/dev-token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: myUserID, tenant_id: 'hubgame-dev', role: 'developer', ttl_seconds: 86400 }),
    })
    if (!res.ok) throw new Error('Unable to get gateway token')
    const payload = (await res.json()) as { token: string }
    localStorage.setItem('hubgame.dev.token', payload.token)
    setToken(payload.token)
    return payload.token
  }

  async function startMatch() {
    setLoading(true)
    setStatus('Starting match...')
    try {
      if (mode === 'offline') {
        const localMatch: MatchState = {
          id: `offline_${Date.now()}`,
          mode: offlineMode,
          board_size: boardSize,
          win_length: winLength,
          board: createBoard(boardSize),
          player_x: myUserID,
          player_o: offlineMode === 'bot' ? 'bot' : slugify(opponentName) || 'player2',
          current: 'X',
          winner: '',
          move_count: 0,
        }
        setMatch(localMatch)
        setScreen('play')
        setChat([])
        setStatus('Offline match started')
        return
      }

      const auth = await ensureToken()
      if (onlineFlow === 'direct') {
        if (!targetUsername.trim()) throw new Error('Enter target username first')
        const res = await fetchAPI('/v1/tiktoe/matches', auth, {
          method: 'POST',
          body: JSON.stringify({
            mode: 'online',
            board_size: boardSize,
            win_length: winLength,
            player_id: myUserID,
            opponent_id: slugify(targetUsername),
          }),
        })
        const payload = (await res.json()) as MatchState
        setMatch(payload)
        setChat([])
        setScreen('play')
        setStatus(`Direct match ready vs ${payload.player_o || targetUsername}`)
        return
      }

      await fetchAPI('/v1/tiktoe/matchmaking/enqueue', auth, {
        method: 'POST',
        body: JSON.stringify({
          user_id: myUserID,
          display_name: playerName,
          board_size: boardSize,
          win_length: winLength,
        }),
      })
      setStatus('Searching opponent...')

      for (let i = 0; i < 35; i++) {
        const poll = await fetchAPI(
          `/v1/tiktoe/matchmaking/status?user_id=${encodeURIComponent(myUserID)}&board_size=${boardSize}&win_length=${winLength}`,
          auth,
          { method: 'GET' },
        )
        const payload = (await poll.json()) as { status: string; match?: MatchState }
        if (payload.status === 'matched' && payload.match) {
          setMatch(payload.match)
          setChat([])
          setScreen('play')
          setStatus(`Matched: ${payload.match.player_x} vs ${payload.match.player_o}`)
          return
        }
        await sleep(1000)
      }
      setStatus('No match yet. Try again.')
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Failed to start')
    } finally {
      setLoading(false)
    }
  }

  async function playCell(row: number, col: number) {
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

  async function refreshMatch(matchID: string) {
    const auth = await ensureToken()
    const res = await fetchAPI(`/v1/tiktoe/matches/${matchID}`, auth, { method: 'GET' })
    const payload = (await res.json()) as MatchState
    setMatch(payload)
  }

  async function refreshChat(matchID: string) {
    const auth = await ensureToken()
    const res = await fetchAPI(`/v1/tiktoe/matches/${matchID}/chat?limit=40`, auth, { method: 'GET' })
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
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Chat failed')
    }
  }

  function connectRealtime(matchID: string, authToken: string) {
    closeRealtime()
    const wsBase = gatewayURL.replace(/^http/, 'ws')
    const access = encodeURIComponent(authToken)
    const matchSocket = new WebSocket(
      `${wsBase}/v1/events/stream?topic=${encodeURIComponent(`tiktoe.match.${matchID}`)}&access_token=${access}`,
    )
    const chatSocket = new WebSocket(
      `${wsBase}/v1/events/stream?topic=${encodeURIComponent(`tiktoe.match.${matchID}.chat`)}&access_token=${access}`,
    )

    matchSocket.onmessage = (event) => {
      try {
        const e = JSON.parse(event.data) as { payload?: MatchState }
        if (e.payload) setMatch(e.payload)
      } catch {
        // ignore
      }
    }

    chatSocket.onmessage = (event) => {
      try {
        const e = JSON.parse(event.data) as { payload?: ChatMessage }
        if (!e.payload) return
        setChat((prev) => (prev.some((m) => m.id === e.payload!.id) ? prev : [...prev, e.payload!]))
      } catch {
        // ignore
      }
    }

    const reconnect = () => {
      setTimeout(() => {
        if (match?.id === matchID && token === authToken && mode === 'online') {
          connectRealtime(matchID, authToken)
          void refreshMatch(matchID)
          void refreshChat(matchID)
        }
      }, 1200)
    }
    matchSocket.onclose = reconnect
    chatSocket.onclose = reconnect

    matchSocketRef.current = matchSocket
    chatSocketRef.current = chatSocket
  }

  function closeRealtime() {
    if (matchSocketRef.current) {
      matchSocketRef.current.onclose = null
      matchSocketRef.current.close()
      matchSocketRef.current = null
    }
    if (chatSocketRef.current) {
      chatSocketRef.current.onclose = null
      chatSocketRef.current.close()
      chatSocketRef.current = null
    }
  }

  function goToSetup(nextMode: Mode) {
    setMode(nextMode)
    setScreen('setup')
    setStatus(nextMode === 'online' ? 'Set your online preferences' : 'Set your offline preferences')
  }

  function leaveMatch() {
    closeRealtime()
    setMatch(null)
    setChat([])
    setChatOpen(false)
    setScreen('choose')
    setStatus('Select a mode to begin')
  }

  return (
    <div className="min-h-screen px-4 py-6 text-[#3f2d1f] sm:px-6">
      <div className="mx-auto max-w-4xl">
        {screen === 'choose' ? (
          <section className="rounded-3xl border border-[#ccb18f] bg-[#f2e6d7]/90 p-6 text-center shadow-md sm:p-10">
            <h1 className="text-3xl font-semibold sm:text-4xl">Tik-Toe</h1>
            <p className="mt-2 text-sm text-[#6f5038]">Choose how you want to play.</p>
            <div className="mt-6 grid gap-3 sm:grid-cols-2">
              <button
                onClick={() => goToSetup('offline')}
                className="rounded-2xl border border-[#b89c78] bg-[#ead4ba] px-4 py-4 text-lg font-semibold hover:bg-[#e1c7a8]"
              >
                Offline
              </button>
              <button
                onClick={() => goToSetup('online')}
                className="rounded-2xl border border-[#7f5e3d] bg-[#7f5e3d] px-4 py-4 text-lg font-semibold text-[#fff4e7] hover:bg-[#6f5035]"
              >
                Online
              </button>
            </div>
          </section>
        ) : null}

        {screen === 'setup' ? (
          <section className="rounded-3xl border border-[#ccb18f] bg-[#f2e6d7]/90 p-5 shadow-md sm:p-6">
            <div className="flex items-center justify-between gap-3">
              <h2 className="text-2xl font-semibold">{mode === 'online' ? 'Online Setup' : 'Offline Setup'}</h2>
              <button onClick={() => setScreen('choose')} className="rounded-full border border-[#b89c78] bg-[#ecd8bf] px-3 py-1 text-sm">
                Back
              </button>
            </div>

            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              <div>
                <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Your username</label>
                <input
                  value={playerName}
                  onChange={(e) => setPlayerName(e.target.value)}
                  className="mt-1 w-full rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
                />
              </div>
              {mode === 'offline' && offlineMode === 'local' ? (
                <div>
                  <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Second player</label>
                  <input
                    value={opponentName}
                    onChange={(e) => setOpponentName(e.target.value)}
                    className="mt-1 w-full rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
                  />
                </div>
              ) : null}
            </div>

            {mode === 'offline' ? (
              <div className="mt-4">
                <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Offline type</label>
                <div className="mt-1 flex gap-2">
                  <button
                    onClick={() => setOfflineMode('bot')}
                    className={`rounded-full border px-3 py-1.5 text-sm ${offlineMode === 'bot' ? 'bg-[#7f5e3d] text-[#fff4e7]' : 'bg-[#efdfca]'}`}
                  >
                    Bot
                  </button>
                  <button
                    onClick={() => setOfflineMode('local')}
                    className={`rounded-full border px-3 py-1.5 text-sm ${offlineMode === 'local' ? 'bg-[#7f5e3d] text-[#fff4e7]' : 'bg-[#efdfca]'}`}
                  >
                    Local 2P
                  </button>
                </div>
              </div>
            ) : (
              <div className="mt-4 rounded-2xl border border-[#ceb08f] bg-[#f7efdf] p-3">
                <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Online match style</label>
                <div className="mt-2 flex gap-2">
                  <button
                    onClick={() => setOnlineFlow('auto')}
                    className={`rounded-full border px-3 py-1.5 text-sm ${onlineFlow === 'auto' ? 'bg-[#7f5e3d] text-[#fff4e7]' : 'bg-[#efdfca]'}`}
                  >
                    Auto Match
                  </button>
                  <button
                    onClick={() => setOnlineFlow('direct')}
                    className={`rounded-full border px-3 py-1.5 text-sm ${onlineFlow === 'direct' ? 'bg-[#7f5e3d] text-[#fff4e7]' : 'bg-[#efdfca]'}`}
                  >
                    Direct Username
                  </button>
                </div>
                {onlineFlow === 'direct' ? (
                  <input
                    value={targetUsername}
                    onChange={(e) => setTargetUsername(e.target.value)}
                    placeholder="Opponent username"
                    className="mt-2 w-full rounded-xl border border-[#ceb08f] bg-white px-3 py-2"
                  />
                ) : null}
              </div>
            )}

            <button
              onClick={() => setAdvanced((v) => !v)}
              className="mt-4 rounded-full border border-[#b89c78] bg-[#ecd8bf] px-3 py-1 text-sm"
            >
              {advanced ? 'Hide advanced settings' : 'Advanced settings'}
            </button>

            {advanced ? (
              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <div>
                  <label className="text-xs uppercase tracking-[0.14em] text-[#78563a]">Board size</label>
                  <select
                    value={boardSize}
                    onChange={(e) => {
                      const size = Number(e.target.value)
                      setBoardSize(size)
                      setWinLength(Math.min(size, winLength))
                    }}
                    className="mt-1 w-full rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
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
                    className="mt-1 w-full rounded-xl border border-[#ceb08f] bg-[#f7efe3] px-3 py-2"
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

            <button
              disabled={loading}
              onClick={() => void startMatch()}
              className="mt-5 rounded-xl bg-[#6d4c2e] px-5 py-2.5 text-sm font-semibold text-[#fff4e7] disabled:opacity-60"
            >
              {loading ? 'Please wait...' : mode === 'online' ? 'Start Online' : 'Start Offline'}
            </button>
          </section>
        ) : null}

        {screen === 'play' ? (
          <section className="rounded-3xl border border-[#ccb18f] bg-[#f2e6d7]/90 p-4 shadow-md sm:p-6">
            <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-xl font-semibold">{status}</h2>
              <button onClick={leaveMatch} className="rounded-full border border-[#b89c78] bg-[#ecd8bf] px-3 py-1 text-sm">
                Exit
              </button>
            </div>

            {match ? (
              <div
                className="mx-auto grid gap-2"
                style={{ gridTemplateColumns: `repeat(${match.board_size}, minmax(0, 1fr))`, maxWidth: 720 }}
              >
                {match.board.flatMap((row, r) =>
                  row.map((cell, c) => (
                    <button
                      key={`${r}-${c}`}
                      onClick={() => void playCell(r, c)}
                      className="aspect-square rounded-xl border border-[#bfa182] bg-[#f8f0e5] text-3xl font-bold text-[#4f3522] hover:bg-[#f4e9db]"
                    >
                      {cell || ''}
                    </button>
                  )),
                )}
              </div>
            ) : null}
          </section>
        ) : null}
      </div>

      {screen === 'play' && mode === 'online' ? (
        <div className="fixed bottom-4 right-4 z-20">
          <button
            onClick={() => setChatOpen((v) => !v)}
            className="rounded-full border border-[#6f5035] bg-[#6f5035] px-4 py-2 text-sm font-semibold text-[#fff4e7] shadow-lg"
          >
            {chatOpen ? 'Close Chat' : 'Chat'}
          </button>

          {chatOpen ? (
            <div className="mt-2 w-[300px] rounded-2xl border border-[#ccb18f] bg-[#f3e7d7] p-3 shadow-xl">
              <div className="mb-2 flex gap-1">
                {['😀', '🔥', '🎯', '👏', '😅'].map((emoji) => (
                  <button key={emoji} onClick={() => void sendChat(emoji)} className="rounded-md border bg-[#efdcc5] px-2 py-1">
                    {emoji}
                  </button>
                ))}
              </div>

              <div className="max-h-52 space-y-1 overflow-auto rounded-lg border border-[#d2b89a] bg-[#f9f1e6] p-2 text-sm">
                {chat.length === 0 ? <p className="text-[#77563b]">No messages yet</p> : null}
                {chat
                  .slice()
                  .reverse()
                  .map((msg) => (
                    <div key={msg.id} className="rounded-md bg-[#efe0cc] px-2 py-1">
                      <p className="text-xs font-semibold text-[#5c4028]">{msg.user_id}</p>
                      <p>{msg.emoji ? `${msg.emoji} ` : ''}{msg.message || ''}</p>
                    </div>
                  ))}
              </div>

              <div className="mt-2 flex gap-2">
                <input
                  value={chatInput}
                  onChange={(e) => setChatInput(e.target.value)}
                  placeholder="Type message"
                  className="flex-1 rounded-lg border border-[#ceb08f] bg-white px-2 py-1.5"
                />
                <button onClick={() => void sendChat()} className="rounded-lg bg-[#6d4c2e] px-3 py-1.5 text-[#fff4e7]">
                  Send
                </button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
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
