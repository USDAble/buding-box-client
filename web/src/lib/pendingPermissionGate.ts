// OCTO-FORK: a failed first-turn permission update must block retries until persisted.
const pending = new Map<string, string>()
export function confirmPendingPermissionChoice(id: string) { pending.delete(id) }
export function createPendingPermissionGate(apply: (id: string, mode: string) => Promise<void>) {
  return {
    require(id: string, mode: string) { pending.set(id, mode) },
    async ensure(id: string): Promise<void> {
      const mode = pending.get(id)
      if (!mode) return
      await apply(id, mode)
      pending.delete(id)
    },
  }
}
