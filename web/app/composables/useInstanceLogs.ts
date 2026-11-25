import { instancesApi } from '~/lib/api/instances'
import { appendLogLine } from '~/lib/instances/logs'

export type LogStatus = 'connecting' | 'open' | 'closed'

/**
 * Live log tail of one instance. The stream (through the BFF) starts with the last lines of the log and then follows
 * it, so every (re)connect begins with a fresh buffer; the stream has no SSE ids, so nothing can be resumed.
 * The EventSource is closed when the owning component goes away. Nothing is opened during SSR.
 */
export function useInstanceLogs(id: string) {
  const lines = ref<string[]>([])
  const status = ref<LogStatus>('connecting')

  if (!import.meta.client || typeof EventSource === 'undefined') {
    return { lines, status, close: () => {} }
  }

  const source = new EventSource(instancesApi.logsStreamUrl(id))
  source.onopen = () => {
    lines.value = []
    status.value = 'open'
  }
  source.onmessage = (message: MessageEvent<string>) => {
    lines.value = appendLogLine(lines.value, message.data)
  }
  source.onerror = () => {
    // The browser retries while CONNECTING. CLOSED means it gave up (for example 404 for an unknown instance).
    status.value = source.readyState === EventSource.CLOSED ? 'closed' : 'connecting'
  }

  const close = () => {
    source.close()
    status.value = 'closed'
  }
  if (getCurrentScope()) onScopeDispose(close)

  return { lines, status, close }
}
