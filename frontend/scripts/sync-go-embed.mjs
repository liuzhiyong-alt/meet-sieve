import { access, cp, rm, writeFile } from 'node:fs/promises'
import { constants } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const frontendDirectory = resolve(scriptDirectory, '..')
const sourceDirectory = resolve(frontendDirectory, 'dist')
const targetDirectory = resolve(frontendDirectory, '..', 'cmd', 'meetsieve', 'frontend', 'dist')

/** 同步已构建的前端资源到 Go embed 目录，避免桌面二进制携带过期访客页。 */
async function syncGoEmbedAssets() {
  await access(sourceDirectory, constants.R_OK)
  await rm(targetDirectory, { recursive: true, force: true })
  await cp(sourceDirectory, targetDirectory, { recursive: true })
  await writeFile(resolve(targetDirectory, '.gitkeep'), '')
}

await syncGoEmbedAssets()
