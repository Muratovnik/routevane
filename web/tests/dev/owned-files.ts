import { open, rm } from 'node:fs/promises'

/** Only a successful exclusive creation transfers a probe to this session. */
export const ownedFiles = () => {
  const paths: string[] = []
  return {
    async create(path: string, contents: string): Promise<void> {
      const file = await open(path, 'wx')
      paths.push(path)
      try {
        await file.writeFile(contents)
      } finally {
        await file.close()
      }
    },
    async cleanup(): Promise<void> {
      for (const path of paths) await rm(path, { force: true })
    },
  }
}
