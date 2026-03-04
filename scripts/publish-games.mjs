#!/usr/bin/env bun
import { existsSync } from 'node:fs'
import { mkdir, readdir, readFile, stat, writeFile } from 'node:fs/promises'
import { resolve, join, relative } from 'node:path'

const root = process.cwd()
const gamesDir = resolve(root, 'games')
const outDir = resolve(root, 'games/.published')
const outFile = resolve(outDir, 'index.json')

const requiredStringFields = ['id', 'name', 'version', 'author', 'description', 'entry']

async function main() {
  await mkdir(outDir, { recursive: true })

  const dirs = await readdir(gamesDir)
  const published = []
  const errors = []

  for (const dir of dirs) {
    if (dir.startsWith('.') || dir === 'TODO' || dir === 'PROGRESS') continue
    const gamePath = join(gamesDir, dir)
    const info = await stat(gamePath)
    if (!info.isDirectory()) continue

    const manifestPath = join(gamePath, 'manifest.json')
    if (!existsSync(manifestPath)) {
      errors.push(`[${dir}] missing manifest.json`)
      continue
    }

    let manifest
    try {
      const raw = await readFile(manifestPath, 'utf8')
      manifest = JSON.parse(raw)
    } catch (err) {
      errors.push(`[${dir}] invalid manifest JSON: ${String(err.message || err)}`)
      continue
    }

    const manifestErrors = validateManifest(manifest, dir)
    if (manifestErrors.length > 0) {
      errors.push(...manifestErrors)
      continue
    }

    const entryPath = join(gamePath, manifest.entry)
    if (!existsSync(entryPath)) {
      errors.push(`[${dir}] entry does not exist: ${manifest.entry}`)
      continue
    }

    if (manifest.cover && !existsSync(join(gamePath, manifest.cover))) {
      errors.push(`[${dir}] cover not found: ${manifest.cover}`)
      continue
    }

    if (Array.isArray(manifest.screenshots)) {
      for (const shot of manifest.screenshots) {
        if (!existsSync(join(gamePath, shot))) {
          errors.push(`[${dir}] screenshot not found: ${shot}`)
        }
      }
    }

    published.push({
      id: manifest.id,
      name: manifest.name,
      version: manifest.version,
      author: manifest.author,
      inhouse: Boolean(manifest.inhouse),
      description: manifest.description,
      categories: manifest.categories || [],
      modes: manifest.modes || [],
      tags: manifest.tags || [],
      cover: manifest.cover || null,
      entry: relative(gamesDir, join(gamePath, manifest.entry)).replaceAll('\\', '/'),
      manifest: relative(root, manifestPath).replaceAll('\\', '/'),
      publishedAt: new Date().toISOString(),
    })
  }

  if (errors.length > 0) {
    console.error('Publish validation failed:\n')
    errors.forEach((err) => console.error(`- ${err}`))
    process.exit(1)
  }

  published.sort((a, b) => a.id.localeCompare(b.id))

  const output = {
    generatedAt: new Date().toISOString(),
    count: published.length,
    games: published,
  }

  await writeFile(outFile, JSON.stringify(output, null, 2) + '\n', 'utf8')
  console.log(`Published ${published.length} game(s) to ${relative(root, outFile)}`)
}

function validateManifest(manifest, dir) {
  const errors = []

  for (const field of requiredStringFields) {
    if (typeof manifest[field] !== 'string' || manifest[field].trim() === '') {
      errors.push(`[${dir}] missing or invalid field: ${field}`)
    }
  }

  if (manifest.id && manifest.id !== dir) {
    errors.push(`[${dir}] manifest id must match directory name (${dir})`)
  }

  if (!Array.isArray(manifest.categories) || manifest.categories.length === 0) {
    errors.push(`[${dir}] categories must be a non-empty array`)
  }

  if (!Array.isArray(manifest.modes) || manifest.modes.length === 0) {
    errors.push(`[${dir}] modes must be a non-empty array`)
  }

  if (!manifest.supports || typeof manifest.supports !== 'object') {
    errors.push(`[${dir}] supports object is required`)
  }

  if (manifest.inhouse === true && manifest.author !== 'hubgame') {
    errors.push(`[${dir}] in-house games must use author \"hubgame\"`)
  }

  return errors
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
