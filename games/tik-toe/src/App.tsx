import { useEffect, useMemo, useRef, useState, useCallback } from 'react'
import { ArrowLeft, Bot, MessageCircle, Play, SendHorizontal, Settings2, Sword, UserRound, Wifi, X, RotateCcw, Home, Users } from 'lucide-react'

type Mode = 'offline' | 'online'
type OfflineMode = 'local' | 'bot'
type OnlineFlow = 'auto' | 'direct'
type BotDifficulty = 'easy' | 'medium' | 'hard'

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

type WinningLine = [number, number][]
type DiscoverablePlayer = {
  user_id: string
  display_name: string
  available: boolean
  last_seen_at: string
}

const TOKEN_STORAGE_KEY = 'hubgame.dev.token'
const gatewayURL = resolveGatewayHTTPBase(import.meta.env.VITE_GATEWAY_URL)
const gatewayWSBase = resolveGatewayWSBase(gatewayURL)

export function App() {
  const matchSocketRef = useRef<WebSocket | null>(null)
  const chatSocketRef = useRef<WebSocket | null>(null)

  const [screen, setScreen] = useState<'choose' | 'setup' | 'play'>('choose')
  const [mode, setMode] = useState<Mode>('offline')
  const [offlineMode, setOfflineMode] = useState<OfflineMode>('bot')
  const [botDifficulty, setBotDifficulty] = useState<BotDifficulty>('medium')
  const [onlineFlow, setOnlineFlow] = useState<OnlineFlow>('auto')
  const [advanced, setAdvanced] = useState(false)

  const [playerName, setPlayerName] = useState(() => getDefaultPlayerName())
  const [opponentName, setOpponentName] = useState('Player2')
  const [targetUsername, setTargetUsername] = useState('')

  const [boardSize, setBoardSize] = useState(3)
  const [winLength, setWinLength] = useState(3)

  const [token, setToken] = useState('')
  const [status, setStatus] = useState('Choose a mode to begin')
  const [loading, setLoading] = useState(false)
  const [match, setMatch] = useState<MatchState | null>(null)
  const [chatOpen, setChatOpen] = useState(false)
  const [chatInput, setChatInput] = useState('')
  const [chat, setChat] = useState<ChatMessage[]>([])
  const [winningLine, setWinningLine] = useState<WinningLine>([])
  const [lastMove, setLastMove] = useState<[number, number] | null>(null)
  const [playerSearch, setPlayerSearch] = useState('')
  const [discoverablePlayers, setDiscoverablePlayers] = useState<DiscoverablePlayer[]>([])
  const [presenceEnabled, setPresenceEnabled] = useState(true)
  const myUserID = useMemo(() => slugify(playerName) || 'player1', [playerName])

  useEffect(() => {
    const saved = localStorage.getItem(TOKEN_STORAGE_KEY)
    if (saved) setToken(saved)
  }, [])

  useEffect(() => {
    if (playerName.trim()) {
      sessionStorage.setItem('hubgame.player_name', playerName.trim())
    }
  }, [playerName])

  useEffect(() => {
    if (!match || screen !== 'play' || mode !== 'online' || !token) return
    connectRealtime(match.id, token)
    void refreshMatch(match.id)
    void refreshChat(match.id)
    return () => closeRealtime()
  }, [match?.id, mode, screen, token])

  useEffect(() => {
    if (screen !== 'setup' || mode !== 'online' || !presenceEnabled) {
      return
    }

    let cancelled = false
    let intervalID = 0

    const syncPresence = async () => {
      try {
        await ensureToken()
        await callAuthed('/v1/tiktoe/presence', {
          method: 'POST',
          body: JSON.stringify({
            user_id: myUserID,
            display_name: playerName,
            available: true,
          }),
        })
        const res = await callAuthed(
          `/v1/tiktoe/players?exclude_user_id=${encodeURIComponent(myUserID)}&q=${encodeURIComponent(playerSearch)}&limit=12`,
          { method: 'GET' },
        )
        const payload = (await res.json()) as DiscoverablePlayer[]
        if (!cancelled) {
          setDiscoverablePlayers(payload)
        }
      } catch {
        if (!cancelled) {
          setDiscoverablePlayers([])
        }
      }
    }

    void syncPresence()
    intervalID = window.setInterval(() => {
      void syncPresence()
    }, 10000)

    return () => {
      cancelled = true
      window.clearInterval(intervalID)
    }
  }, [screen, mode, presenceEnabled, playerSearch, myUserID, playerName])

  useEffect(() => {
    if (match?.winner && match.winner !== 'draw') {
      const line = findWinningLine(match.board, match.win_length, match.winner)
      setWinningLine(line)
    } else {
      setWinningLine([])
    }
  }, [match?.winner, match?.board, match?.win_length])

  async function ensureToken(forceRefresh = false) {
    if (!forceRefresh && token) return token
    const res = await fetch(`${gatewayURL}/v1/auth/dev-token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: myUserID, tenant_id: 'hubgame-dev', role: 'developer', ttl_seconds: 86400 }),
    })

    if (!res.ok) {
      const reason = await res.text()
      const message =
        res.status === 404
          ? 'Online mode unavailable: gateway dev auth endpoint is off. Set HUBGAME_ENABLE_DEV_AUTH=true on gateway.'
          : `Unable to get gateway token (${res.status}). ${reason || 'Check gateway/controller services.'}`
      throw new Error(message)
    }

    const payload = (await res.json()) as { token: string }
    localStorage.setItem(TOKEN_STORAGE_KEY, payload.token)
    setToken(payload.token)
    return payload.token
  }

  async function callAuthed(path: string, init: RequestInit) {
    let auth = await ensureToken()
    try {
      return await fetchAPI(path, auth, init)
    } catch (err) {
      const message = err instanceof Error ? err.message.toLowerCase() : ''
      if (!message.includes('401') && !message.includes('invalid token') && !message.includes('missing bearer')) {
        throw err
      }
      localStorage.removeItem(TOKEN_STORAGE_KEY)
      setToken('')
      auth = await ensureToken(true)
      return fetchAPI(path, auth, init)
    }
  }

  async function startMatch() {
    setLoading(true)
    setStatus('Preparing match...')
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
        setStatus(offlineMode === 'bot' ? `Your turn against ${labelBotDifficulty(botDifficulty)} bot` : 'Your turn')
        setLastMove(null)
        setWinningLine([])
        return
      }

      await ensureToken(true)

      if (onlineFlow === 'direct') {
        if (!targetUsername.trim()) throw new Error('Enter target username first')
        const res = await callAuthed('/v1/tiktoe/matches', {
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
        setStatus('Match ready')
        setLastMove(null)
        setWinningLine([])
        return
      }

      await callAuthed('/v1/tiktoe/matchmaking/enqueue', {
        method: 'POST',
        body: JSON.stringify({
          user_id: myUserID,
          display_name: playerName,
          board_size: boardSize,
          win_length: winLength,
        }),
      })
      setStatus('Finding opponent...')

      for (let i = 0; i < 35; i++) {
        const poll = await callAuthed(
          `/v1/tiktoe/matchmaking/status?user_id=${encodeURIComponent(myUserID)}&board_size=${boardSize}&win_length=${winLength}`,
          { method: 'GET' },
        )
        const payload = (await poll.json()) as { status: string; match?: MatchState }
        if (payload.status === 'matched' && payload.match) {
          setMatch(payload.match)
          setChat([])
          setScreen('play')
          setStatus('Match found')
          setLastMove(null)
          setWinningLine([])
          return
        }
        await sleep(1000)
      }
      setStatus('No opponent yet. Use a different username in each tab and try again.')
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Failed to start')
    } finally {
      setLoading(false)
    }
  }

  async function playCell(row: number, col: number) {
    if (!match || match.winner || loading) return
    if (match.board[row][col] !== '') return
    if (mode === 'offline' && match.current !== 'X') return

    setLoading(true)
    setLastMove([row, col])

    if (mode === 'offline') {
      const next = structuredClone(match)
      next.board[row][col] = next.current
      next.move_count += 1
      const winner = checkWinner(next.board, next.win_length)

      if (winner) {
        next.winner = winner
        setStatus(`Winner: ${winner}`)
        const line = findWinningLine(next.board, next.win_length, winner)
        setWinningLine(line)
      } else if (isBoardFull(next.board)) {
        next.winner = 'draw'
        setStatus('Draw')
      } else {
        next.current = next.current === 'X' ? 'O' : 'X'
        setStatus(next.current === 'X' ? 'Your turn' : 'Opponent turn')

        if (offlineMode === 'bot' && next.current === 'O') {
          await sleep(300)
          const bot = findBotMove(next.board, next.win_length, botDifficulty)
          if (bot) {
            next.board[bot.row][bot.col] = 'O'
            next.move_count += 1
            setLastMove([bot.row, bot.col])
            const botWinner = checkWinner(next.board, next.win_length)
            if (botWinner) {
              next.winner = botWinner
              setStatus(`Winner: ${botWinner}`)
              const line = findWinningLine(next.board, next.win_length, botWinner)
              setWinningLine(line)
            } else if (isBoardFull(next.board)) {
              next.winner = 'draw'
              setStatus('Draw')
            } else {
              next.current = 'X'
              setStatus('Your turn')
            }
          }
        }
      }

      setMatch(next)
      setLoading(false)
      return
    }

    try {
      const res = await callAuthed(`/v1/tiktoe/matches/${match.id}/moves`, {
        method: 'POST',
        body: JSON.stringify({ user_id: myUserID, row, col }),
      })
      const payload = (await res.json()) as MatchState
      setMatch(payload)
      if (payload.winner) {
        setStatus(payload.winner === 'draw' ? 'Draw' : `Winner: ${payload.winner}`)
        if (payload.winner !== 'draw') {
          const line = findWinningLine(payload.board, payload.win_length, payload.winner)
          setWinningLine(line)
        }
      } else {
        setStatus(payload.current === 'X' ? 'X turn' : 'O turn')
      }
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Move failed')
      setLastMove(null)
    } finally {
      setLoading(false)
    }
  }

  async function refreshMatch(matchID: string) {
    const res = await callAuthed(`/v1/tiktoe/matches/${matchID}`, { method: 'GET' })
    const payload = (await res.json()) as MatchState
    setMatch(payload)
  }

  async function refreshChat(matchID: string) {
    const res = await callAuthed(`/v1/tiktoe/matches/${matchID}/chat?limit=40`, { method: 'GET' })
    const payload = (await res.json()) as ChatMessage[]
    setChat(payload)
  }

  async function sendChat() {
    if (!match || mode !== 'online') return
    const message = chatInput.trim()
    if (!message) return
    try {
      await callAuthed(`/v1/tiktoe/matches/${match.id}/chat`, {
        method: 'POST',
        body: JSON.stringify({ user_id: myUserID, message, emoji: '' }),
      })
      setChatInput('')
    } catch (err) {
      setStatus(err instanceof Error ? err.message : 'Chat failed')
    }
  }

  function connectRealtime(matchID: string, authToken: string) {
    closeRealtime()

    const access = encodeURIComponent(authToken)
    const matchSocket = new WebSocket(
      `${gatewayWSBase}/v1/events/stream?topic=${encodeURIComponent(`tiktoe.match.${matchID}`)}&access_token=${access}`,
    )
    const chatSocket = new WebSocket(
      `${gatewayWSBase}/v1/events/stream?topic=${encodeURIComponent(`tiktoe.match.${matchID}.chat`)}&access_token=${access}`,
    )

    matchSocket.onmessage = (event) => {
      try {
        const e = JSON.parse(event.data) as { payload?: MatchState }
        if (e.payload) setMatch(e.payload)
      } catch {
        // ignore malformed frames
      }
    }

    chatSocket.onmessage = (event) => {
      try {
        const e = JSON.parse(event.data) as { payload?: ChatMessage }
        if (!e.payload) return
        setChat((prev) => (prev.some((m) => m.id === e.payload!.id) ? prev : [...prev, e.payload!]))
      } catch {
        // ignore malformed frames
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
    setStatus(nextMode === 'online' ? 'Configure online mode' : 'Configure offline mode')
  }

  function leaveMatch() {
    closeRealtime()
    setMatch(null)
    setChat([])
    setChatOpen(false)
    setScreen('choose')
    setStatus('Choose a mode to begin')
  }

  function resetGame() {
    if (!match) return
    const reset: MatchState = {
      ...match,
      board: createBoard(match.board_size),
      current: 'X',
      winner: '',
      move_count: 0,
    }
    setMatch(reset)
    setLastMove(null)
    setWinningLine([])
    setStatus('Your turn')
  }

  const isCellDisabled = useCallback((row: number, col: number) => {
    if (!match || match.winner || loading) return true
    if (match.board[row][col] !== '') return true
    if (mode === 'offline' && match.current !== 'X') return true
    return false
  }, [match, loading, mode])

  return (
    <div className="min-h-screen px-4 py-6 text-[#3f2d1f] sm:px-6 safe-top safe-bottom">
      <div className="mx-auto flex min-h-[calc(100vh-3rem)] max-w-5xl items-center justify-center">
        {/* Mode Selection Screen */}
        {screen === 'choose' ? (
          <section className="card-elegant w-full max-w-2xl p-6 text-center screen-enter sm:p-10">
            <h1 className="text-3xl font-bold tracking-tight text-[#6f5035] sm:text-4xl">Tik-Toe</h1>
            <p className="mt-2 text-sm text-[#6f5038]">Choose how you want to play.</p>

            <div className="mx-auto mt-8 grid max-w-xl gap-4 sm:grid-cols-2">
              <button
                onClick={() => goToSetup('offline')}
                className="mode-card touch-target flex aspect-square flex-col items-center justify-center gap-4 rounded-3xl p-6 text-lg font-semibold"
              >
                <Bot size={30} />
                Offline
              </button>
              <button
                onClick={() => goToSetup('online')}
                className="mode-card mode-card-primary touch-target flex aspect-square flex-col items-center justify-center gap-4 rounded-3xl p-6 text-lg font-semibold"
              >
                <Wifi size={30} />
                Online
              </button>
            </div>
          </section>
        ) : null}

        {/* Setup Screen */}
        {screen === 'setup' ? (
          <section className="card-elegant w-full max-w-2xl p-6 screen-enter sm:p-8">
            <div className="mb-6 grid grid-cols-[auto_1fr_auto] items-center gap-3">
              <button
                onClick={() => setScreen('choose')}
                className="touch-target inline-flex items-center gap-2 rounded-xl border border-[#d2b89a] bg-white px-4 py-2.5 text-sm font-semibold text-[#6f5035] hover:bg-[#f9f1e6] press-scale"
              >
                <ArrowLeft size={18} />
                Back
              </button>
              <h2 className="text-xl font-bold text-[#6f5035]">
                {mode === 'online' ? 'Online Match' : 'Offline Game'}
              </h2>
              <div />
            </div>

            <div className="space-y-6">
              {/* Player Name */}
              <div>
                <label className="mb-2 block text-sm font-semibold text-[#5c4028]">Your Name</label>
                <input
                  type="text"
                  value={playerName}
                  onChange={(e) => setPlayerName(e.target.value)}
                  placeholder="Enter your name"
                  className="input-elegant w-full"
                />
              </div>

              {/* Offline Mode Options */}
              {mode === 'offline' ? (
                <>
                  <div>
                    <label className="mb-3 block text-sm font-semibold text-[#5c4028]">Opponent</label>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <button
                        onClick={() => setOfflineMode('bot')}
                        className={`touch-target flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 px-4 py-3.5 font-semibold transition-smooth ${
                          offlineMode === 'bot'
                            ? 'border-[#6f5035] bg-[#6f5035] text-white'
                            : 'border-[#d2b89a] bg-white text-[#6f5035] hover:bg-[#f9f1e6]'
                        }`}
                      >
                        <Bot size={20} />
                        vs Bot
                      </button>
                      <button
                        onClick={() => setOfflineMode('local')}
                        className={`touch-target flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 px-4 py-3.5 font-semibold transition-smooth ${
                          offlineMode === 'local'
                            ? 'border-[#6f5035] bg-[#6f5035] text-white'
                            : 'border-[#d2b89a] bg-white text-[#6f5035] hover:bg-[#f9f1e6]'
                        }`}
                      >
                        <UserRound size={20} />
                        vs Friend
                      </button>
                    </div>
                  </div>

                  {offlineMode === 'local' && (
                    <div>
                      <label className="mb-2 block text-sm font-semibold text-[#5c4028]">Opponent Name</label>
                      <input
                        type="text"
                        value={opponentName}
                        onChange={(e) => setOpponentName(e.target.value)}
                        placeholder="Opponent name"
                        className="input-elegant w-full"
                      />
                    </div>
                  )}

                  {offlineMode === 'bot' && (
                    <div>
                      <label className="mb-3 block text-sm font-semibold text-[#5c4028]">Bot Difficulty</label>
                      <div className="grid gap-3 sm:grid-cols-3">
                        {(['easy', 'medium', 'hard'] as BotDifficulty[]).map((difficulty) => (
                          <button
                            key={difficulty}
                            onClick={() => setBotDifficulty(difficulty)}
                            className={`touch-target flex min-h-14 items-center justify-center rounded-2xl border-2 px-4 py-3.5 font-semibold capitalize transition-smooth ${
                              botDifficulty === difficulty
                                ? 'border-[#6f5035] bg-[#6f5035] text-white'
                                : 'border-[#d2b89a] bg-white text-[#6f5035] hover:bg-[#f9f1e6]'
                            }`}
                          >
                            {difficulty}
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                </>
              ) : null}

              {/* Online Mode Options */}
              {mode === 'online' ? (
                <>
                  <div>
                    <label className="mb-3 block text-sm font-semibold text-[#5c4028]">Match Type</label>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <button
                        onClick={() => setOnlineFlow('auto')}
                        className={`touch-target flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 px-4 py-3.5 font-semibold transition-smooth ${
                          onlineFlow === 'auto'
                            ? 'border-[#6f5035] bg-[#6f5035] text-white'
                            : 'border-[#d2b89a] bg-white text-[#6f5035] hover:bg-[#f9f1e6]'
                        }`}
                      >
                        <Users size={20} />
                        Auto Match
                      </button>
                      <button
                        onClick={() => setOnlineFlow('direct')}
                        className={`touch-target flex min-h-14 items-center justify-center gap-2 rounded-2xl border-2 px-4 py-3.5 font-semibold transition-smooth ${
                          onlineFlow === 'direct'
                            ? 'border-[#6f5035] bg-[#6f5035] text-white'
                            : 'border-[#d2b89a] bg-white text-[#6f5035] hover:bg-[#f9f1e6]'
                        }`}
                      >
                        <Sword size={20} />
                        Challenge
                      </button>
                    </div>
                  </div>

                  {onlineFlow === 'direct' && (
                    <div>
                      <label className="mb-2 block text-sm font-semibold text-[#5c4028]">Challenge Username</label>
                      <input
                        type="text"
                        value={targetUsername}
                        onChange={(e) => setTargetUsername(e.target.value)}
                        placeholder="Enter opponent username"
                        className="input-elegant w-full"
                      />
                    </div>
                  )}

                  <div className="space-y-3 rounded-2xl border border-[#d2b89a] bg-[#f9f1e6] p-4">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <p className="text-sm font-semibold text-[#5c4028]">Available Players</p>
                        <p className="text-xs text-[#7b5f46]">See who is online and jump into a direct challenge.</p>
                      </div>
                      <button
                        onClick={() => setPresenceEnabled((value) => !value)}
                        className={`touch-target rounded-xl border px-3 py-2 text-xs font-semibold transition-smooth ${
                          presenceEnabled
                            ? 'border-[#6f5035] bg-[#6f5035] text-white'
                            : 'border-[#d2b89a] bg-white text-[#6f5035]'
                        }`}
                      >
                        {presenceEnabled ? 'Discovery On' : 'Discovery Off'}
                      </button>
                    </div>

                    <input
                      type="text"
                      value={playerSearch}
                      onChange={(e) => setPlayerSearch(e.target.value)}
                      placeholder="Search players"
                      className="input-elegant w-full"
                    />

                    <div className="max-h-60 space-y-2 overflow-auto">
                      {presenceEnabled && discoverablePlayers.length === 0 ? (
                        <p className="rounded-xl bg-white px-3 py-4 text-center text-sm text-[#7b5f46]">No available players right now.</p>
                      ) : null}
                      {!presenceEnabled ? (
                        <p className="rounded-xl bg-white px-3 py-4 text-center text-sm text-[#7b5f46]">Turn discovery on to publish yourself and browse players.</p>
                      ) : null}
                      {discoverablePlayers.map((player) => (
                        <button
                          key={player.user_id}
                          onClick={() => {
                            setOnlineFlow('direct')
                            setTargetUsername(player.user_id)
                          }}
                          className="flex w-full items-center justify-between rounded-2xl border border-[#d9c2a7] bg-white px-4 py-3 text-left transition-smooth hover:border-[#6f5035] hover:bg-[#fdf9f2]"
                        >
                          <div>
                            <p className="text-sm font-semibold text-[#5c4028]">{player.display_name}</p>
                            <p className="text-xs text-[#7b5f46]">@{player.user_id}</p>
                          </div>
                          <span className="rounded-xl bg-[#efe0cc] px-3 py-2 text-xs font-semibold text-[#6f5035]">Challenge</span>
                        </button>
                      ))}
                    </div>
                  </div>
                </>
              ) : null}

              {/* Advanced Settings */}
              <div>
                <button
                  onClick={() => setAdvanced(!advanced)}
                  className="touch-target inline-flex items-center gap-2 rounded-xl border border-[#d2b89a] bg-white px-4 py-2.5 text-sm font-semibold text-[#6f5035] hover:bg-[#f9f1e6] press-scale"
                >
                  <Settings2 size={18} />
                  {advanced ? 'Hide' : 'Show'} Advanced Settings
                </button>
              </div>

              {advanced ? (
                <div className="animate-fade-in space-y-4 rounded-2xl border border-[#d2b89a] bg-[#f9f1e6] p-4">
                  <div>
                    <label className="mb-2 block text-sm font-semibold text-[#5c4028]">
                      Board Size: {boardSize}x{boardSize}
                    </label>
                    <input
                      type="range"
                      min="3"
                      max="6"
                      value={boardSize}
                      onChange={(e) => {
                        const size = Number(e.target.value)
                        setBoardSize(size)
                        if (winLength > size) setWinLength(size)
                      }}
                      className="w-full accent-[#6f5035]"
                    />
                  </div>

                  <div>
                    <label className="mb-2 block text-sm font-semibold text-[#5c4028]">
                      Win Length: {winLength}
                    </label>
                    <input
                      type="range"
                      min="3"
                      max={boardSize}
                      value={winLength}
                      onChange={(e) => setWinLength(Number(e.target.value))}
                      className="w-full accent-[#6f5035]"
                    />
                  </div>
                </div>
              ) : null}

              {/* Start Button */}
              <button
                onClick={startMatch}
                disabled={loading}
                className="btn-primary touch-target mt-4 flex w-full items-center justify-center gap-2 rounded-2xl px-6 py-4 text-lg font-semibold disabled:opacity-50 press-scale"
              >
                {loading ? (
                  <>
                    <span className="animate-spin-slow">
                      <RotateCcw size={20} />
                    </span>
                    Starting...
                  </>
                ) : (
                  <>
                    <Play size={20} />
                    Start Game
                  </>
                )}
              </button>
            </div>
          </section>
        ) : null}

        {/* Play Screen */}
        {screen === 'play' && match ? (
          <section className="card-elegant w-full max-w-3xl p-4 screen-enter sm:p-6">
            {/* Header */}
            <div className="mb-6 grid grid-cols-[auto_1fr_auto] items-center gap-3">
              <button
                onClick={leaveMatch}
                className="touch-target inline-flex items-center gap-2 rounded-xl border border-[#d2b89a] bg-white px-3 py-2 text-sm font-semibold text-[#6f5035] hover:bg-[#f9f1e6] press-scale sm:px-4 sm:py-2.5"
              >
                <Home size={18} />
                <span className="hidden sm:inline">Exit</span>
              </button>

              <div className="text-center">
                <p className="text-sm font-medium text-[#8b6b4a]">{status}</p>
                {match.winner ? (
                  <p className="mt-1 text-sm font-bold text-[#6f5035]">
                    {match.winner === 'draw' ? 'Draw' : `${match.winner} wins`}
                  </p>
                ) : null}
              </div>

              <button
                onClick={resetGame}
                className="touch-target inline-flex items-center gap-2 rounded-xl border border-[#d2b89a] bg-white px-3 py-2 text-sm font-semibold text-[#6f5035] hover:bg-[#f9f1e6] press-scale sm:px-4 sm:py-2.5"
              >
                <RotateCcw size={18} />
                <span className="hidden sm:inline">Restart</span>
              </button>
            </div>

            {/* Player Info */}
            <div className="mx-auto mb-6 grid max-w-xl grid-cols-3 items-center rounded-2xl bg-gradient-to-r from-[#f2e3ce] to-[#e8d5b5] px-4 py-3 text-center">
              <div className="flex items-center gap-2">
                <div className="flex h-10 w-10 items-center justify-center rounded-full bg-[#6f5035] text-white font-bold">
                  X
                </div>
                <div>
                  <p className="text-xs font-medium text-[#77563b]">Player X</p>
                  <p className="text-sm font-semibold text-[#5c4028] truncate max-w-[100px] sm:max-w-[150px]">
                    {match.player_x === 'bot' ? 'Bot' : match.player_x}
                  </p>
                </div>
              </div>

              <div className="text-center">
                <p className="text-xs text-[#8b6b4a]">Turn</p>
                <p className="text-2xl font-bold text-[#6f5035]">{match.current}</p>
              </div>

              <div className="flex items-center justify-end gap-2 text-right">
                <div>
                  <p className="text-xs font-medium text-[#77563b]">Player O</p>
                  <p className="text-sm font-semibold text-[#5c4028] truncate max-w-[100px] sm:max-w-[150px]">
                    {match.player_o === 'bot' ? 'Bot' : match.player_o}
                  </p>
                </div>
                <div className="flex h-10 w-10 items-center justify-center rounded-full bg-[#8b6b4a] text-white font-bold">
                  O
                </div>
              </div>
            </div>

            {/* Game Board */}
            <div className="no-select mx-auto mb-4 flex justify-center touch-none">
              <div
                className="grid gap-2 sm:gap-3"
                style={{
                  gridTemplateColumns: `repeat(${match.board_size}, minmax(0, 1fr))`,
                  width: '100%',
                  maxWidth: match.board_size <= 4 ? '460px' : '560px',
                }}
              >
                {match.board.flatMap((row, r) =>
                  row.map((cell, c) => {
                    const isWinning = winningLine.some(([wr, wc]) => wr === r && wc === c)
                    const isLast = lastMove && lastMove[0] === r && lastMove[1] === c
                    const disabled = isCellDisabled(r, c)

                    return (
                      <button
                        key={`${r}-${c}`}
                        onClick={() => playCell(r, c)}
                        disabled={disabled}
                        className={`aspect-square touch-target flex items-center justify-center rounded-xl sm:rounded-2xl border-2 font-bold transition-smooth press-scale
                          ${isWinning 
                            ? 'winning-cell border-[#ffd700] text-3xl sm:text-5xl' 
                            : isLast
                            ? 'border-[#6f5035] bg-[#f0e4d4] text-3xl sm:text-5xl'
                            : 'border-[#d2b89a] bg-[#f8f0e5] text-3xl sm:text-5xl hover:bg-[#f4e9db]'
                          }
                          ${!disabled && !cell ? 'cursor-pointer hover:shadow-md' : ''}
                          ${disabled && !cell ? 'cursor-default opacity-60' : ''}
                        `}
                      >
                        {cell === 'X' && (
                          <span className="cell-x animate-pop-in text-[#6f5035]">X</span>
                        )}
                        {cell === 'O' && (
                          <span className="cell-o animate-pop-in text-[#8b6b4a]">O</span>
                        )}
                      </button>
                    )
                  }),
                )}
              </div>
            </div>

            {/* Mobile action buttons */}
            <div className="flex gap-3 sm:hidden">
              <button
                onClick={resetGame}
                className="btn-secondary touch-target flex-1 rounded-xl px-4 py-3 text-sm font-semibold press-scale"
              >
                <RotateCcw size={16} className="inline" /> Restart
              </button>
              <button
                onClick={() => setChatOpen(!chatOpen)}
                className="btn-primary touch-target flex-1 rounded-xl px-4 py-3 text-sm font-semibold press-scale"
              >
                <MessageCircle size={16} className="inline" /> {chatOpen ? 'Close' : 'Chat'}
              </button>
            </div>
          </section>
        ) : null}
      </div>

      {/* Chat Panel */}
      {screen === 'play' && mode === 'online' && chatOpen ? (
        <div className="fixed bottom-4 right-4 z-20 w-[calc(100vw-2rem)] max-w-sm animate-fade-in sm:right-6">
          <div className="card-elegant p-3 shadow-xl">
            <div className="mb-2 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-[#6f5035]">Match Chat</h3>
              <button
                onClick={() => setChatOpen(false)}
                className="touch-target rounded-lg p-1 text-[#6f5035] hover:bg-[#f0e4d4] press-scale"
              >
                <X size={16} />
              </button>
            </div>

            <div className="mb-2 max-h-48 space-y-1.5 overflow-auto rounded-xl border border-[#d2b89a] bg-[#f9f1e6] p-2">
              {chat.length === 0 ? (
                <p className="text-center text-sm text-[#77563b]">No messages yet</p>
              ) : (
                chat
                  .slice()
                  .reverse()
                  .map((msg) => (
                    <div key={msg.id} className="animate-fade-in rounded-lg bg-[#efe0cc] px-2.5 py-1.5">
                      <p className="text-xs font-semibold text-[#5c4028]">{msg.user_id}</p>
                      <p className="text-sm">{msg.message || ''}</p>
                    </div>
                  ))
              )}
            </div>

            <div className="flex gap-2">
              <input
                value={chatInput}
                onChange={(e) => setChatInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && sendChat()}
                placeholder="Type message..."
                className="input-elegant flex-1 text-sm"
              />
              <button
                onClick={() => sendChat()}
                className="btn-primary touch-target inline-flex items-center gap-1.5 rounded-xl px-3 py-2 text-sm press-scale"
              >
                <SendHorizontal size={16} />
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}

// Confetti Component
function findWinningLine(board: string[][], winLength: number, winner: string): [number, number][] {
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
      if (symbol !== winner) continue
      
      for (const [dr, dc] of dirs) {
        let matches = true
        const line: [number, number][] = [[r, c]]
        
        for (let k = 1; k < winLength; k++) {
          const nr = r + dr * k
          const nc = c + dc * k
          if (nr < 0 || nr >= n || nc < 0 || nc >= n || board[nr][nc] !== symbol) {
            matches = false
            break
          }
          line.push([nr, nc])
        }
        
        if (matches) return line
      }
    }
  }
  
  return []
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

function resolveGatewayHTTPBase(envURL?: string) {
  const explicit = normalizeBase(envURL)
  if (explicit) return explicit

  if (typeof window !== 'undefined' && window.location.port === '3000') {
    return '/api'
  }
  return 'http://localhost:8080'
}

function resolveGatewayWSBase(httpBase: string) {
  if (httpBase.startsWith('http://') || httpBase.startsWith('https://')) {
    return httpBase.replace(/^http/, 'ws')
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}${httpBase}`
}

function normalizeBase(v?: string) {
  if (!v) return ''
  const trimmed = v.trim()
  if (!trimmed) return ''
  return trimmed.endsWith('/') ? trimmed.slice(0, -1) : trimmed
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

function findBotMove(board: string[][], winLength: number, difficulty: BotDifficulty) {
  const moves = listOpenCells(board)
  if (moves.length === 0) return null

  if (difficulty === 'easy') {
    return randomMove(moves)
  }

  const winningMove = findImmediateMove(board, winLength, 'O')
  if (winningMove) return winningMove

  if (difficulty === 'medium') {
    const blockingMove = findImmediateMove(board, winLength, 'X')
    if (blockingMove) return blockingMove
    return preferredMove(board, moves)
  }

  const blockingMove = findImmediateMove(board, winLength, 'X')
  if (blockingMove) return blockingMove

  if (board.length === 3 && winLength === 3) {
    return minimaxBestMove(board)
  }

  return strongestScoredMove(board, winLength, moves)
}

function listOpenCells(board: string[][]) {
  const moves: Array<{ row: number; col: number }> = []
  for (let r = 0; r < board.length; r++) {
    for (let c = 0; c < board.length; c++) {
      if (!board[r][c]) moves.push({ row: r, col: c })
    }
  }
  return moves
}

function randomMove(moves: Array<{ row: number; col: number }>) {
  return moves[Math.floor(Math.random() * moves.length)] ?? null
}

function findImmediateMove(board: string[][], winLength: number, symbol: 'X' | 'O') {
  const moves = listOpenCells(board)
  for (const move of moves) {
    const clone = cloneBoard(board)
    clone[move.row][move.col] = symbol
    if (checkWinner(clone, winLength) === symbol) {
      return move
    }
  }
  return null
}

function preferredMove(board: string[][], moves: Array<{ row: number; col: number }>) {
  const center = Math.floor(board.length / 2)
  if (!board[center][center]) return { row: center, col: center }

  const corners = moves.filter((move) => {
    const last = board.length - 1
    return (
      (move.row === 0 && move.col === 0) ||
      (move.row === 0 && move.col === last) ||
      (move.row === last && move.col === 0) ||
      (move.row === last && move.col === last)
    )
  })
  if (corners.length > 0) return randomMove(corners)
  return randomMove(moves)
}

function strongestScoredMove(board: string[][], winLength: number, moves: Array<{ row: number; col: number }>) {
  let bestMove = moves[0]
  let bestScore = Number.NEGATIVE_INFINITY

  for (const move of moves) {
    const attackScore = evaluateMove(board, winLength, move, 'O')
    const defenseScore = evaluateMove(board, winLength, move, 'X')
    const centerBias = centerWeight(board.length, move.row, move.col)
    const score = attackScore * 2 + defenseScore * 1.5 + centerBias
    if (score > bestScore) {
      bestScore = score
      bestMove = move
    }
  }

  return bestMove
}

function evaluateMove(board: string[][], winLength: number, move: { row: number; col: number }, symbol: 'X' | 'O') {
  const dirs = [
    [0, 1],
    [1, 0],
    [1, 1],
    [1, -1],
  ]
  let best = 0

  for (const [dr, dc] of dirs) {
    let count = 1

    for (const direction of [-1, 1]) {
      for (let step = 1; step < winLength; step++) {
        const nr = move.row + dr * step * direction
        const nc = move.col + dc * step * direction
        if (nr < 0 || nr >= board.length || nc < 0 || nc >= board.length) break
        if (board[nr][nc] !== symbol) break
        count++
      }
    }

    best = Math.max(best, count)
  }

  return best
}

function centerWeight(size: number, row: number, col: number) {
  const center = (size - 1) / 2
  return size - (Math.abs(center - row) + Math.abs(center - col))
}

function minimaxBestMove(board: string[][]) {
  let bestScore = Number.NEGATIVE_INFINITY
  let bestMove: { row: number; col: number } | null = null

  for (const move of listOpenCells(board)) {
    const clone = cloneBoard(board)
    clone[move.row][move.col] = 'O'
    const score = minimax(clone, false)
    if (score > bestScore) {
      bestScore = score
      bestMove = move
    }
  }

  return bestMove
}

function minimax(board: string[][], maximizing: boolean): number {
  const winner = checkWinner(board, 3)
  if (winner === 'O') return 10
  if (winner === 'X') return -10
  if (isBoardFull(board)) return 0

  if (maximizing) {
    let best = Number.NEGATIVE_INFINITY
    for (const move of listOpenCells(board)) {
      const clone = cloneBoard(board)
      clone[move.row][move.col] = 'O'
      best = Math.max(best, minimax(clone, false))
    }
    return best
  }

  let best = Number.POSITIVE_INFINITY
  for (const move of listOpenCells(board)) {
    const clone = cloneBoard(board)
    clone[move.row][move.col] = 'X'
    best = Math.min(best, minimax(clone, true))
  }
  return best
}

function cloneBoard(board: string[][]) {
  return board.map((row) => [...row])
}

function labelBotDifficulty(difficulty: BotDifficulty) {
  if (difficulty === 'easy') return 'easy'
  if (difficulty === 'hard') return 'hard'
  return 'medium'
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

function getDefaultPlayerName() {
  const key = 'hubgame.player_name'
  const existing = sessionStorage.getItem(key)
  if (existing && existing.trim()) {
    return existing.trim()
  }
  const next = `player-${Math.random().toString(36).slice(2, 6)}`
  sessionStorage.setItem(key, next)
  return next
}
