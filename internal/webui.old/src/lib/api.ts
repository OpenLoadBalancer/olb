import axios, { AxiosError, AxiosInstance, AxiosRequestConfig } from 'axios'
import { toast } from 'sonner'

const apiClient: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

apiClient.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('olb-auth-token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

apiClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiError>) => {
    const message = error.response?.data?.error || error.message || 'An error occurred'

    if (error.response?.status === 401) {
      localStorage.removeItem('olb-auth-token')
      window.location.href = '/login'
    } else if (error.response?.status >= 500) {
      toast.error('Server Error', { description: message })
    } else if (error.response?.status !== 404) {
      toast.error('Error', { description: message })
    }

    return Promise.reject(error)
  }
)

interface ApiError {
  error: string
  message?: string
}

export async function apiRequest<T>(
  method: string,
  url: string,
  data?: unknown,
  config?: AxiosRequestConfig
): Promise<T> {
  const response = await apiClient.request<T>({
    method,
    url,
    data,
    ...config
  })
  return response.data
}

export function get<T>(url: string, config?: AxiosRequestConfig) {
  return apiRequest<T>('GET', url, undefined, config)
}

export function post<T>(url: string, data?: unknown, config?: AxiosRequestConfig) {
  return apiRequest<T>('POST', url, data, config)
}

export function put<T>(url: string, data?: unknown, config?: AxiosRequestConfig) {
  return apiRequest<T>('PUT', url, data, config)
}

export function patch<T>(url: string, data?: unknown, config?: AxiosRequestConfig) {
  return apiRequest<T>('PATCH', url, data, config)
}

export function del<T>(url: string, config?: AxiosRequestConfig) {
  return apiRequest<T>('DELETE', url, undefined, config)
}

export default apiClient
