import path from 'node:path'
import { createRequire } from 'node:module'

// Resolve through the actual installed Next/React lint parent. These are
// toolchain dependencies; the application has no runtime Zod schema consumer.
const nextConfig = createRequire(path.join(process.cwd(), 'node_modules/eslint-config-next/package.json'))
const parent = createRequire(nextConfig.resolve('eslint-plugin-react-hooks'))
const { z } = parent('zod')
const { fromZodError } = parent('zod-validation-error')

it('loads the real dual-compatible parent and error bridge with the same patched Zod4', () => {
  const version = parent('zod/package.json').version.split('.').map(Number)
  expect(version[0]).toBe(4)
  expect(version[1] > 6 || (version[1] === 6 && version[2] >= 6)).toBe(true)
  const bridge = createRequire(parent.resolve('zod-validation-error'))
  expect(bridge.resolve('zod')).toBe(parent.resolve('zod'))
  expect(typeof nextConfig('eslint-plugin-react-hooks').rules['rules-of-hooks'].create).toBe('function')
})

it('rejects a small oversized bounded collection before visiting any element', () => {
  let visited = 0
  const element = z.string().refine(() => { visited++; return true })
  const parsed = z.array(element).max(2).safeParse(Array(32).fill('synthetic-fixture'))
  expect(parsed.success).toBe(false)
  expect(parsed.error.issues).toHaveLength(1)
  expect(parsed.error.issues[0].code).toBe('too_big')
  expect(visited).toBe(0)
})

it('preserves valid schema output and Zod4 issue conversion through the actual parent bridge', () => {
  const schema = z.object({ attempts: z.number().int().min(1) })
  expect(schema.parse({ attempts: 2 })).toEqual({ attempts: 2 })
  const parsed = schema.safeParse({ attempts: 0 })
  expect(parsed.success).toBe(false)
  expect(parsed.error).toBeInstanceOf(z.ZodError)
  expect(parsed.error.issues[0].path).toEqual(['attempts'])
  expect(fromZodError(parsed.error).message).toContain('attempts')
})
