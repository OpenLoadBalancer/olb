import { useEffect, useRef, useCallback, useState } from "react"
import type { APIEventItem } from "@/types"

interface UseEventStreamOptions {
  /** Called when a new event arrives */
  onEvent?: (event: APIEventItem) => void
  /** Auto-reconnect interval in ms (default: 3000) */
  reconnectInterval?: number
  /** Whether the stream is enabled (default: true) */
  enabled?: boolean
}

interface UseEventStreamResult {
  /** Most recent event received */
  lastEvent: APIEventItem | null
  /** Whether the SSE connection is active */
  connected: boolean
  /** Manually reconnect */
  reconnect: () => void
}

/**
 * useEventStream subscribes to the SSE events stream at /api/v1/events/stream.
 * It auto-reconnects on disconnection and provides the last event received.
 */
export function useEventStream(options?: UseEventStreamOptions): UseEventStreamResult {
  const [lastEvent, setLastEvent] = useState<APIEventItem | null>(null)
  const [connected, setConnected] = useState(false)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const abortController = useRef<AbortController | null>(null)
  const onEventRef = useRef(options?.onEvent)
  useEffect(() => {
    onEventRef.current = options?.onEvent
  })

  const connect = useCallback(() => {
    // Clean up any existing connection
    if (abortController.current) {
      abortController.current.abort()
    }

    const enabled = options?.enabled !== false
    if (!enabled) return

    const controller = new AbortController()
    abortController.current = controller

    const connectStream = () => {
      fetch("/api/v1/events/stream", {
        signal: controller.signal,
        headers: { Accept: "text/event-stream" },
      })
        .then((response) => {
          if (!response.ok || !response.body) {
            throw new Error(`SSE connection failed: ${response.status}`)
          }
          setConnected(true)

          const reader = response.body.getReader()
          const decoder = new TextDecoder()
          let buffer = ""

          const processChunk = (): Promise<void> => {
            return reader.read().then(({ done, value }) => {
              if (done) {
                setConnected(false)
                return
              }

              buffer += decoder.decode(value, { stream: true })
              const lines = buffer.split("\n")
              buffer = lines.pop() || ""

              for (const line of lines) {
                if (line.startsWith("data: ")) {
                  try {
                    const event: APIEventItem = JSON.parse(line.slice(6))
                    setLastEvent(event)
                    onEventRef.current?.(event)
                  } catch {
                    // Ignore malformed data lines
                  }
                }
              }

              return processChunk()
            })
          }

          return processChunk()
        })
        .catch(() => {
          if (controller.signal.aborted) return
          setConnected(false)
          // Auto-reconnect after interval
          const interval = options?.reconnectInterval ?? 3000
          reconnectTimer.current = setTimeout(() => {
            if (!controller.signal.aborted) {
              connectStream()
            }
          }, interval)
        })
    }

    connectStream()
  }, [options?.enabled, options?.reconnectInterval])

  useEffect(() => {
    connect()

    return () => {
      if (abortController.current) {
        abortController.current.abort()
      }
      if (reconnectTimer.current) {
        clearTimeout(reconnectTimer.current)
      }
      setConnected(false)
    }
  }, [connect])

  return { lastEvent, connected, reconnect: connect }
}
