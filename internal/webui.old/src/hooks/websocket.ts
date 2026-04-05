import { useEffect, useRef, useState, useCallback } from 'react'
import { toast } from 'sonner'

interface WebSocketMessage {
  type: 'metrics' | 'health' | 'event' | 'error'
  data: any
  timestamp: string
}

type MessageHandler = (message: WebSocketMessage) => void

interface UseWebSocketOptions {
  url: string
  onMessage?: MessageHandler
  onConnect?: () => void
  onDisconnect?: () => void
  onError?: (error: Event) => void
  reconnect?: boolean
  reconnectInterval?: number
  maxReconnectAttempts?: number
}

export function useWebSocket({
  url,
  onMessage,
  onConnect,
  onDisconnect,
  onError,
  reconnect = true,
  reconnectInterval = 3000,
  maxReconnectAttempts = 5
}: UseWebSocketOptions) {
  const [isConnected, setIsConnected] = useState(false)
  const [isConnecting, setIsConnecting] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttemptsRef = useRef(0)
  const reconnectTimeoutRef = useRef<NodeJS.Timeout>()

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return
    }

    setIsConnecting(true)

    try {
      const ws = new WebSocket(url)
      wsRef.current = ws

      ws.onopen = () => {
        setIsConnected(true)
        setIsConnecting(false)
        reconnectAttemptsRef.current = 0
        onConnect?.()
      }

      ws.onmessage = (event) => {
        try {
          const message: WebSocketMessage = JSON.parse(event.data)
          onMessage?.(message)
        } catch (error) {
          console.error('Failed to parse WebSocket message:', error)
        }
      }

      ws.onclose = () => {
        setIsConnected(false)
        setIsConnecting(false)
        onDisconnect?.()

        if (reconnect && reconnectAttemptsRef.current < maxReconnectAttempts) {
          reconnectAttemptsRef.current++
          reconnectTimeoutRef.current = setTimeout(() => {
            console.log(`[WebSocket] Reconnecting... (${reconnectAttemptsRef.current}/${maxReconnectAttempts})`)
            connect()
          }, reconnectInterval)
        } else if (reconnectAttemptsRef.current >= maxReconnectAttempts) {
          toast.error('WebSocket connection failed after maximum retries')
        }
      }

      ws.onerror = (error) => {
        setIsConnecting(false)
        onError?.(error)
      }
    } catch (error) {
      setIsConnecting(false)
      console.error('Failed to create WebSocket connection:', error)
    }
  }, [url, onMessage, onConnect, onDisconnect, onError, reconnect, reconnectInterval, maxReconnectAttempts])

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current)
    }
    wsRef.current?.close()
    wsRef.current = null
  }, [])

  const send = useCallback((data: any) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data))
      return true
    }
    return false
  }, [])

  useEffect(() => {
    connect()
    return () => disconnect()
  }, [connect, disconnect])

  return {
    isConnected,
    isConnecting,
    connect,
    disconnect,
    send
  }
}

// Hook for real-time metrics
export function useRealTimeMetrics() {
  const [metrics, setMetrics] = useState<any>(null)

  const { isConnected } = useWebSocket({
    url: `ws://${window.location.host}/ws/metrics`,
    onMessage: (message) => {
      if (message.type === 'metrics') {
        setMetrics(message.data)
      }
    },
    onConnect: () => {
      console.log('[WebSocket] Connected to metrics stream')
    },
    onDisconnect: () => {
      console.log('[WebSocket] Disconnected from metrics stream')
    }
  })

  return { metrics, isConnected }
}

// Hook for real-time health updates
export function useRealTimeHealth() {
  const [healthStatus, setHealthStatus] = useState<Map<string, any>>(new Map())

  const { isConnected } = useWebSocket({
    url: `ws://${window.location.host}/ws/health`,
    onMessage: (message) => {
      if (message.type === 'health') {
        setHealthStatus(prev => new Map(prev).set(message.data.backendId, message.data))
      }
    }
  })

  return { healthStatus, isConnected }
}

// Hook for system events
export function useSystemEvents() {
  const { send, isConnected } = useWebSocket({
    url: `ws://${window.location.host}/ws/events`,
    onMessage: (message) => {
      if (message.type === 'event') {
        const { severity, title, description } = message.data

        switch (severity) {
          case 'info':
            toast.info(title, { description })
            break
          case 'success':
            toast.success(title, { description })
            break
          case 'warning':
            toast.warning(title, { description })
            break
          case 'error':
            toast.error(title, { description })
            break
        }
      }
    }
  })

  const subscribe = useCallback((channel: string) => {
    send({ action: 'subscribe', channel })
  }, [send])

  const unsubscribe = useCallback((channel: string) => {
    send({ action: 'unsubscribe', channel })
  }, [send])

  return { subscribe, unsubscribe, isConnected }
}
