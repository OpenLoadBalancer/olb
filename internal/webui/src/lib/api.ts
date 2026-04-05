const API_BASE = (import.meta as any).env?.VITE_API_URL || ''

export class APIError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

async function fetchAPI<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}/api/v1${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })

  if (!response.ok) {
    throw new APIError(response.status, await response.text())
  }

  return response.json()
}

export const api = {
  getHealth: () => fetchAPI<{success: boolean; data: {status: string; checks: Record<string, {status: string; message: string}>; timestamp: string}}>('/system/health'),
  getInfo: () => fetchAPI<{success: boolean; data: {version: string; commit: string; build_date: string; uptime: string; state: string; go_version: string}}>('/system/info'),
  getPools: () => fetchAPI<any[]>('/pools'),
  getListeners: () => fetchAPI<any[]>('/listeners'),
}
