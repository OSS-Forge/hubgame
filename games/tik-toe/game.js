(() => {
  const state = {
    mode: 'bot',
    boardSize: 3,
    winLength: 3,
    botDifficulty: 'medium',
    turnTimer: 15,
    board: [],
    current: 'X',
    symbols: { X: 'X', O: 'O' },
    moves: [],
    locked: false,
    winner: null,
    activeModifier: null,
    modifiers: {
      X: { double: true, freeze: true, swap: true, undo: true },
      O: { double: true, freeze: true, swap: true, undo: true },
    },
    freezeMap: new Map(),
    extraMove: null,
    timerLeft: null,
    timerHandle: null,
    turnIndex: 1,
  }

  const els = {
    mode: document.getElementById('mode'),
    boardSize: document.getElementById('boardSize'),
    winLength: document.getElementById('winLength'),
    botDifficulty: document.getElementById('botDifficulty'),
    turnTimer: document.getElementById('turnTimer'),
    startBtn: document.getElementById('startBtn'),
    restartBtn: document.getElementById('restartBtn'),
    status: document.getElementById('status'),
    turnMeta: document.getElementById('turnMeta'),
    timerValue: document.getElementById('timerValue'),
    moveCount: document.getElementById('moveCount'),
    board: document.getElementById('board'),
    eventLog: document.getElementById('eventLog'),
    modsX: document.getElementById('modsX'),
    modsO: document.getElementById('modsO'),
  }

  const MODS = [
    { id: 'double', label: 'Double Move' },
    { id: 'freeze', label: 'Freeze Cell' },
    { id: 'swap', label: 'Swap Symbols' },
    { id: 'undo', label: 'Undo' },
  ]

  function init() {
    bindEvents()
    syncWinLengthOptions()
    renderModifierPanels()
    renderBoard()
    renderStatus('Press Start Match.')
  }

  function bindEvents() {
    els.mode.addEventListener('change', () => {
      state.mode = els.mode.value
      updateMeta()
    })

    els.boardSize.addEventListener('change', () => {
      state.boardSize = Number(els.boardSize.value)
      syncWinLengthOptions()
      renderBoard()
    })

    els.winLength.addEventListener('change', () => {
      state.winLength = Number(els.winLength.value)
      updateMeta()
    })

    els.botDifficulty.addEventListener('change', () => {
      state.botDifficulty = els.botDifficulty.value
    })

    els.turnTimer.addEventListener('change', () => {
      state.turnTimer = Number(els.turnTimer.value)
      if (state.turnTimer > 0 && state.moves.length > 0 && !state.winner) {
        resetTurnTimer()
      }
    })

    els.startBtn.addEventListener('click', startMatch)
    els.restartBtn.addEventListener('click', restartMatch)
  }

  function syncWinLengthOptions() {
    const size = Number(els.boardSize.value)
    const values = [3, 4, 5].filter((v) => v <= size)
    els.winLength.innerHTML = values.map((v) => `<option value="${v}">${v}</option>`).join('')
    if (!values.includes(state.winLength)) {
      state.winLength = values[values.length - 1]
    }
    els.winLength.value = String(state.winLength)
  }

  function resetStatePreserveSetup() {
    state.board = Array.from({ length: state.boardSize }, () => Array(state.boardSize).fill(''))
    state.current = 'X'
    state.symbols = { X: 'X', O: 'O' }
    state.moves = []
    state.locked = false
    state.winner = null
    state.activeModifier = null
    state.modifiers = {
      X: { double: true, freeze: true, swap: true, undo: true },
      O: { double: true, freeze: true, swap: true, undo: true },
    }
    state.freezeMap = new Map()
    state.extraMove = null
    state.turnIndex = 1
    clearTurnTimer()
    state.timerLeft = state.turnTimer > 0 ? state.turnTimer : null
  }

  function startMatch() {
    state.mode = els.mode.value
    state.boardSize = Number(els.boardSize.value)
    state.winLength = Number(els.winLength.value)
    state.botDifficulty = els.botDifficulty.value
    state.turnTimer = Number(els.turnTimer.value)

    resetStatePreserveSetup()
    renderBoard()
    renderModifierPanels()
    logEvent('Match started')
    renderStatus(`Player ${state.symbols[state.current]} turn`)
    updateMeta()
    resetTurnTimer()
  }

  function restartMatch() {
    if (state.board.length === 0) {
      startMatch()
      return
    }
    resetStatePreserveSetup()
    renderBoard()
    renderModifierPanels()
    logEvent('Match restarted')
    renderStatus(`Player ${state.symbols[state.current]} turn`)
    updateMeta()
    resetTurnTimer()
  }

  function renderBoard(winCells = []) {
    els.board.innerHTML = ''
    const grid = document.createElement('div')
    grid.className = 'grid'
    grid.style.gridTemplateColumns = `repeat(${state.boardSize}, minmax(0, 1fr))`

    for (let r = 0; r < state.boardSize; r++) {
      for (let c = 0; c < state.boardSize; c++) {
        const btn = document.createElement('button')
        btn.className = 'cell'
        const key = `${r},${c}`
        const cellValue = state.board[r]?.[c] || ''
        btn.textContent = cellValue
        btn.dataset.r = String(r)
        btn.dataset.c = String(c)

        if (state.freezeMap.has(key)) {
          btn.classList.add('frozen')
        }
        if (winCells.some(([wr, wc]) => wr === r && wc === c)) {
          btn.classList.add('win')
        }

        btn.addEventListener('click', onCellClick)
        grid.appendChild(btn)
      }
    }

    els.board.appendChild(grid)
    els.moveCount.textContent = String(state.moves.length)
  }

  function onCellClick(event) {
    if (state.locked || state.winner || state.board.length === 0) return

    const btn = event.currentTarget
    const r = Number(btn.dataset.r)
    const c = Number(btn.dataset.c)
    const key = `${r},${c}`

    if (state.board[r][c]) return
    if (state.freezeMap.has(key)) return

    if (state.activeModifier === 'freeze') {
      if (!state.modifiers[state.current].freeze) return
      state.freezeMap.set(key, 2)
      state.modifiers[state.current].freeze = false
      state.activeModifier = null
      logEvent(`${state.current} froze cell (${r + 1},${c + 1})`) 
      nextTurn()
      return
    }

    makeMove(r, c)
  }

  function makeMove(r, c) {
    if (state.locked || state.winner) return
    state.board[r][c] = state.symbols[state.current]
    state.moves.push({ r, c, player: state.current, symbol: state.symbols[state.current] })

    const winner = detectWinner(state.board, state.winLength)
    if (winner) {
      state.winner = winner.symbol
      renderBoard(winner.cells)
      renderStatus(`Winner: ${winner.symbol}`)
      logEvent(`Winner is ${winner.symbol}`)
      clearTurnTimer()
      return
    }

    if (isBoardFull(state.board)) {
      state.winner = 'draw'
      renderBoard()
      renderStatus('Draw match')
      logEvent('Draw reached')
      clearTurnTimer()
      return
    }

    if (state.activeModifier === 'double' && state.modifiers[state.current].double && state.extraMove !== state.current) {
      state.modifiers[state.current].double = false
      state.extraMove = state.current
      state.activeModifier = null
      renderBoard()
      renderModifierPanels()
      renderStatus(`${state.current} earned an extra move`) 
      logEvent(`${state.current} used Double Move`) 
      resetTurnTimer()
      if (state.mode === 'bot' && state.current === 'O') {
        botPlay()
      }
      return
    }

    state.extraMove = null
    nextTurn()
  }

  function nextTurn() {
    state.current = state.current === 'X' ? 'O' : 'X'
    state.turnIndex += 1
    reduceFreezeCounters()
    state.activeModifier = null
    renderBoard()
    renderModifierPanels()
    renderStatus(`Player ${state.symbols[state.current]} turn`)
    updateMeta()
    resetTurnTimer()

    if (state.mode === 'bot' && state.current === 'O') {
      botPlay()
    }
  }

  function reduceFreezeCounters() {
    for (const [key, turns] of state.freezeMap.entries()) {
      if (turns <= 1) {
        state.freezeMap.delete(key)
      } else {
        state.freezeMap.set(key, turns - 1)
      }
    }
  }

  function renderModifierPanels() {
    renderModsFor('X', els.modsX)
    renderModsFor('O', els.modsO)
  }

  function renderModsFor(player, container) {
    container.innerHTML = ''
    MODS.forEach((mod) => {
      const btn = document.createElement('button')
      const available = state.modifiers[player][mod.id]
      btn.className = 'mod-btn'
      if (!available) btn.classList.add('used')
      if (state.current === player && state.activeModifier === mod.id) btn.classList.add('active')
      btn.textContent = mod.label

      btn.addEventListener('click', () => activateModifier(player, mod.id))
      container.appendChild(btn)
    })
  }

  function activateModifier(player, modId) {
    if (state.winner || state.locked || player !== state.current) return
    if (!state.modifiers[player][modId]) return

    if (modId === 'swap') {
      state.modifiers[player].swap = false
      const oldX = state.symbols.X
      state.symbols.X = state.symbols.O
      state.symbols.O = oldX
      logEvent(`${player} swapped symbols`) 
      renderStatus(`Symbols swapped. ${state.current} turn`)
      renderModifierPanels()
      renderBoard()
      return
    }

    if (modId === 'undo') {
      if (state.moves.length === 0) return
      const last = state.moves.pop()
      state.board[last.r][last.c] = ''
      state.modifiers[player].undo = false
      state.current = last.player
      state.winner = null
      state.activeModifier = null
      logEvent(`${player} undid last move`) 
      renderBoard()
      renderModifierPanels()
      renderStatus(`Undo used. ${state.current} turn`) 
      resetTurnTimer()
      return
    }

    state.activeModifier = state.activeModifier === modId ? null : modId
    renderModifierPanels()
    renderStatus(
      state.activeModifier
        ? `${state.current} selected ${MODS.find((m) => m.id === modId).label}`
        : `Player ${state.symbols[state.current]} turn`,
    )
  }

  function botPlay() {
    state.locked = true
    setTimeout(() => {
      if (state.winner || state.current !== 'O') {
        state.locked = false
        return
      }

      const move = chooseBotMove()
      if (!move) {
        state.locked = false
        return
      }
      makeMove(move.r, move.c)
      state.locked = false
    }, 420)
  }

  function chooseBotMove() {
    const open = getOpenCells(state.board, state.freezeMap)
    if (open.length === 0) return null

    if (state.botDifficulty === 'easy') {
      return open[Math.floor(Math.random() * open.length)]
    }

    const winMove = findImmediateWin('O')
    if (winMove) return winMove

    const blockMove = findImmediateWin('X')
    if (blockMove) return blockMove

    if (state.botDifficulty === 'hard' && state.boardSize === 3 && state.winLength === 3) {
      const best = minimaxBestMove(state.board)
      if (best) return best
    }

    return pickHeuristic(open)
  }

  function pickHeuristic(open) {
    const center = Math.floor(state.boardSize / 2)
    const centerMove = open.find((m) => m.r === center && m.c === center)
    if (centerMove) return centerMove

    const corners = open.filter((m) =>
      (m.r === 0 || m.r === state.boardSize - 1) && (m.c === 0 || m.c === state.boardSize - 1),
    )
    if (corners.length > 0) {
      return corners[Math.floor(Math.random() * corners.length)]
    }
    return open[Math.floor(Math.random() * open.length)]
  }

  function findImmediateWin(player) {
    const symbol = state.symbols[player]
    const open = getOpenCells(state.board, state.freezeMap)
    for (const cell of open) {
      state.board[cell.r][cell.c] = symbol
      const winner = detectWinner(state.board, state.winLength)
      state.board[cell.r][cell.c] = ''
      if (winner && winner.symbol === symbol) return cell
    }
    return null
  }

  function minimaxBestMove(board) {
    let bestScore = -Infinity
    let bestMove = null
    getOpenCells(board, state.freezeMap).forEach((move) => {
      board[move.r][move.c] = state.symbols.O
      const score = minimax(board, false, 0)
      board[move.r][move.c] = ''
      if (score > bestScore) {
        bestScore = score
        bestMove = move
      }
    })
    return bestMove
  }

  function minimax(board, maximizing, depth) {
    const win = detectWinner(board, 3)
    if (win) {
      if (win.symbol === state.symbols.O) return 10 - depth
      if (win.symbol === state.symbols.X) return depth - 10
    }
    if (isBoardFull(board)) return 0

    if (maximizing) {
      let best = -Infinity
      getOpenCells(board, state.freezeMap).forEach((move) => {
        board[move.r][move.c] = state.symbols.O
        best = Math.max(best, minimax(board, false, depth + 1))
        board[move.r][move.c] = ''
      })
      return best
    }

    let best = Infinity
    getOpenCells(board, state.freezeMap).forEach((move) => {
      board[move.r][move.c] = state.symbols.X
      best = Math.min(best, minimax(board, true, depth + 1))
      board[move.r][move.c] = ''
    })
    return best
  }

  function resetTurnTimer() {
    clearTurnTimer()
    if (state.turnTimer <= 0 || state.winner) {
      els.timerValue.textContent = '--'
      return
    }
    state.timerLeft = state.turnTimer
    els.timerValue.textContent = String(state.timerLeft)
    state.timerHandle = setInterval(() => {
      state.timerLeft -= 1
      els.timerValue.textContent = String(Math.max(0, state.timerLeft))
      if (state.timerLeft <= 0) {
        clearTurnTimer()
        onTurnTimeout()
      }
    }, 1000)
  }

  function clearTurnTimer() {
    if (state.timerHandle) {
      clearInterval(state.timerHandle)
      state.timerHandle = null
    }
  }

  function onTurnTimeout() {
    if (state.winner) return
    const open = getOpenCells(state.board, state.freezeMap)
    if (open.length === 0) return
    const forced = open[Math.floor(Math.random() * open.length)]
    logEvent(`${state.current} timed out; auto-move at (${forced.r + 1},${forced.c + 1})`) 
    makeMove(forced.r, forced.c)
  }

  function updateMeta() {
    const modeLabel = state.mode === 'bot' ? `Bot (${state.botDifficulty})` : 'Local'
    els.turnMeta.textContent = `${state.boardSize}x${state.boardSize} • Win ${state.winLength} • ${modeLabel}`
  }

  function renderStatus(text) {
    els.status.textContent = text
  }

  function logEvent(text) {
    const li = document.createElement('li')
    li.textContent = `${new Date().toLocaleTimeString()} — ${text}`
    els.eventLog.prepend(li)
    while (els.eventLog.children.length > 50) {
      els.eventLog.removeChild(els.eventLog.lastChild)
    }
  }

  function getOpenCells(board, freezeMap) {
    const out = []
    for (let r = 0; r < board.length; r++) {
      for (let c = 0; c < board.length; c++) {
        if (!board[r][c] && !freezeMap.has(`${r},${c}`)) {
          out.push({ r, c })
        }
      }
    }
    return out
  }

  function detectWinner(board, winLength) {
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
          const cells = [[r, c]]
          for (let k = 1; k < winLength; k++) {
            const nr = r + dr * k
            const nc = c + dc * k
            if (nr < 0 || nr >= n || nc < 0 || nc >= n) break
            if (board[nr][nc] !== symbol) break
            cells.push([nr, nc])
          }
          if (cells.length === winLength) {
            return { symbol, cells }
          }
        }
      }
    }
    return null
  }

  function isBoardFull(board) {
    for (let r = 0; r < board.length; r++) {
      for (let c = 0; c < board.length; c++) {
        if (!board[r][c]) return false
      }
    }
    return true
  }

  init()
})()
