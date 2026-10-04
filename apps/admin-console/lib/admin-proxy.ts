import { callerProxy } from './caller-proxy'

/** Forward admin calls with the caller's JWT; preserve upstream auth failures. */
export async function adminProxy(
  path: string,
  options?: RequestInit & { userToken?: string }
) {
  return callerProxy(`/admin${path}`, options)
}
