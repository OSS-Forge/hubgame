#!/usr/bin/env bun
import { existsSync } from 'node:fs'
import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, resolve, join } from 'node:path'

const root = process.cwd()
const publishedIndex = resolve(root, 'games/.published/index.json')
const webPublicDir = resolve(root, 'web/public')
const webGamesDir = resolve(webPublicDir, 'games')
const fallbackFile = resolve(webPublicDir, 'fallback-catalog.json')

async function main() {
  if (!existsSync(publishedIndex)) {
    throw new Error('Missing games/.published/index.json. Run bun scripts/publish-games.mjs first.')
  }

  const raw = await readFile(publishedIndex, 'utf8')
  const index = JSON.parse(raw)
  const items = Array.isArray(index.games) ? index.games : []

  await rm(webGamesDir, { recursive: true, force: true })
  await mkdir(webGamesDir, { recursive: true })

  const fallback = []
  for (const item of items) {
    const gameId = item.id
    const manifestPath = resolve(root, item.manifest)
    const manifestRaw = await readFile(manifestPath, 'utf8')
    const manifest = JSON.parse(manifestRaw)

    const sourceGameDir = dirname(manifestPath)
    const targetGameDir = join(webGamesDir, gameId)
    await mkdir(targetGameDir, { recursive: true })

    if (!manifest.entry.startsWith('dist/')) {
      throw new Error(`[${gameId}] manifest.entry must point to dist/* for web fallback sync`)
    }
    const distDir = join(sourceGameDir, 'dist')
    if (!existsSync(distDir)) {
      throw new Error(`[${gameId}] missing dist folder. Run game build before syncing.`)
    }
    await cp(distDir, join(targetGameDir, 'dist'), { recursive: true })

    const manifestTarget = join(targetGameDir, 'manifest.json')
    await cp(manifestPath, manifestTarget)
    await writeGameRouteIndex(targetGameDir, manifest.entry)

    const readmePath = join(sourceGameDir, 'README.md')
    if (existsSync(readmePath)) {
      await cp(readmePath, join(targetGameDir, 'README.md'))
    }
    if (manifest.cover || (Array.isArray(manifest.screenshots) && manifest.screenshots.length > 0)) {
      const assetsDir = join(sourceGameDir, 'assets')
      if (existsSync(assetsDir)) {
        await cp(assetsDir, join(targetGameDir, 'assets'), { recursive: true })
      }
    }

    fallback.push({
      id: manifest.id,
      name: manifest.name,
      studio: manifest.author,
      category: manifest.categories?.[0] || 'Strategy',
      modes: manifest.modes || ['Solo'],
      vibe: manifest.description || 'Play now on HubGame.',
      cover: manifest.cover ? `/games/${gameId}/${manifest.cover}` : '/vite.svg',
      rating: 4.5,
      players: 'offline ready',
      installed: false,
      tags: manifest.tags || [],
    })
  }

  await writeFile(
    fallbackFile,
    JSON.stringify(
      {
        generatedAt: new Date().toISOString(),
        count: fallback.length,
        source: 'games/.published/index.json',
        games: fallback,
      },
      null,
      2,
    ) + '\n',
    'utf8',
  )

  console.log(`Synced ${fallback.length} game(s) to web/public/games and web/public/fallback-catalog.json`)
}

async function writeGameRouteIndex(targetGameDir, entry) {
  const safeEntry = entry
    .split('/')
    .map((part) => encodeURIComponent(part))
    .join('/')
  await writeFile(
    join(targetGameDir, 'index.html'),
    `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <meta http-equiv="refresh" content="0; url=${safeEntry}" />
    <title>Opening game</title>
    <script>window.location.replace(${JSON.stringify(safeEntry)})</script>
  </head>
  <body>
    <a href="${safeEntry}">Open game</a>
  </body>
</html>
`,
    'utf8',
  )
}

main().catch((err) => {
  console.error(err.message || err)
  process.exit(1)
})
